# Password Hashing

Package `github.com/nathanpls/godo/password` hashes passwords with Argon2id and
stores parameters, salt, and derived key in the standard PHC string format.

```sh
godo add password
```

```go
hasher := password.NewDefault()

encoded, err := hasher.Hash([]byte(plainPassword))
if err != nil {
    log.Fatal(err)
}

matched, err := hasher.Verify([]byte(loginPassword), encoded)
if err != nil {
    log.Printf("invalid stored password hash: %v", err)
}
if !matched {
    // Reject the login without revealing which credential failed.
}
```

Store the complete encoded hash. Never store or log plaintext passwords.
`Verify` uses constant-time key comparison and returns `false, nil` for an
ordinary password mismatch. Malformed, unsupported, or resource-unsafe encoded
hashes return `password.ErrInvalidHash`.

Passwords are treated as bytes and limited to 1 MiB; oversized input returns
`password.ErrPasswordTooLong`. Verification accepts weaker legacy Argon2id
hashes within strict 64 MiB and combined-work ceilings so successful logins can
upgrade them. Apply rate limiting and bound concurrent login attempts because
password hashing is intentionally expensive.

After a successful login, check whether parameters have changed:

```go
rehash, err := hasher.NeedsRehash(encoded)
if err == nil && rehash {
    replacement, err := hasher.Hash([]byte(loginPassword))
    // Persist replacement after checking err.
}
```

The defaults use 64 MiB, three iterations, two lanes, a 16-byte random salt,
and a 32-byte key. Custom parameters must pass bounded validation. Tune them with
benchmarks on production-class hardware; do not lower them merely to speed up
tests.

The package clears its salt and derived-key buffers after use. The underlying
`x/crypto/argon2` implementation does not promise to wipe its internal working
matrix; deployments concerned with process memory dumps need operating-system
hardening in addition to application-level hashing.
