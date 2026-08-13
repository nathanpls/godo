# IDs

Package `github.com/nathanpls/godo/core/id` creates opaque, URL-safe identifiers with
at least 128 bits of cryptographic randomness using Go's standard library.

```sh
godo add id
```

Create an untyped ID:

```go
resourceID := id.New()
```

Add a short type prefix when IDs from several resources appear together:

```go
userID, err := id.NewPrefixed("usr")
if err != nil {
    log.Fatal(err)
}
```

Prefixes contain 1-32 lowercase ASCII letters or digits and start with a letter.
Generated IDs are lowercase and safe in URLs, filenames, logs, and databases.
They are opaque: clients must not infer timestamps, ordering, or other metadata
from them.

Prefer database-generated integer IDs when they already fit the service. Use
this package for public identifiers that should be unguessable and independent
of database sequence state.
