# Repository scout report

## Detected stack
- **Language:** Go 1.25 (`go.mod`)
- **CLI framework:** Cobra (`cmd/root.go`, `cmd/email.go`, `go.mod`)
- **Major libraries:**
  - IMAP: `github.com/emersion/go-imap/v2` (`internal/email/client.go`, `go.mod`)
  - MIME parsing: `github.com/jhillyerd/enmime` (`internal/email/client.go`, `go.mod`)
  - HTML→PDF: `github.com/chromedp/chromedp` (`internal/email/htmlpdf.go`, `go.mod`)
  - PDF inspection: `github.com/pdfcpu/pdfcpu` (`internal/pdfutil/detect.go`, `go.mod`)
  - Domain normalization: `golang.org/x/net/publicsuffix` (`internal/resolver/sender.go`)
- **Build/packaging:** single Go module binary `invp` (`go.mod`, `main.go`, `Justfile`)
- **Deployment/runtime:** GitHub Actions builds and releases cross-platform binaries; no Docker or infra manifests found (`.github/workflows/ci.yml`, `.github/workflows/release.yml`)

## Conventions
- **Formatting/linting:** Go-standard formatting implied; explicit lint command is `golangci-lint run ./...` with CI action, but no repo-local `.golangci.yml` was found (`Justfile`, `.github/workflows/ci.yml`)
- **Type checking:** compile-time via `go build -o invp .` (`Justfile`, `.github/workflows/ci.yml`)
- **Testing:** package-local `_test.go` files, table-driven tests with `t.Run`, temp dirs, and small mocks/interfaces (`internal/config/config_test.go`, `internal/email/exporter_test.go`, `internal/resolver/sender_test.go`)
- **Docs:** README is minimal; architecture clues live mostly in code and repo notes (`README.md`, `AGENTS.md`)

## Linting and testing commands
- No single all-in-one check target was found.
- Recommended minimal set:
  - Lint: `golangci-lint run ./...` (`Justfile:l18`, `.github/workflows/ci.yml:l18-l20`)
  - Test: `go test ./...` (`Justfile:l2-l3`, `.github/workflows/ci.yml:l30-l31`)
  - Build/type-check: `go build -o invp .` (`.github/workflows/ci.yml:l42`, `Justfile:l22-l23`)
- Convenience shortcuts:
  - `just lint`, `just test`, `just build`, `just run email export` (`Justfile`)
- Note: `just test-integration` / `go test -tags integration -v ./...` is advertised, but no `//go:build integration` files were found in the repo (`Justfile`, repo-wide search)

## Project structure hotspots
- `main.go` — binary entrypoint; calls `cmd.Execute()`
- `cmd/` — user-facing CLI commands and flags; key entrypoints are `root.go`, `email.go`, `email_export.go`, `email_mailboxes.go`, `version.go`
- `internal/email/` — core pipeline: IMAP fetch, MIME parsing, HTML→PDF, export orchestration; likely highest-change area
- `internal/config/` — JSON config loading, validation, default resolution
- `internal/output/` — output directory layout, attachment writes, provider README password hints
- `internal/resolver/` — sender-email → provider-name mapping and alias overrides
- `internal/pdfutil/` — PDF detection and password-lock checks
- `internal/logger/` — custom `slog` handler and verbosity levels
- `.github/workflows/` — CI/build/release automation boundaries

## Do and don't patterns
- **Do:** keep CLI thin and push behavior into `internal/` packages (`cmd/email_export.go`, `cmd/email_mailboxes.go`, `internal/email/exporter.go`)
- **Do:** use small interfaces for testability instead of heavy DI (`internal/email/client.go`, `internal/output/writer.go`)
- **Do:** use structured logging with `slog` plus custom verbosity levels (`internal/logger/logger.go`, `internal/email/client.go`, `internal/email/exporter.go`)
- **Do:** wrap errors with context and often continue on per-mailbox/per-message failures for partial results (`internal/config/config.go`, `internal/email/client.go`, `internal/email/exporter.go`)
- **Do:** use package-local, table-driven unit tests (`internal/config/config_test.go`, `internal/email/exporter_test.go`, `internal/email/parser_test.go`)
- **Don't:** use a DI container/service locator; constructors and direct package calls are preferred (`internal/email/exporter.go`, `internal/email/client.go`)
- **Don't:** use env-var/secret-manager config in-repo; configuration is file-based JSON with explicit passwords (`internal/config/config.go`, `config.example.json`)
- **Don't:** expose reusable public packages; the design prefers `internal/` boundaries over library packaging (`internal/`, `go.mod`)

## Open questions
- The repo advertises integration tests, but no integration-tagged test files were found. If real IMAP integration coverage exists outside the repo or is planned, that materially affects confidence in end-to-end behavior.
