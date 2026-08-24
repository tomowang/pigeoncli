package signature

import (
	"context"
	"testing"

	"github.com/tomowang/pigeoncli/internal/storage/sqlite"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	db, err := sqlite.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewService(db)
}

func TestAddRejectsEmptyBody(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.Add(context.Background(), "work", "Name", "", true); err == nil {
		t.Fatalf("expected error for empty body")
	}
}

func TestDefaultFallsBackToGlobal(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)

	if _, err := svc.Add(ctx, "", "Global", "-- global", true); err != nil {
		t.Fatalf("Add global: %v", err)
	}

	sig, ok, err := svc.Default(ctx, "work")
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	if !ok || sig.Body != "-- global" {
		t.Fatalf("unexpected default: ok=%v sig=%+v", ok, sig)
	}
}

func TestListAndSetDefault(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)

	a, err := svc.Add(ctx, "work", "A", "sig a", true)
	if err != nil {
		t.Fatalf("Add a: %v", err)
	}
	b, err := svc.Add(ctx, "work", "B", "sig b", false)
	if err != nil {
		t.Fatalf("Add b: %v", err)
	}

	if err := svc.SetDefault(ctx, b.ID); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}

	sigs, err := svc.List(ctx, "work")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sigs) != 2 {
		t.Fatalf("expected 2 signatures, got %d", len(sigs))
	}
	for _, s := range sigs {
		if s.ID == a.ID && s.IsDefault {
			t.Fatalf("expected signature a to no longer be default: %+v", s)
		}
		if s.ID == b.ID && !s.IsDefault {
			t.Fatalf("expected signature b to be default: %+v", s)
		}
	}
}
