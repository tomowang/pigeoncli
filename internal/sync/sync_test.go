package sync

import "testing"

func TestWindowHorizon(t *testing.T) {
	remote := map[uint32][]string{
		1: nil, 2: nil, 3: nil, 4: nil, 5: nil,
	}

	if got := windowHorizon(remote, 10); got != 0 {
		t.Fatalf("expected no cutoff when count >= len(remote), got %d", got)
	}
	if got := windowHorizon(remote, 5); got != 0 {
		t.Fatalf("expected no cutoff when count == len(remote), got %d", got)
	}
	if got := windowHorizon(remote, 2); got != 4 {
		t.Fatalf("expected horizon 4 (keeping UIDs 4,5), got %d", got)
	}
	if got := windowHorizon(remote, 1); got != 5 {
		t.Fatalf("expected horizon 5 (keeping only UID 5), got %d", got)
	}
	if got := windowHorizon(map[uint32][]string{}, 5); got != 0 {
		t.Fatalf("expected no cutoff for an empty remote set, got %d", got)
	}
}

func TestFlagsEqual(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
		want bool
	}{
		{"both nil", nil, nil, true},
		{"same order", []string{`\Seen`, `\Flagged`}, []string{`\Seen`, `\Flagged`}, true},
		{"different order", []string{`\Seen`, `\Flagged`}, []string{`\Flagged`, `\Seen`}, true},
		{"different length", []string{`\Seen`}, []string{`\Seen`, `\Flagged`}, false},
		{"different content", []string{`\Seen`}, []string{`\Flagged`}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := flagsEqual(c.a, c.b); got != c.want {
				t.Fatalf("flagsEqual(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
			}
		})
	}
}
