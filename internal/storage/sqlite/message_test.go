package sqlite

import (
	"testing"
	"time"
)

func TestFormatDateRoundTrip(t *testing.T) {
	want := time.Date(2026, 3, 5, 14, 30, 45, 123456789, time.FixedZone("PST", -8*60*60))

	got := parseDate(formatDate(want))

	if !got.Equal(want) {
		t.Fatalf("round trip mismatch: got %v, want %v", got, want)
	}
	if got.Location() != time.UTC {
		t.Fatalf("expected formatDate to normalize to UTC, got location %v", got.Location())
	}
}

func TestParseDateLegacyFormat(t *testing.T) {
	// Rows written before the RFC3339Nano fix hold Go's default
	// time.Time.String() format, e.g. "2026-03-05 14:30:45 +0000 UTC".
	got := parseDate("2026-03-05 14:30:45 +0000 UTC")
	want := time.Date(2026, 3, 5, 14, 30, 45, 0, time.UTC)

	if !got.Equal(want) {
		t.Fatalf("legacy format parse: got %v, want %v", got, want)
	}
}

func TestParseDateEmptyAndMalformed(t *testing.T) {
	cases := []string{
		"",
		"not a date",
		"2026-03-05",
	}
	for _, s := range cases {
		if got := parseDate(s); !got.IsZero() {
			t.Errorf("parseDate(%q) = %v, want zero time", s, got)
		}
	}
}
