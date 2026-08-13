# godo Development

When adding or changing functionality, complete the affected integration work in
the same task:

- Keep the public API small, composable, and documented with Go package comments.
- Update the `godo` CLI when the feature needs installation, generation,
  deployment, discovery, or another developer workflow.
- Update canonical Markdown under `docs/` and register new pages in the docs
  server when users or agents need to discover the behavior.
- Update the root README when package listings, installation, or primary usage
  changes.
- Add focused tests after the implementation is stable, then run formatting,
  race tests, vet, and module consistency checks.
- Commit coherent implementation and documentation sections separately when that
  improves history.
- Rebuild managed local documentation services after their source changes and
  verify agent access with `Accept: text/markdown`.

Only update integrations affected by the change; do not add unrelated boilerplate
or speculative functionality.
