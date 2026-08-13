# Service Lifecycle

Package `github.com/nathanpls/godo/core/lifecycle` runs service components under one
cancellation context and performs bounded concurrent shutdown.

```sh
godo add lifecycle
```

Create a signal-derived application context with the standard library, then run
the HTTP server and background workers together:

```go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()

server := &http.Server{Addr: ":8080", Handler: router}

err := lifecycle.Run(ctx, 15*time.Second,
    lifecycle.HTTPServer("api", server),
    lifecycle.Service{
        Name: "jobs",
        Run: func(ctx context.Context) error {
            return worker.Run(ctx)
        },
        Shutdown: func(ctx context.Context) error {
            return worker.Shutdown(ctx)
        },
    },
)
if err != nil {
    log.Fatal(err)
}
```

Services start concurrently. Parent cancellation, any service error, or a
service returning normally cancels the shared run context and starts all
configured shutdown functions concurrently. Runtime and shutdown errors are
joined. Parent cancellation is graceful, while exceeding the shutdown timeout
returns `context.DeadlineExceeded`.

`HTTPServer` converts `http.ErrServerClosed` into a normal stop and forcibly
closes active connections if graceful shutdown reaches its deadline. WebSockets
and other hijacked connections still need application-specific cancellation.
