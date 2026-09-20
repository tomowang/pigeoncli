package logs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLog(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pigeon.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func record(i int) string {
	return fmt.Sprintf(`{"time":"2026-01-02T03:04:05.%09dZ","level":"INFO","msg":"record %d","n":%d}`, i, i, i)
}

func TestBeforePagesNewestFirst(t *testing.T) {
	var lines []string
	for i := 0; i < 10; i++ {
		lines = append(lines, record(i))
	}
	s := NewService(writeLog(t, lines...))

	cur, err := s.End()
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for cur > 0 {
		var page []Entry
		page, cur, err = s.Before(cur, 4)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range page {
			got = append(got, e.Message)
		}
	}

	if len(got) != 10 {
		t.Fatalf("got %d entries, want 10: %v", len(got), got)
	}
	for i, msg := range got {
		if want := fmt.Sprintf("record %d", 9-i); msg != want {
			t.Fatalf("entry %d = %q, want %q", i, msg, want)
		}
	}
}

func TestBeforeSpansMultipleChunks(t *testing.T) {
	const n = 2000 // ~150 bytes/line => well over one 64 KiB chunk
	var lines []string
	for i := 0; i < n; i++ {
		lines = append(lines, fmt.Sprintf(`{"time":"2026-01-02T03:04:05Z","level":"INFO","msg":"record %d","pad":%q}`, i, strings.Repeat("x", 100)))
	}
	s := NewService(writeLog(t, lines...))
	cur, _ := s.End()

	var count int
	next := n - 1
	for cur > 0 {
		page, c, err := s.Before(cur, 300)
		if err != nil {
			t.Fatal(err)
		}
		cur = c
		for _, e := range page {
			if want := fmt.Sprintf("record %d", next); e.Message != want {
				t.Fatalf("got %q, want %q", e.Message, want)
			}
			next--
			count++
		}
	}
	if count != n {
		t.Fatalf("got %d entries, want %d", count, n)
	}
}

func TestBeforeLineLongerThanChunk(t *testing.T) {
	long := fmt.Sprintf(`{"time":"2026-01-02T03:04:05Z","level":"ERROR","msg":"big","blob":%q}`, strings.Repeat("y", readChunk*2+10))
	s := NewService(writeLog(t, record(1), long, record(2)))
	cur, _ := s.End()

	page, cur, err := s.Before(cur, 10)
	if err != nil {
		t.Fatal(err)
	}
	if cur != 0 || len(page) != 3 || page[1].Message != "big" {
		t.Fatalf("cur=%d entries=%d, want 0 and 3 with the long line in the middle", cur, len(page))
	}
}

func TestBeforeExcludesRecordsAfterEnd(t *testing.T) {
	path := writeLog(t, record(1), record(2))
	s := NewService(path)
	cur, _ := s.End()

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(record(3) + "\n")
	_ = f.Close()

	page, _, err := s.Before(cur, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 || page[0].Message != "record 2" {
		t.Fatalf("got %v, want records 2 and 1 only", page)
	}
}

func TestParseLine(t *testing.T) {
	e, ok := parseLine([]byte(`{"time":"2026-01-02T03:04:05Z","level":"WARN","msg":"hi","b":"two words","a":1}`))
	if !ok {
		t.Fatal("parse failed")
	}
	if e.Level != "WARN" || e.Message != "hi" || e.Attrs != `a=1 b="two words"` || e.Time.IsZero() {
		t.Fatalf("unexpected entry: %+v", e)
	}
	for _, bad := range []string{"", "not json", "[1]"} {
		if _, ok := parseLine([]byte(bad)); ok {
			t.Fatalf("parseLine(%q) ok, want skipped", bad)
		}
	}
}

func TestMissingFile(t *testing.T) {
	s := NewService(filepath.Join(t.TempDir(), "nope.jsonl"))
	cur, err := s.End()
	if err != nil || cur != 0 {
		t.Fatalf("End = %d, %v; want 0, nil", cur, err)
	}
	page, next, err := s.Before(10, 5)
	if err != nil || len(page) != 0 || next != 0 {
		t.Fatalf("Before = %v, %d, %v; want empty, 0, nil", page, next, err)
	}
}
