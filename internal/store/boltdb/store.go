// Package boltdb implements the store interface using BoltDB.
package boltdb

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"time"
	"uuid"

	"go.etcd.io/bbolt"

	"github.com/NamanBalaji/tdm/internal/download"
	"github.com/NamanBalaji/tdm/internal/store"
)

var (
	bucketName        = []byte("downloads")
	ErrBucketNotFound = errors.New("bucket not found")
)

type Store struct {
	db *bbolt.DB
}

var _ store.Store = (*Store)(nil)

func New(path string) (*Store, error) {
	db, err := bbolt.Open(path, 0o600, &bbolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	err = db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bucketName)
		return err
	})
	if err != nil {
		db.Close() //nolint:errcheck

		return nil, fmt.Errorf("failed to initialize bucket: %w", err)
	}

	return &Store{db: db}, nil
}

func (s *Store) Save(_ context.Context, dl *download.Download) error {
	data, err := json.Marshal(dl)
	if err != nil {
		return fmt.Errorf("failed to marshal download: %w", err)
	}

	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketName)
		if b == nil {
			return ErrBucketNotFound
		}

		return b.Put([]byte(dl.ID.String()), data)
	})
}

func (s *Store) GetAll(_ context.Context) ([]*download.Download, error) {
	var downloads []*download.Download

	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketName)
		if b == nil {
			return ErrBucketNotFound
		}

		return b.ForEach(func(k, v []byte) error {
			var dl download.Download
			if err := json.Unmarshal(v, &dl); err != nil {
				return fmt.Errorf("failed to unmarshal download %s: %w", k, err)
			}

			downloads = append(downloads, &dl)

			return nil
		})
	})

	return downloads, err
}

func (s *Store) Delete(_ context.Context, id uuid.UUID) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketName)
		if b == nil {
			return ErrBucketNotFound
		}

		return b.Delete([]byte(id.String()))
	})
}

func (s *Store) Close() error {
	return s.db.Close()
}
