package auth

import (
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestSetGetDeleteOAuthToken(t *testing.T) {
	want := &oauth2.Token{
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(time.Hour).Truncate(time.Second),
	}
	if err := SetOAuthToken("gacct", want); err != nil {
		t.Fatalf("SetOAuthToken: %v", err)
	}

	got, err := GetOAuthToken("gacct")
	if err != nil {
		t.Fatalf("GetOAuthToken: %v", err)
	}
	if got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if !got.Expiry.Equal(want.Expiry) {
		t.Fatalf("expiry mismatch: got %v, want %v", got.Expiry, want.Expiry)
	}

	if err := DeleteOAuthToken("gacct"); err != nil {
		t.Fatalf("DeleteOAuthToken: %v", err)
	}
	if _, err := GetOAuthToken("gacct"); err == nil {
		t.Fatalf("expected error getting token after delete")
	}
}

func TestDeleteOAuthTokenMissingIsNotError(t *testing.T) {
	if err := DeleteOAuthToken("does-not-exist"); err != nil {
		t.Fatalf("DeleteOAuthToken on missing entry: %v", err)
	}
}

func TestOAuthTokenAndPasswordDoNotCollide(t *testing.T) {
	if err := SetPassword("shared-slug", "s3cret"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	if err := SetOAuthToken("shared-slug", &oauth2.Token{AccessToken: "tok"}); err != nil {
		t.Fatalf("SetOAuthToken: %v", err)
	}

	pw, err := GetPassword("shared-slug")
	if err != nil {
		t.Fatalf("GetPassword: %v", err)
	}
	if pw != "s3cret" {
		t.Fatalf("password was clobbered by oauth token write: got %q", pw)
	}

	tok, err := GetOAuthToken("shared-slug")
	if err != nil {
		t.Fatalf("GetOAuthToken: %v", err)
	}
	if tok.AccessToken != "tok" {
		t.Fatalf("got %q, want %q", tok.AccessToken, "tok")
	}
}
