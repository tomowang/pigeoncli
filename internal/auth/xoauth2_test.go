package auth

import "testing"

func TestXOAuth2ClientStart(t *testing.T) {
	c := newXOAuth2Client("user@gmail.com", "ya29.token")
	mech, ir, err := c.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if mech != "XOAUTH2" {
		t.Fatalf("mech = %q, want XOAUTH2", mech)
	}
	want := "user=user@gmail.com\x01auth=Bearer ya29.token\x01\x01"
	if string(ir) != want {
		t.Fatalf("initial response = %q, want %q", ir, want)
	}
}

func TestXOAuth2ClientNextReturnsEmptyResponse(t *testing.T) {
	c := newXOAuth2Client("user@gmail.com", "ya29.token")
	resp, err := c.Next([]byte(`{"status":"400","schemes":"bearer"}`))
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if len(resp) != 0 {
		t.Fatalf("Next response = %q, want empty", resp)
	}
}
