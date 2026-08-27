# Contributing

Issues and pull requests are welcome. Keep adapters vendor-neutral at the domain boundary, add tests for scoring changes, and make safe behavior the default.

```bash
make fmt
make vet
make test-race
make build
```

Commit messages should explain the user-visible outcome. New probes must document the exact event they generate, required privileges, cleanup behavior, and how ground truth is established independently.
