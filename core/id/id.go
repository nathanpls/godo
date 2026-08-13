package id

import (
	"crypto/rand"
	"errors"
	"strings"
)

// New returns an opaque, lowercase, URL-safe identifier containing at least
// 128 bits of cryptographic randomness.
func New() string {
	return strings.ToLower(rand.Text())
}

// NewPrefixed returns an identifier such as usr_hzsv2y6...
//
// Prefix must start with a lowercase ASCII letter and contain at most 32
// lowercase ASCII letters or digits.
func NewPrefixed(prefix string) (string, error) {
	if !validPrefix(prefix) {
		return "", errors.New("id: prefix must be 1-32 lowercase ASCII letters or digits and start with a letter")
	}
	return prefix + "_" + New(), nil
}

func validPrefix(prefix string) bool {
	if len(prefix) < 1 || len(prefix) > 32 || prefix[0] < 'a' || prefix[0] > 'z' {
		return false
	}
	for _, character := range prefix[1:] {
		if character < 'a' || character > 'z' {
			if character < '0' || character > '9' {
				return false
			}
		}
	}
	return true
}
