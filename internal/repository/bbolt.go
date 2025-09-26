package repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.etcd.io/bbolt"
)

const (
	downloadsBucket = "downloads"
)

type Object struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

var (
	// ErrDownloadNotFound is returned when a download cannot be found.
	ErrDownloadNotFound = errors.New("download not found")
	ErrBucketNotFound   = errors.New("bucket not found")
)

type Download interface {
	GetID() uuid.UUID
	Type() string
	MarshalJSON() ([]byte, error)
}

// BboltRepository implements the downloader.DownloadRepository interface.
type BboltRepository struct {
	db *bbolt.DB
}

// NewBboltRepository creates a new bbolt repository.
func NewBboltRepository(dbPath string) (*BboltRepository, error) {
	options := &bbolt.Options{
		Timeout: 1 * time.Second,
	}

	db, err := bbolt.Open(dbPath, 0o600, options)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	repo := &BboltRepository{
		db: db,
	}

	err = repo.initialize()
	if err != nil {
		db.Close()
		return nil, err
	}

	return repo, nil
}

// Save stores a download in the repository.
func (b *BboltRepository) Save(d Download) error {
	raw, err := d.MarshalJSON()
	if err != nil {
		return err
	}

	buf, err := json.Marshal(&Object{
		Type: d.Type(),
		Data: raw,
	})
	if err != nil {
		return err
	}

	return b.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(downloadsBucket))
		return b.Put([]byte(d.GetID().String()), buf)
	})
}

// GetAll retrieves all downloads.
func (b *BboltRepository) GetAll() (map[string]Object, error) {
	downloads := make(map[string]Object)

	err := b.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(downloadsBucket))

		return b.ForEach(func(k, v []byte) error {
			var obj Object

			err := json.Unmarshal(v, &obj)
			if err != nil {
				return err
			}

			downloads[string(k)] = obj

			return nil
		})
	})

	return downloads, err
}

// Delete removes a download.
func (b *BboltRepository) Delete(id string) error {
	return b.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(downloadsBucket))
		if bucket == nil {
			return fmt.Errorf("%w, bucket: %s", ErrBucketNotFound, downloadsBucket)
		}

		if bucket.Get([]byte(id)) == nil {
			return ErrDownloadNotFound
		}

		return bucket.Delete([]byte(id))
	})
}

// Close closes the database.
func (b *BboltRepository) Close() error {
	return b.db.Close()
}

// initialize sets up buckets and schema.
func (b *BboltRepository) initialize() error {
	return b.db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(downloadsBucket))
		if err != nil {
			return fmt.Errorf("failed to create downloads bucket: %w", err)
		}

		return nil
	})
}
