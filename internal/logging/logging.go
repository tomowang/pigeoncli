package logging

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// DefaultPath returns the default log file path
// ($XDG_CACHE_HOME/pigeon/pigeon.log, or the OS equivalent), matching the
// convention used by sqlite.DefaultPath() and blob.DefaultDir().
func DefaultPath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve cache dir: %w", err)
	}
	return filepath.Join(dir, "pigeon", "pigeon.log"), nil
}

// ParseLevel maps a --log-level flag value (debug, info, warn, or error,
// case-insensitive) to a slog.Level. Any other value is an error.
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown log level %q (want debug, info, warn, or error)", s)
	}
}

// Init opens path for appending (creating its parent directory and the
// file itself if needed) and installs a slog.Logger writing to it at level
// as the process-wide default (slog.SetDefault). The returned close func
// closes the underlying file and should be called on shutdown.
func Init(path string, level slog.Level) (close func() error, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	handler := slog.NewTextHandler(f, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))
	return f.Close, nil
}
