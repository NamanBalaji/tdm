// Package store defines the persistence interface for downloads.
package store

import (
	"context"
	"uuid"

	"github.com/NamanBalaji/tdm/internal/download"
)

// Store abstracts download persistence.
type Store interface {
	Save(ctx context.Context, dl *download.Download) error
	GetAll(ctx context.Context) ([]*download.Download, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Close() error
}
