# CLI

The `godo` CLI builds Go programs into persistent local services. Services start
with the Linux user session, restart after failures, and are published to agents
through the global OpenCode `AGENTS.md` file.

## Requirements

- Go 1.26 or newer
- Linux with a running `systemd --user` manager
- Applications that listen on the port supplied through the `PORT` environment
  variable

## Install

From the repository root:

```sh
./cli/install.sh
```

The script builds and installs `godo` to `$HOME/.local/bin/godo`. Make sure
`$HOME/.local/bin` is on `PATH`. Alternatively, use `go install ./cli/godo` and
add the Go binary directory, normally `$HOME/go/bin`, to `PATH`.

## Add a service

Pass a Go package directory, package path, or individual `.go` file:

```sh
godo service add ./docs \
  --name godo-docs \
  --additions 'Request Accept: text/markdown for canonical documentation'
```

This command:

1. Builds the target into a standalone binary.
2. Selects the first available port from `41000-41999`.
3. Installs and starts a `systemd --user` service.
4. Adds the service URL and usage note to the managed `<godo>` block in the
   global OpenCode `AGENTS.md`.

Set a specific port when needed:

```sh
godo service add ./docs --name godo-docs --port 8080
```

The target is resolved relative to the current directory when it is added. It
can therefore be rebuilt later regardless of the current shell directory.

## List services

```sh
godo service list
```

The output contains the stable ID, name, local URL, build target, and agent
instructions for each service. IDs start at `1`, increase monotonically, and are
not reused after removal.

## Update a service

Rebuild the original target and restart its service:

```sh
godo service update 1
```

The new binary is built before the running service is changed. If the updated
service cannot restart, godo restores and restarts the previous binary.

## Remove a service

```sh
godo service remove 1
```

This stops and disables the user service, removes its built files, updates the
registry, and removes it from agent discovery.

## Agent discovery

Service changes synchronize `~/.config/opencode/AGENTS.md` automatically. Run a
manual synchronization with:

```sh
godo agent
```

Only content between `<godo>` and `</godo>` is generated. Existing instructions
outside that block are preserved. A missing block is appended to the file.

## Application contract

Services receive their assigned port in `PORT`:

```go
port := os.Getenv("PORT")
if port == "" {
    port = "8080"
}

log.Fatal(http.ListenAndServe(":"+port, handler))
```

## Files and logs

- Registry: `$XDG_CONFIG_HOME/godo/services.json`, normally
  `~/.config/godo/services.json`
- Binaries: `$XDG_DATA_HOME/godo/services`, normally
  `~/.local/share/godo/services`
- User units: `$XDG_CONFIG_HOME/systemd/user`, normally
  `~/.config/systemd/user`
- Logs: `journalctl --user -u godo-<id>.service`

For example:

```sh
journalctl --user -u godo-1.service -f
```
