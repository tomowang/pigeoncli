// Package logging installs pigeon's process-wide slog.Logger, writing to a
// durable log file rather than stdout/stderr. This matters because the TUI
// (internal/tui) takes over the full terminal via bubbletea and requires a
// real TTY — any stray write to stdout/stderr while it's running would
// corrupt the rendered frame. CLI commands' existing user-facing stdout
// output (internal/cli) is unaffected: this package is an additional
// diagnostic channel, not a replacement for it.
//
// Callers use the installed logger via the package-level slog.Info/
// slog.Debug/slog.Warn/slog.Error functions (slog.SetDefault) rather than
// threading a *slog.Logger through every internal/core/sync/imap call —
// pigeon is a single-process CLI/TUI binary, so there's no per-request or
// multi-tenant need that would justify that extra plumbing.
package logging
