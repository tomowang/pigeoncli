package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultPath(t *testing.T) {
	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	want := filepath.Join("pigeon", "pigeon.jsonl")
	if !strings.HasSuffix(path, want) {
		t.Fatalf("DefaultPath() = %q, want suffix %q", path, want)
	}
}

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in      string
		want    slog.Level
		wantErr bool
	}{
		{"debug", slog.LevelDebug, false},
		{"INFO", slog.LevelInfo, false},
		{"Warn", slog.LevelWarn, false},
		{"error", slog.LevelError, false},
		{"", 0, true},
		{"verbose", 0, true},
	}
	for _, c := range cases {
		got, err := ParseLevel(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseLevel(%q): expected error, got nil", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseLevel(%q): unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestInitCreatesFileAndDirsAndFilters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "pigeon.jsonl")

	closeFn, err := Init(path, slog.LevelWarn)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	slog.Debug("debug line should be filtered out")
	slog.Warn("warn line should appear")

	if err := closeFn(); err != nil {
		t.Fatalf("close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "debug line") {
		t.Errorf("log file contains filtered debug line: %q", content)
	}
	if !strings.Contains(content, "warn line") {
		t.Errorf("log file missing expected warn line: %q", content)
	}
}

func TestInitAppendsAcrossCalls(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pigeon.jsonl")

	closeFn, err := Init(path, slog.LevelInfo)
	if err != nil {
		t.Fatalf("first Init: %v", err)
	}
	slog.Info("first line")
	if err := closeFn(); err != nil {
		t.Fatalf("close: %v", err)
	}

	closeFn, err = Init(path, slog.LevelInfo)
	if err != nil {
		t.Fatalf("second Init: %v", err)
	}
	slog.Info("second line")
	if err := closeFn(); err != nil {
		t.Fatalf("close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "first line") || !strings.Contains(content, "second line") {
		t.Errorf("expected both lines to be present, got: %q", content)
	}
}
