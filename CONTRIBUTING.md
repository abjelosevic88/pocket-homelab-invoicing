# Contributing

Thanks for helping! Pocket Invoicing aims to stay small, boring and reliable. Before a big change, open an issue to discuss it.

## Dev setup

```bash
git clone https://github.com/abjelosevic88/pocket-homelab-invoicing && cd pocket-homelab-invoicing
cd web && npm install && cd ..
make dev            # API on :8080 with demo data, Vite on :5173
```

- Go ≥ 1.26, Node ≥ 20. No CGO, no external services needed for development.
- `make test` runs `go vet`, `go test ./...` and the TypeScript type-check. CI runs the same plus a container build.
- Format Go with `gofmt`; keep the frontend free of extra UI frameworks (plain CSS in `web/src/styles.css`).

## Project conventions

- **Schema changes** go in a new file `internal/store/migrations/NNNN_name.sql`. Migrations run in order and are recorded; never edit an applied migration.
- **Money** is computed with `internal/money` (decimal) and rounded to the currency's minor unit. Don't do float math on amounts elsewhere.
- **API** lives under `/api/v1`. Add new routes in `internal/server/server.go` and document them in `docs/api.md`.
- **Settings** are a single JSON blob (`internal/store/settings.go`). Add fields with sensible zero-value defaults so upgrades are seamless.
- Keep the binary self-contained: embed assets, avoid runtime dependencies.

## Pull requests

1. One topic per PR, with tests where it makes sense.
2. Update `CHANGELOG.md` under *Unreleased*.
3. Make sure `make test` passes.
