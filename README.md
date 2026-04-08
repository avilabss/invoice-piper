# invoice-piper (`invp`)

`invp` connects to IMAP mailboxes, extracts invoice/receipt attachments, and writes them into a predictable directory layout for accounting workflows.

## Install / build

Prerequisites:
- Go **1.25+**
- IMAP account credentials
- **Chrome/Chromium in PATH** (only needed for HTML-email-to-PDF conversion)

Build a local binary named `invp`:

```bash
go build -o invp .
```

Run without building:

```bash
go run . <args>
```

Install from module path (binary name: `invoice-piper`):

```bash
go install github.com/avilabss/invoice-piper@latest
```

If you installed via `go install`, replace `invp` with `invoice-piper` in command examples below.

`just` shortcuts:

```bash
just build
just test
just lint
just run email export
```

## Config discovery

Config is loaded in this order:

1. `--config /path/to/config.json`
2. `./config.json`
3. `<os.UserConfigDir()>/invoice-piper/config.json`

Common `os.UserConfigDir()` examples:
- macOS: `~/Library/Application Support`
- Linux: `~/.config`
- Windows: `%AppData%`

If nothing is found, `invp` exits with: `config file not found; create ... (see config.example.json)`.

## Config schema

Example:

```json
{
  "output_dir": "./invoices",
  "provider_aliases": {
    "custom-billing.com": "mycorp"
  },
  "email": {
    "accounts": [
      {
        "name": "personal-gmail",
        "host": "imap.gmail.com",
        "security": "imaps",
        "port": 993,
        "username": "user@gmail.com",
        "password_env": "INVP_PERSONAL_GMAIL_PASSWORD",
        "mailboxes": ["INBOX"]
      }
    ]
  }
}
```

Top-level fields:
- `output_dir` (optional, default `./invoices`)
- `provider_aliases` (optional) — domain → provider-name override
- `email.accounts` (**required**, at least one account)

Per-account fields:
- `name` (optional, default = `username`)
- `host` (**required**)
- `security` (optional when omitted, defaults to `"imaps"`; if set, must be `"imaps"`, `"starttls"`, or `"plain"`)
  - `"imaps"` — TLS from connect (implicit TLS / IMAPS)
  - `"starttls"` — starts plain, then upgrades the connection with STARTTLS
  - `"plain"` — no TLS; use only intentionally for trusted/local bridge-style endpoints
- `port`
  - for `security: "imaps"`: optional, defaults to `993` when omitted
  - for `security: "starttls"` or `security: "plain"`: **required**
- `tls_skip_verify` (optional, default `false`) — only allowed for `imaps` / `starttls`; omit this field for `plain`
- `username` (**required**)
- `password_env` or `password` (**exactly one required**)
- `mailboxes` (optional, default `["INBOX"]`)

Transport note (important):
- `invp` now uses explicit IMAP transport modes. If you use bridge/local IMAP endpoints, set both `security` and `port` explicitly.
- Older configs that only changed `port` (while relying on implicit TLS defaults) should be updated to `security: "starttls"` or `"plain"` as appropriate.

Bridge-style STARTTLS account example:

```json
{
  "name": "proton-bridge",
  "host": "127.0.0.1",
  "security": "starttls",
  "port": 1143,
  "tls_skip_verify": true,
  "username": "bridge-user",
  "password_env": "INVP_PROTON_BRIDGE_PASSWORD",
  "mailboxes": ["INBOX"]
}
```

Use `tls_skip_verify` only for TLS-based modes (`imaps` / `starttls`) when you understand the security tradeoff (for example, a local bridge certificate you do not validate).

Mailbox behavior:
- Omit `mailboxes` to search only `INBOX`
- Set `mailboxes: ["ALL"]` to list and search every mailbox on that account

## Environment secrets

`password_env` is preferred over plaintext `password`.

```bash
export INVP_PERSONAL_GMAIL_PASSWORD="app-password"
```

If the env var is missing or empty, config load fails before any IMAP work starts.

## Core commands

```bash
invp version
invp email mailboxes
invp email export
invp email export --year 2025 --month 1
invp email export --year 2025 --month 1 --concurrency 1
```

Useful global flags:
- `--config` config file path
- `-v`, `-vv`, `-vvv` increasing log verbosity

`email export` defaults:
- `--year` omitted → current year
- `--month` omitted → current month
- `--concurrency` omitted or `<=0` → default concurrency (`3`)

## Output layout

Attachments are written under:

```text
<output_dir>/<year>/<month>/<provider>/<timestamp>.<ext>
```

Example:

```text
./invoices/2025/01/amazon/20250115_143022.pdf
```

Notes:
- `<provider>` is resolved from sender domain (with `provider_aliases` overrides)
- Provider directory names are sanitized to lowercase letters/digits/hyphens
- Filename collisions add suffixes (`..._2.pdf`, `..._3.pdf`, ...)
- Attachments without extension are saved as `.bin`
- On Unix-like systems, directories are created with `0700` and files with `0600`
- Effective privacy controls still depend on OS/filesystem behavior (for example, Windows ACLs are not managed by `invp`)

Password-locked PDF handling:
- Password-protected PDFs are still saved
- A provider-local `README.md` is created/appended with extracted password hints:

```text
<output_dir>/<year>/<month>/<provider>/README.md
```

## Partial failures and exit behavior

- `invp email export` continues processing and prints a summary for every account.
- It exits non-zero if **any** account had fetch/parse/write/conversion errors.
- `invp email mailboxes` lists mailboxes account-by-account and exits non-zero if one or more accounts fail.

This means you can still get useful output files even when exit status is non-zero.

## Troubleshooting

- **Config not found**: pass `--config` or create `./config.json` / user config path.
- **`password_env` error**: ensure the env var exists and is non-empty in the same shell/session.
- **IMAP login/connect errors**: verify `security`, `host`, and `port` (`993` is the default only for `imaps`; `starttls` / `plain` require explicit ports), plus username and app password.
- **Chrome/Chromium error during export**: install Chrome/Chromium and ensure it is available in `PATH`.
- **No files exported**: confirm `--year/--month` window and mailbox selection (`INBOX` vs explicit mailboxes vs `ALL`).
