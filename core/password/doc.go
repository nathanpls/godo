// Package password hashes and verifies passwords with Argon2id.
//
// Encoded hashes include their parameters and salt. Applications should store
// the complete encoded value and use Hasher.NeedsRehash after successful
// verification to identify hashes that use outdated parameters.
package password
