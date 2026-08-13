package apikey

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
	"unicode"
)

const fileVersion = 1

// Key describes an API key without exposing its secret.
type Key struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Prefix    string    `json:"prefix"`
	CreatedAt time.Time `json:"created_at"`
}

type storedKey struct {
	Key
	Hash string `json:"hash"`
}

type keyFile struct {
	Version int         `json:"version"`
	NextID  int         `json:"next_id"`
	Keys    []storedKey `json:"keys"`
}

// FileStore authenticates keys stored in a local JSON file. It reads the file
// for each authentication so key creation and revocation take effect without a
// service restart.
type FileStore struct {
	path string
}

// NewFileStore creates a Store backed by path.
func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

// Path returns the configured authentication file path.
func (store *FileStore) Path() string {
	if store == nil {
		return ""
	}
	return store.path
}

// Authenticate verifies token and returns its non-secret identity.
func (store *FileStore) Authenticate(token string) (Key, bool, error) {
	if store == nil || store.path == "" {
		return Key{}, false, errors.New("apikey: auth file path must not be empty")
	}
	registry, err := readKeyFile(store.path)
	if err != nil {
		return Key{}, false, err
	}
	want := sha256.Sum256([]byte(token))
	for _, candidate := range registry.Keys {
		stored, err := hex.DecodeString(candidate.Hash)
		if err != nil || len(stored) != sha256.Size {
			return Key{}, false, fmt.Errorf("apikey: auth file contains an invalid hash for key %d", candidate.ID)
		}
		if subtle.ConstantTimeCompare(stored, want[:]) == 1 {
			return candidate.Key, true, nil
		}
	}
	return Key{}, false, nil
}

// InitFile creates an empty authentication file with mode 0600. It refuses to
// replace an existing file.
func InitFile(path string) error {
	if path == "" {
		return errors.New("apikey: auth file path must not be empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("apikey: create auth directory: %w", err)
	}
	content, err := json.MarshalIndent(keyFile{Version: fileVersion, NextID: 1, Keys: []storedKey{}}, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("apikey: auth file %s already exists", path)
		}
		return fmt.Errorf("apikey: create auth file: %w", err)
	}
	if _, err := file.Write(content); err != nil {
		file.Close()
		_ = os.Remove(path)
		return fmt.Errorf("apikey: write auth file: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		_ = os.Remove(path)
		return fmt.Errorf("apikey: sync auth file: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("apikey: close auth file: %w", err)
	}
	return syncDirectory(filepath.Dir(path))
}

// CreateKey creates a key, stores only its hash, and returns the secret once.
func CreateKey(path, name string) (Key, string, error) {
	name = strings.TrimSpace(name)
	if !validKeyName(name) {
		return Key{}, "", errors.New("apikey: key name must be 1-100 characters on one line")
	}
	unlock, err := lockKeyFile(path)
	if err != nil {
		return Key{}, "", err
	}
	defer unlock()
	registry, err := readKeyFile(path)
	if err != nil {
		return Key{}, "", err
	}
	if registry.NextID == math.MaxInt {
		return Key{}, "", errors.New("apikey: key ID space is exhausted")
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return Key{}, "", fmt.Errorf("apikey: generate secret: %w", err)
	}
	prefix := "godo_" + hex.EncodeToString(random[:4])
	token := prefix + "_" + base64.RawURLEncoding.EncodeToString(random)
	digest := sha256.Sum256([]byte(token))
	identity := Key{ID: registry.NextID, Name: name, Prefix: prefix, CreatedAt: time.Now().UTC()}
	registry.NextID++
	registry.Keys = append(registry.Keys, storedKey{Key: identity, Hash: hex.EncodeToString(digest[:])})
	if err := writeKeyFile(path, registry); err != nil {
		return Key{}, "", err
	}
	return identity, token, nil
}

// ListKeys returns all non-secret key metadata in ID order.
func ListKeys(path string) ([]Key, error) {
	registry, err := readKeyFile(path)
	if err != nil {
		return nil, err
	}
	result := make([]Key, len(registry.Keys))
	for i, stored := range registry.Keys {
		result[i] = stored.Key
	}
	slices.SortFunc(result, func(a, b Key) int { return a.ID - b.ID })
	return result, nil
}

// RevokeKey removes a key by ID. It reports whether a key was removed.
func RevokeKey(path string, id int) (bool, error) {
	if id < 1 {
		return false, errors.New("apikey: key ID must be greater than zero")
	}
	unlock, err := lockKeyFile(path)
	if err != nil {
		return false, err
	}
	defer unlock()
	registry, err := readKeyFile(path)
	if err != nil {
		return false, err
	}
	before := len(registry.Keys)
	registry.Keys = slices.DeleteFunc(registry.Keys, func(key storedKey) bool { return key.ID == id })
	if len(registry.Keys) == before {
		return false, nil
	}
	if err := writeKeyFile(path, registry); err != nil {
		return false, err
	}
	return true, nil
}

func readKeyFile(path string) (keyFile, error) {
	file, err := openSecureFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return keyFile{}, fmt.Errorf("apikey: auth file %s does not exist", path)
		}
		return keyFile{}, err
	}
	defer file.Close()
	var registry keyFile
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&registry); err != nil {
		return keyFile{}, fmt.Errorf("apikey: decode auth file: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return keyFile{}, errors.New("apikey: auth file contains unexpected data")
	}
	if registry.Version != fileVersion {
		return keyFile{}, fmt.Errorf("apikey: unsupported auth file version %d", registry.Version)
	}
	if registry.NextID < 1 {
		return keyFile{}, errors.New("apikey: auth file has an invalid next_id")
	}
	seen := make(map[int]bool, len(registry.Keys))
	seenHashes := make(map[string]bool, len(registry.Keys))
	greatestID := 0
	for _, key := range registry.Keys {
		digest, hashErr := hex.DecodeString(key.Hash)
		if key.ID < 1 || seen[key.ID] || !validKeyName(key.Name) || key.Prefix == "" || hashErr != nil || len(digest) != sha256.Size || seenHashes[key.Hash] || key.CreatedAt.IsZero() {
			return keyFile{}, errors.New("apikey: auth file contains invalid key metadata")
		}
		seen[key.ID] = true
		seenHashes[key.Hash] = true
		greatestID = max(greatestID, key.ID)
	}
	if registry.NextID <= greatestID {
		return keyFile{}, errors.New("apikey: auth file next_id does not follow existing keys")
	}
	return registry, nil
}

func validKeyName(name string) bool {
	if name == "" || len(name) > 100 {
		return false
	}
	for _, character := range name {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func openSecureFile(path string) (*os.File, error) {
	descriptor, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(descriptor), path)
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("apikey: inspect auth file: %w", err)
	}
	if !info.Mode().IsRegular() {
		file.Close()
		return nil, fmt.Errorf("apikey: auth file %s must be a regular file", path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		file.Close()
		return nil, fmt.Errorf("apikey: auth file %s must not be accessible by group or others", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Getuid()) {
		file.Close()
		return nil, fmt.Errorf("apikey: auth file %s must be owned by the current user", path)
	}
	return file, nil
}

func lockKeyFile(path string) (func(), error) {
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("apikey: open auth lock: %w", err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		lock.Close()
		return nil, fmt.Errorf("apikey: lock auth file: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		_ = lock.Close()
	}, nil
}

func writeKeyFile(path string, registry keyFile) error {
	content, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return fmt.Errorf("apikey: encode auth file: %w", err)
	}
	content = append(content, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(path), ".auth-*")
	if err != nil {
		return fmt.Errorf("apikey: create temporary auth file: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("apikey: replace auth file: %w", err)
	}
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("apikey: open auth directory: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("apikey: sync auth directory: %w", err)
	}
	return nil
}
