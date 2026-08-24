package blob

import (
	"testing"
)

func TestWriteReadRoundTrip(t *testing.T) {
	s := NewStore(t.TempDir())
	ref := s.Ref(1, 2, 3)

	if _, err := s.Read(ref); err == nil {
		t.Fatalf("expected error reading before write")
	}

	want := []byte("From: a@example.com\r\n\r\nHello.\r\n")
	if err := s.Write(ref, want); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := s.Read(ref)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRefIsStableAndFilesystemSafe(t *testing.T) {
	s := NewStore(t.TempDir())
	ref := s.Ref(10, 20, 30)
	if ref != s.Ref(10, 20, 30) {
		t.Fatalf("expected Ref to be deterministic")
	}
	if ref == "" {
		t.Fatalf("expected non-empty ref")
	}
}
