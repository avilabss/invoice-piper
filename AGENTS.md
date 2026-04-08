# Invoice Piper (invp)

## Build & Test
- Build: `go build -o invp .`
- Test: `go test ./...`
- Lint: `golangci-lint run ./...`
- Run: `go run . <args>`
- Use `just` for shortcuts: `just test`, `just build`, `just lint`, `just run email export`

## Architecture
- `cmd/` — Cobra CLI commands (root, email, email_export, email_mailboxes)
- `internal/config/` — Config loading and validation
- `internal/email/` — IMAP client, MIME parser, HTML→PDF, exporter orchestrator
- `internal/resolver/` — Email sender → provider name mapping
- `internal/output/` — File writing, directory structure, README password hints
- `internal/pdfutil/` — PDF detection and password-lock checking

## Conventions
- Table-driven tests with `t.Run()`
- Interfaces for testability (e.g., `IMAPClient`)
- `internal/` over `pkg/` — nothing is externally importable
- Config resolution: `--config` flag → `./config.json` → `~/.config/invoice-piper/config.json`
