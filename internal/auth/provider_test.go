package auth

import "testing"

func TestNewProviderDefaultsToPassword(t *testing.T) {
	p, err := NewProvider("", "user@example.com", "acct")
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if _, ok := p.(PasswordProvider); !ok {
		t.Fatalf("got %T, want PasswordProvider", p)
	}
}

func TestNewProviderPassword(t *testing.T) {
	p, err := NewProvider(AuthTypePassword, "user@example.com", "acct")
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if _, ok := p.(PasswordProvider); !ok {
		t.Fatalf("got %T, want PasswordProvider", p)
	}
}

func TestNewProviderGoogle(t *testing.T) {
	p, err := NewProvider(AuthTypeGoogle, "user@gmail.com", "acct")
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if _, ok := p.(GoogleProvider); !ok {
		t.Fatalf("got %T, want GoogleProvider", p)
	}
}

func TestNewProviderUnknown(t *testing.T) {
	if _, err := NewProvider("carrier-pigeon", "user@example.com", "acct"); err == nil {
		t.Fatalf("expected error for unknown auth type")
	}
}
