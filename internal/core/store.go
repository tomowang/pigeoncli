// Package core wires the internal/core/* services to their storage
// backends (internal/storage/sqlite, internal/storage/blob). It is the
// only place outside internal/storage/* that imports those packages —
// internal/cli and internal/tui call Open with plain paths and get back
// ready-to-use services, so they never import internal/storage directly.
package core

import (
	"context"

	"github.com/tomowang/pigeoncli/internal/core/folder"
	"github.com/tomowang/pigeoncli/internal/core/message"
	"github.com/tomowang/pigeoncli/internal/core/signature"
	"github.com/tomowang/pigeoncli/internal/storage/blob"
	"github.com/tomowang/pigeoncli/internal/storage/sqlite"
)

// Store owns the local sqlite cache and blob store, and the core services
// backed by them. Call Close when done with it.
type Store struct {
	db *sqlite.DB

	Folder    *folder.Service
	Message   *message.Service
	Signature *signature.Service
}

// Open opens the sqlite cache and blob store, creating them if needed, and
// wires up Folder, Message, and Signature. An empty dbPath or blobDir
// resolves to its package default (the XDG cache dir).
func Open(ctx context.Context, dbPath, blobDir string) (*Store, error) {
	if dbPath == "" {
		var err error
		dbPath, err = sqlite.DefaultPath()
		if err != nil {
			return nil, err
		}
	}
	if blobDir == "" {
		var err error
		blobDir, err = blob.DefaultDir()
		if err != nil {
			return nil, err
		}
	}

	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		return nil, err
	}
	blobs := blob.NewStore(blobDir)

	return &Store{
		db:        db,
		Folder:    folder.NewService(db),
		Message:   message.NewService(db, blobs),
		Signature: signature.NewService(db),
	}, nil
}

// Close closes the underlying sqlite connection.
func (s *Store) Close() error {
	return s.db.Close()
}
