package sqlite

import (
	"context"
	"testing"
)

func TestSignatureDefaultResolution(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	globalID, err := db.AddSignature(ctx, "", "Global", "-- global sig", true)
	if err != nil {
		t.Fatalf("AddSignature global: %v", err)
	}

	sig, ok, err := db.DefaultSignature(ctx, "work")
	if err != nil {
		t.Fatalf("DefaultSignature (no account sig yet): %v", err)
	}
	if !ok || sig.ID != globalID {
		t.Fatalf("expected global default, got ok=%v sig=%+v", ok, sig)
	}

	workID, err := db.AddSignature(ctx, "work", "Work", "-- work sig", true)
	if err != nil {
		t.Fatalf("AddSignature work: %v", err)
	}

	sig, ok, err = db.DefaultSignature(ctx, "work")
	if err != nil {
		t.Fatalf("DefaultSignature (with account sig): %v", err)
	}
	if !ok || sig.ID != workID {
		t.Fatalf("expected account-specific default, got ok=%v sig=%+v", ok, sig)
	}

	// Global default is unaffected by the account-scoped default.
	sig, ok, err = db.DefaultSignature(ctx, "personal")
	if err != nil {
		t.Fatalf("DefaultSignature (other account): %v", err)
	}
	if !ok || sig.ID != globalID {
		t.Fatalf("expected global default for unrelated account, got ok=%v sig=%+v", ok, sig)
	}
}

func TestSetDefaultSignatureClearsPreviousInScope(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	if _, err := db.AddSignature(ctx, "work", "First", "one", true); err != nil {
		t.Fatalf("AddSignature first: %v", err)
	}
	second, err := db.AddSignature(ctx, "work", "Second", "two", false)
	if err != nil {
		t.Fatalf("AddSignature second: %v", err)
	}

	if err := db.SetDefaultSignature(ctx, second); err != nil {
		t.Fatalf("SetDefaultSignature: %v", err)
	}

	sigs, err := db.ListSignatures(ctx, "work")
	if err != nil {
		t.Fatalf("ListSignatures: %v", err)
	}
	var gotDefault int64 = -1
	for _, s := range sigs {
		if s.IsDefault {
			gotDefault = s.ID
		}
	}
	if gotDefault != second {
		t.Fatalf("expected signature %d to be default, got %d", second, gotDefault)
	}
}

func TestUpdateAndRemoveSignature(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	id, err := db.AddSignature(ctx, "work", "Name", "body", false)
	if err != nil {
		t.Fatalf("AddSignature: %v", err)
	}
	if err := db.UpdateSignature(ctx, id, "New Name", "new body"); err != nil {
		t.Fatalf("UpdateSignature: %v", err)
	}

	sigs, err := db.ListSignatures(ctx, "work")
	if err != nil {
		t.Fatalf("ListSignatures: %v", err)
	}
	if len(sigs) != 1 || sigs[0].Name != "New Name" || sigs[0].BodyPlain != "new body" {
		t.Fatalf("unexpected signature after update: %+v", sigs)
	}

	if err := db.RemoveSignature(ctx, id); err != nil {
		t.Fatalf("RemoveSignature: %v", err)
	}
	sigs, err = db.ListSignatures(ctx, "work")
	if err != nil {
		t.Fatalf("ListSignatures after remove: %v", err)
	}
	if len(sigs) != 0 {
		t.Fatalf("expected no signatures after remove, got %+v", sigs)
	}
}
