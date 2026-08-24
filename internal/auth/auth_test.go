package auth

import (
	"os"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}

func TestSetGetDeletePassword(t *testing.T) {
	if err := SetPassword("acct1", "s3cret"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	got, err := GetPassword("acct1")
	if err != nil {
		t.Fatalf("GetPassword: %v", err)
	}
	if got != "s3cret" {
		t.Fatalf("got %q, want %q", got, "s3cret")
	}

	if err := DeletePassword("acct1"); err != nil {
		t.Fatalf("DeletePassword: %v", err)
	}
	if _, err := GetPassword("acct1"); err == nil {
		t.Fatalf("expected error getting password after delete")
	}
}

func TestDeletePasswordMissingIsNotError(t *testing.T) {
	if err := DeletePassword("does-not-exist"); err != nil {
		t.Fatalf("DeletePassword on missing entry: %v", err)
	}
}
