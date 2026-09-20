package logs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"
	"time"
)

// Cursor marks a position in the log file: everything before it (bytes
// [0, Cursor)) is still unread. Zero means the start of the file has been
// reached, i.e. there's nothing older left to load.
type Cursor int64

// Entry is one parsed log record.
type Entry struct {
	Time    time.Time
	Level   string // DEBUG, INFO, WARN, or ERROR
	Message string
	Attrs   string // remaining fields as space-separated key=value pairs, keys sorted
}

// readChunk is how many bytes Before reads backward per step; it doubles
// when a single line is longer than the chunk.
const readChunk = 64 << 10

// Service reads the log file at Path.
type Service struct {
	path string
}

// NewService returns a Service reading the log file at path. The file need
// not exist yet.
func NewService(path string) *Service {
	return &Service{path: path}
}

// End returns a Cursor at the current end of the log file. Passing it to
// Before pages through everything written up to now, so records appended
// afterwards (e.g. by the running session itself) are excluded. A missing
// file yields the zero Cursor.
func (s *Service) End() (Cursor, error) {
	fi, err := os.Stat(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("stat log file: %w", err)
	}
	return Cursor(fi.Size()), nil
}

// Before returns up to limit records that precede cur, newest first, and
// the Cursor to pass to the next call to continue further back. The
// returned Cursor is zero once the start of the file is reached. Lines
// that aren't valid slog JSON records are skipped.
func (s *Service) Before(cur Cursor, limit int) ([]Entry, Cursor, error) {
	if cur <= 0 || limit <= 0 {
		return nil, 0, nil
	}
	f, err := os.Open(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, cur, fmt.Errorf("open log file: %w", err)
	}
	defer func() { _ = f.Close() }()

	var entries []Entry
	end := int64(cur)
	size := int64(readChunk)
	for end > 0 && len(entries) < limit {
		start := max(end-size, 0)
		buf := make([]byte, end-start)
		if _, err := f.ReadAt(buf, start); err != nil && !errors.Is(err, io.EOF) {
			return entries, Cursor(end), fmt.Errorf("read log file: %w", err)
		}

		// Unless the chunk begins at the file start, its first line may be
		// cut off, so only the lines after the first newline are usable.
		base := start
		if start > 0 {
			i := bytes.IndexByte(buf, '\n')
			if i < 0 {
				size *= 2 // one line longer than the chunk; widen and retry
				continue
			}
			base = start + int64(i) + 1
			if base == end {
				size *= 2 // the only newline is the last byte; no complete line yet
				continue
			}
			buf = buf[i+1:]
		}
		size = readChunk

		// Walk the complete lines newest to oldest. end always lands on the
		// start of the line last consumed, which is where the next read
		// must stop.
		lineEnd := len(buf)
		for lineEnd > 0 && len(entries) < limit {
			data := bytes.TrimSuffix(buf[:lineEnd], []byte("\n"))
			lineStart := bytes.LastIndexByte(data, '\n') + 1
			if e, ok := parseLine(data[lineStart:]); ok {
				entries = append(entries, e)
			}
			lineEnd = lineStart
			end = base + int64(lineStart)
		}
		if lineEnd == 0 && len(entries) < limit {
			end = base // whole chunk consumed; continue before it
		}
	}
	return entries, Cursor(end), nil
}

// parseLine decodes one slog JSON record. ok is false for blank lines and
// anything that isn't a JSON object.
func parseLine(line []byte) (Entry, bool) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return Entry{}, false
	}
	var raw map[string]any
	if err := json.Unmarshal(line, &raw); err != nil {
		return Entry{}, false
	}

	var e Entry
	if s, ok := raw["time"].(string); ok {
		e.Time, _ = time.Parse(time.RFC3339Nano, s)
	}
	e.Level, _ = raw["level"].(string)
	e.Message, _ = raw["msg"].(string)
	delete(raw, "time")
	delete(raw, "level")
	delete(raw, "msg")

	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	pairs := make([]string, len(keys))
	for i, k := range keys {
		pairs[i] = k + "=" + formatValue(raw[k])
	}
	e.Attrs = strings.Join(pairs, " ")
	return e, true
}

func formatValue(v any) string {
	if s, ok := v.(string); ok {
		if s == "" || strings.ContainsAny(s, " \t\"=") {
			return fmt.Sprintf("%q", s)
		}
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}
