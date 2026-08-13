package apikey

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileStoreLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".godo", "auth.json")
	if err := InitFile(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}
	if err := InitFile(path); err == nil {
		t.Fatal("existing auth file was replaced")
	}

	first, firstToken, err := CreateKey(path, "agent")
	if err != nil {
		t.Fatal(err)
	}
	second, secondToken, err := CreateKey(path, "deployment")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != 1 || second.ID != 2 || firstToken == secondToken || !strings.HasPrefix(firstToken, first.Prefix+"_") {
		t.Fatalf("keys = %+v %q, %+v %q", first, firstToken, second, secondToken)
	}
	third, _, err := CreateKeyWithScopes(path, "scoped", []string{"plans:write", "plans:read", "plans:read"})
	if err != nil {
		t.Fatal(err)
	}
	if !third.HasScope("plans:read") || !third.HasScope("plans:write") || len(third.Scopes) != 2 {
		t.Fatalf("scopes = %v", third.Scopes)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), firstToken) || strings.Contains(string(content), secondToken) {
		t.Fatal("auth file contains a plaintext token")
	}

	store := NewFileStore(path)
	identity, valid, err := store.Authenticate(firstToken)
	if err != nil || !valid || identity.ID != first.ID || identity.Name != "agent" {
		t.Fatalf("identity = %+v, valid = %t, error = %v", identity, valid, err)
	}
	if _, valid, err := store.Authenticate(firstToken + "invalid"); err != nil || valid {
		t.Fatalf("invalid token: valid = %t, error = %v", valid, err)
	}

	keys, err := ListKeys(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 3 || keys[0].ID != 1 || keys[1].ID != 2 || keys[2].ID != 3 {
		t.Fatalf("keys = %+v", keys)
	}

	revoked, err := RevokeKey(path, first.ID)
	if err != nil || !revoked {
		t.Fatalf("revoked = %t, error = %v", revoked, err)
	}
	if _, valid, err := store.Authenticate(firstToken); err != nil || valid {
		t.Fatalf("revoked token: valid = %t, error = %v", valid, err)
	}
	if _, valid, err := store.Authenticate(secondToken); err != nil || !valid {
		t.Fatalf("remaining token: valid = %t, error = %v", valid, err)
	}
	if revoked, err := RevokeKey(path, first.ID); err != nil || revoked {
		t.Fatalf("second revoke = %t, error = %v", revoked, err)
	}
}

func TestFileStoreRejectsLoosePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := InitFile(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ListKeys(path); err == nil {
		t.Fatal("auth file with loose permissions was accepted")
	}
}

func TestFileStoreRejectsSymlink(t *testing.T) {
	directory := t.TempDir()
	realPath := filepath.Join(directory, "real.json")
	linkPath := filepath.Join(directory, "auth.json")
	if err := InitFile(realPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Fatal(err)
	}
	if _, err := ListKeys(linkPath); err == nil {
		t.Fatal("symlinked auth file was accepted")
	}
}

func TestCreateKeyValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := InitFile(path); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "line\nbreak", "agent\tprod", "agent\x1bprod", strings.Repeat("x", 101)} {
		if _, _, err := CreateKey(path, name); err == nil {
			t.Fatalf("invalid name %q was accepted", name)
		}
	}
}

func TestFileStoreRequiresFile(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "missing.json"))
	if _, _, err := store.Authenticate("key"); err == nil {
		t.Fatal("missing file was accepted")
	}
	var nilStore *FileStore
	if _, _, err := nilStore.Authenticate("key"); err == nil {
		t.Fatal("nil file store was accepted")
	}
	if _, err := ListKeys(""); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty path error = %v", err)
	}
}

func TestVersionOneFileIsUpgraded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := InitFile(path); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content = []byte(strings.Replace(string(content), `"version": 2`, `"version": 1`, 1))
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := CreateKeyWithScopes(path, "scoped", []string{"plans:read"}); err != nil {
		t.Fatal(err)
	}
	content, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `"version": 2`) {
		t.Fatalf("file was not upgraded:\n%s", content)
	}
}
