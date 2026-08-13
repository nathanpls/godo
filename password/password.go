package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	maxPasswordBytes = 1 << 20
	maxEncodedBytes  = 512
	maxVerifyMemory  = 64 * 1024
	maxVerifyWork    = 256 * 1024
	argon2Version    = 19
)

// ErrInvalidHash reports a malformed, unsupported, or unsafe encoded hash.
var ErrInvalidHash = errors.New("password: invalid encoded hash")

// ErrPasswordTooLong reports a password larger than the supported 1 MiB byte
// limit.
var ErrPasswordTooLong = errors.New("password: password exceeds 1 MiB")

// Params controls Argon2id hashing. Memory is measured in KiB.
type Params struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// DefaultParams returns the recommended default parameters: 64 MiB of memory,
// three iterations, two lanes, a 16-byte salt, and a 32-byte derived key.
func DefaultParams() Params {
	return Params{Memory: 64 * 1024, Iterations: 3, Parallelism: 2, SaltLength: 16, KeyLength: 32}
}

// Hasher hashes passwords using one validated parameter set.
type Hasher struct{ params Params }

// New creates a Hasher. Parameters are bounded so malformed configuration or
// encoded hashes cannot request unreasonable memory or CPU.
func New(params Params) (*Hasher, error) {
	if err := validateGenerationParams(params); err != nil {
		return nil, err
	}
	return &Hasher{params: params}, nil
}

// NewDefault creates a Hasher using DefaultParams.
func NewDefault() *Hasher {
	hasher, err := New(DefaultParams())
	if err != nil {
		panic(err)
	}
	return hasher
}

// Hash returns a PHC-formatted Argon2id hash with a fresh random salt.
func (hasher *Hasher) Hash(password []byte) (string, error) {
	if hasher == nil {
		return "", errors.New("password: hasher must not be nil")
	}
	if len(password) > maxPasswordBytes {
		return "", ErrPasswordTooLong
	}
	salt := make([]byte, hasher.params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("password: generate salt: %w", err)
	}
	key := argon2.IDKey(password, salt, hasher.params.Iterations, hasher.params.Memory, hasher.params.Parallelism, hasher.params.KeyLength)
	encoded := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2Version, hasher.params.Memory, hasher.params.Iterations, hasher.params.Parallelism, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
	clear(key)
	clear(salt)
	return encoded, nil
}

// Verify reports whether password matches encoded. A mismatch is not an error.
func (hasher *Hasher) Verify(password []byte, encoded string) (bool, error) {
	if hasher == nil {
		return false, errors.New("password: hasher must not be nil")
	}
	if len(password) > maxPasswordBytes {
		return false, ErrPasswordTooLong
	}
	parsed, salt, want, err := parseHash(encoded)
	if err != nil {
		return false, err
	}
	got := argon2.IDKey(password, salt, parsed.Iterations, parsed.Memory, parsed.Parallelism, parsed.KeyLength)
	match := subtle.ConstantTimeCompare(got, want) == 1
	clear(got)
	clear(want)
	clear(salt)
	return match, nil
}

// NeedsRehash reports whether encoded uses valid but different parameters.
func (hasher *Hasher) NeedsRehash(encoded string) (bool, error) {
	if hasher == nil {
		return false, errors.New("password: hasher must not be nil")
	}
	params, salt, key, err := parseHash(encoded)
	if err != nil {
		return false, err
	}
	clear(key)
	clear(salt)
	return params != hasher.params, nil
}

func validateGenerationParams(params Params) error {
	work := uint64(params.Memory) * uint64(params.Iterations)
	if params.Parallelism < 1 || params.Parallelism > 16 || params.Memory > maxVerifyMemory || params.Iterations < 1 || params.Iterations > 10 || work > maxVerifyWork || !strongProfile(params.Memory, params.Iterations) || params.SaltLength < 16 || params.SaltLength > 48 || params.KeyLength < 16 || params.KeyLength > 64 {
		return errors.New("password: parameters exceed supported safety bounds")
	}
	return nil
}

func strongProfile(memory, iterations uint32) bool {
	switch iterations {
	case 1:
		return memory >= 46*1024
	case 2:
		return memory >= 19*1024
	case 3:
		return memory >= 12*1024
	case 4:
		return memory >= 9*1024
	default:
		return memory >= 7*1024
	}
}

func validateVerificationParams(params Params) error {
	work := uint64(params.Memory) * uint64(params.Iterations)
	if params.Parallelism < 1 || params.Parallelism > 16 || params.Memory < 8*uint32(params.Parallelism) || params.Memory > maxVerifyMemory || params.Iterations < 1 || params.Iterations > 10 || work > maxVerifyWork || params.SaltLength < 8 || params.SaltLength > 48 || params.KeyLength < 12 || params.KeyLength > 64 {
		return fmt.Errorf("%w: parameters exceed verification safety bounds", ErrInvalidHash)
	}
	return nil
}

func parseHash(encoded string) (Params, []byte, []byte, error) {
	if len(encoded) == 0 || len(encoded) > maxEncodedBytes {
		return Params{}, nil, nil, ErrInvalidHash
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return Params{}, nil, nil, ErrInvalidHash
	}
	parameters := strings.Split(parts[3], ",")
	if len(parameters) != 3 {
		return Params{}, nil, nil, ErrInvalidHash
	}
	memory, err := parseHashParameter(parameters[0], "m", 32)
	if err != nil {
		return Params{}, nil, nil, err
	}
	iterations, err := parseHashParameter(parameters[1], "t", 32)
	if err != nil {
		return Params{}, nil, nil, err
	}
	parallelism, err := parseHashParameter(parameters[2], "p", 8)
	if err != nil {
		return Params{}, nil, nil, err
	}
	if len(parts[4]) > 88 || len(parts[5]) > 88 {
		return Params{}, nil, nil, ErrInvalidHash
	}
	if strings.ContainsAny(parts[4], "\r\n") || strings.ContainsAny(parts[5], "\r\n") {
		return Params{}, nil, nil, ErrInvalidHash
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil {
		return Params{}, nil, nil, ErrInvalidHash
	}
	key, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil {
		clear(salt)
		return Params{}, nil, nil, ErrInvalidHash
	}
	params := Params{Memory: uint32(memory), Iterations: uint32(iterations), Parallelism: uint8(parallelism), SaltLength: uint32(len(salt)), KeyLength: uint32(len(key))}
	if err := validateVerificationParams(params); err != nil {
		clear(salt)
		clear(key)
		return Params{}, nil, nil, err
	}
	return params, salt, key, nil
}

func parseHashParameter(value, name string, bits int) (uint64, error) {
	prefix := name + "="
	if !strings.HasPrefix(value, prefix) {
		return 0, ErrInvalidHash
	}
	raw := strings.TrimPrefix(value, prefix)
	parsed, err := strconv.ParseUint(raw, 10, bits)
	if err != nil {
		return 0, ErrInvalidHash
	}
	if strconv.FormatUint(parsed, 10) != raw {
		return 0, ErrInvalidHash
	}
	return parsed, nil
}
