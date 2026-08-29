# AGENTS.md

This file provides guidance to AI coding agents (Claude Code, etc.) when working with code in this repository.

## Commands

```
make build   # build ./cmd/pigeon -> bin/pigeon (with version ldflags)
make run     # go run ./cmd/pigeon
make test    # go test ./...
make vet     # go vet ./...
make lint    # golangci-lint run
make tidy    # go mod tidy
```

Single package/test: `go test ./internal/core/account/...` (or `-run TestName` once tests exist).

`pigeon` with no subcommand launches the TUI; `pigeon version` prints build info; `pigeon --help` lists subcommands as they're added.

The TUI (`internal/tui`) requires a real TTY — it will fail with `could not open a new TTY` when run headless/non-interactively (e.g. piped or backgrounded shells). This is expected; test it in an actual terminal.

## Architecture

pigeon is a local email client (TUI-first, with a CLI, and an HTTP API planned later) built on `charmbracelet/bubbletea`. The codebase is layered so all three frontends share one core:

```
internal/cli, internal/tui, (future) internal/httpapi
                    |
              internal/core/*         <- account, folder, message, signature, compose
       (services with DTO-only APIs; the ONLY layer the frontends may import)
                    |
   internal/imap, internal/smtp, internal/mime, internal/sync, internal/storage/sqlite
```

**The boundary that matters:** `internal/tui` and `internal/cli` must only import `internal/core/*` (and `internal/buildinfo`) — never `internal/imap`, `internal/smtp`, `internal/storage`, or `internal/sync` directly. Those lower-level packages are consumed exclusively by `internal/core/*`. This is what lets `internal/httpapi` be added later as a third frontend without touching business logic. When adding a feature, put the actual logic in the relevant `internal/core/<domain>` service and keep `internal/tui`/`internal/cli` code to rendering/argument-parsing plus calls into that service.

**Credentials vs. config:** account connection settings (host, port, TLS, username) live in a TOML config file (`internal/config`); secrets (passwords, tokens) live in the OS keyring via `internal/auth`, never in the config file or the sqlite DB. `internal/auth` defines a `Provider` interface (`IMAPCredentials`, `SMTPAuth`) — v1 only implements a password provider, but OAuth2 can be added later as a second implementation without changing `internal/imap`/`internal/smtp`.

**Local cache, not live IMAP passthrough:** message/folder data is synced into a local SQLite DB (`internal/storage/sqlite`, `modernc.org/sqlite` — pure Go, no cgo, chosen for easy cross-compilation) via `internal/sync`, which reconciles per-folder `UIDVALIDITY`/`UID` state. Headers sync eagerly; raw message bodies are fetched and cached lazily on first open (stored via `raw_ref`, not as inline sqlite blobs). The TUI/CLI read from this cache through `internal/core/*`, not live from IMAP, so mailbox browsing works offline after a sync.

**Bubbletea async pattern:** every `internal/core` call that touches the network is wrapped in a `tea.Cmd` closure returning a typed `tea.Msg` (e.g. `messagesLoadedMsg`, `sendResultMsg`, `errMsg`); `App.Update` routes these to the relevant sub-model. Long-running streams (like sync progress) use a channel plus a self-re-arming `tea.Cmd` that reads one event and re-issues itself until a "done" event arrives — don't block `Update` on a channel read directly.

**Logging:** `internal/logging` installs a process-wide `log/slog` default logger (`slog.SetDefault`) writing to a file — never stdout/stderr, since the TUI takes over the full terminal and any stray print would corrupt the rendered frame. `internal/cli/root.go`'s `PersistentPreRunE`/`PersistentPostRun` initialize/close it for every subcommand, controlled by `--log-level` (debug/info/warn/error) and `--log-file` (default `$XDG_CACHE_HOME/pigeon/pigeon.log`). Call sites use the package-level `slog.Info`/`slog.Debug`/etc. directly (no logger threaded through `internal/core`/`internal/sync`/`internal/imap` signatures) — it's additive to, not a replacement for, the TUI's ephemeral status bar (`internal/tui/status.go`) and the CLI's existing user-facing stdout output.

## Commit conventions

Commits follow [Conventional Commits](https://www.conventionalcommits.org/) with a scope: `type(scope): subject`. Common types: `feat`, `fix`, `refactor`, `docs`, `test`, `chore`, `ci`, `build`.

Scopes mirror the top-level `internal/` packages, plus a few cross-cutting ones:

| Scope | Covers |
|---|---|
| `cli` | `internal/cli` |
| `tui` | `internal/tui` |
| `core` | `internal/core/*` (or a subdomain scope like `account`/`folder`/`message`/`compose`/`signature` when a change is isolated to one) |
| `imap` | `internal/imap` |
| `smtp` | `internal/smtp` |
| `mime` | `internal/mime` |
| `sync` | `internal/sync` |
| `storage` | `internal/storage/sqlite` |
| `auth` | `internal/auth` |
| `config` | `internal/config` |
| `logging` | `internal/logging` |
| `httpapi` | `internal/httpapi` |
| `agents` | `AGENTS.md` / `CLAUDE.md` |
| `build` | `go.mod`, `Makefile`, `.golangci.yml` |
| `ci` | `.github/workflows` |

Example: `feat(auth): add keyring-backed password provider`.

## Project status

This is an early scaffold (Phase 0 of a phased build). Most `internal/*` packages beyond `cli`, `tui`, and `buildinfo` currently contain only a `doc.go` describing their intended responsibility — check a package's `doc.go` before assuming it has no logic yet vs. hasn't been built. The full phased plan (account config, IMAP sync, message read/raw view, reply/send, signatures) lives in the repo owner's Claude Code plan history if you need the original design rationale; infer current architecture from the code itself, not from that plan, once packages diverge from it.
