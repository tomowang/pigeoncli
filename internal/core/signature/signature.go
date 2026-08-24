package signature

import (
	"context"
	"fmt"

	"github.com/tomowang/pigeoncli/internal/storage/sqlite"
)

// Signature is the signature DTO used by internal/cli, internal/tui, and
// internal/core/compose. AccountSlug is "" for the global default
// signature (used by an account that has none of its own).
type Signature struct {
	ID          int64
	AccountSlug string
	Name        string
	Body        string
	IsDefault   bool
}

// Service manages saved signatures.
type Service struct {
	db *sqlite.DB
}

// NewService creates a Service backed by db.
func NewService(db *sqlite.DB) *Service {
	return &Service{db: db}
}

// List returns every signature visible to accountSlug: its own plus the
// global ones, account-specific first. Pass "" to list only global
// signatures.
func (s *Service) List(ctx context.Context, accountSlug string) ([]Signature, error) {
	rows, err := s.db.ListSignatures(ctx, accountSlug)
	if err != nil {
		return nil, err
	}
	return toSignatures(rows), nil
}

// ListAll returns every signature across every account and global.
func (s *Service) ListAll(ctx context.Context) ([]Signature, error) {
	rows, err := s.db.ListAllSignatures(ctx)
	if err != nil {
		return nil, err
	}
	return toSignatures(rows), nil
}

// Add creates a signature scoped to accountSlug ("" for global).
func (s *Service) Add(ctx context.Context, accountSlug, name, body string, isDefault bool) (Signature, error) {
	if body == "" {
		return Signature{}, fmt.Errorf("signature body must not be empty")
	}
	id, err := s.db.AddSignature(ctx, accountSlug, name, body, isDefault)
	if err != nil {
		return Signature{}, err
	}
	return Signature{ID: id, AccountSlug: accountSlug, Name: name, Body: body, IsDefault: isDefault}, nil
}

// Update replaces a signature's name and body.
func (s *Service) Update(ctx context.Context, id int64, name, body string) error {
	if body == "" {
		return fmt.Errorf("signature body must not be empty")
	}
	return s.db.UpdateSignature(ctx, id, name, body)
}

// Remove deletes a signature.
func (s *Service) Remove(ctx context.Context, id int64) error {
	return s.db.RemoveSignature(ctx, id)
}

// SetDefault makes a signature the default within its own scope (global,
// or the account it belongs to).
func (s *Service) SetDefault(ctx context.Context, id int64) error {
	return s.db.SetDefaultSignature(ctx, id)
}

// Default resolves the signature that should be inserted into a new draft
// for accountSlug: that account's own default if it has one, otherwise
// the global default, otherwise none (ok is false).
func (s *Service) Default(ctx context.Context, accountSlug string) (Signature, bool, error) {
	row, ok, err := s.db.DefaultSignature(ctx, accountSlug)
	if err != nil || !ok {
		return Signature{}, false, err
	}
	return toSignature(row), true, nil
}

func toSignature(r sqlite.SignatureRow) Signature {
	return Signature{ID: r.ID, AccountSlug: r.AccountSlug, Name: r.Name, Body: r.BodyPlain, IsDefault: r.IsDefault}
}

func toSignatures(rows []sqlite.SignatureRow) []Signature {
	out := make([]Signature, len(rows))
	for i, r := range rows {
		out[i] = toSignature(r)
	}
	return out
}
