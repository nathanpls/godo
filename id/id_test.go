package id

import (
	"strings"
	"testing"
)

func TestNewAndPrefix(t *testing.T) {
	first, second := New(), New()
	if first == second || len(first) < 20 || strings.ToLower(first) != first {
		t.Fatalf("IDs = %q %q", first, second)
	}
	prefixed, err := NewPrefixed("usr2")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(prefixed, "usr2_") {
		t.Fatalf("prefixed ID = %q", prefixed)
	}
	for _, prefix := range []string{"", "2user", "User", "user-id", strings.Repeat("x", 33)} {
		if _, err := NewPrefixed(prefix); err == nil {
			t.Fatalf("invalid prefix %q was accepted", prefix)
		}
	}
}
