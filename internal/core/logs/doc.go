// Package logs is a core domain/service layer package. It exposes Service,
// a read-only view over the JSON Lines log file that internal/logging
// writes, so internal/tui (and a future internal/httpapi) can show past
// log records without importing internal/logging or parsing slog's on-disk
// format themselves.
package logs
