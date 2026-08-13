package password

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func testParams() Params {
	return Params{Memory: 19 * 1024, Iterations: 2, Parallelism: 1, SaltLength: 16, KeyLength: 16}
}

func TestHashVerifyAndRehash(t *testing.T) {
	hasher, err := New(testParams())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := hasher.Hash([]byte("correct horse battery staple"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("hash = %q", encoded)
	}
	matched, err := hasher.Verify([]byte("correct horse battery staple"), encoded)
	if err != nil || !matched {
		t.Fatalf("matched = %t, error = %v", matched, err)
	}
	matched, err = hasher.Verify([]byte("wrong"), encoded)
	if err != nil || matched {
		t.Fatalf("wrong password matched = %t, error = %v", matched, err)
	}
	rehash, err := hasher.NeedsRehash(encoded)
	if err != nil || rehash {
		t.Fatalf("rehash = %t, error = %v", rehash, err)
	}
	other, _ := New(Params{Memory: 46 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 16})
	rehash, err = other.NeedsRehash(encoded)
	if err != nil || !rehash {
		t.Fatalf("rehash = %t, error = %v", rehash, err)
	}
}

func TestMalformedAndUnsafeHashesAreRejected(t *testing.T) {
	hasher, _ := New(testParams())
	for _, encoded := range []string{
		"not-a-hash",
		"$argon2i$v=19$m=8,t=1,p=1$YWJjZGVmZ2hpamtsbW5vcA$YWJjZGVmZ2hpamtsbW5vcA",
		"$argon2id$v=19$m=262145,t=1,p=1$YWJjZGVmZ2hpamtsbW5vcA$YWJjZGVmZ2hpamtsbW5vcA",
		"$argon2id$v=19$m=8,t=1,p=1$bad*$YWJjZGVmZ2hpamtsbW5vcA",
		"$argon2id$v=19$m=19456,t=2,p=1$YWJjZGVmZ2hpamtsbW5vcA\n$YWJjZGVmZ2hpamtsbW5vcA",
		"$argon2id$v=19$m=019456,t=2,p=1$YWJjZGVmZ2hpamtsbW5vcA$YWJjZGVmZ2hpamtsbW5vcA",
		strings.Repeat("x", maxEncodedBytes+1),
	} {
		if _, err := hasher.Verify([]byte("password"), encoded); !errors.Is(err, ErrInvalidHash) {
			t.Fatalf("hash %q error = %v", encoded, err)
		}
	}
}

func TestVerifiesLegacyKnownVectorAndRequestsRehash(t *testing.T) {
	key, err := hex.DecodeString("068d62b26455936aa6ebe60060b0a65870dbfa3ddf8d41f7")
	if err != nil {
		t.Fatal(err)
	}
	encoded := "$argon2id$v=19$m=64,t=2,p=1$" + base64.RawStdEncoding.EncodeToString([]byte("somesalt")) + "$" + base64.RawStdEncoding.EncodeToString(key)
	hasher := NewDefault()
	matched, err := hasher.Verify([]byte("password"), encoded)
	if err != nil || !matched {
		t.Fatalf("matched = %t, error = %v", matched, err)
	}
	rehash, err := hasher.NeedsRehash(encoded)
	if err != nil || !rehash {
		t.Fatalf("rehash = %t, error = %v", rehash, err)
	}
}

func TestParameterValidation(t *testing.T) {
	for _, params := range []Params{{}, {Memory: 19 * 1024, Iterations: 11, Parallelism: 1, SaltLength: 16, KeyLength: 16}, {Memory: 19 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 16}, {Memory: 65 * 1024, Iterations: 2, Parallelism: 1, SaltLength: 16, KeyLength: 16}, {Memory: 64 * 1024, Iterations: 5, Parallelism: 1, SaltLength: 16, KeyLength: 16}, {Memory: 19 * 1024, Iterations: 2, Parallelism: 1, SaltLength: 49, KeyLength: 16}} {
		if _, err := New(params); err == nil {
			t.Fatalf("invalid parameters were accepted: %+v", params)
		}
	}
}
