package repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/NamanBalaji/tdm/internal/downloader"
	"github.com/google/uuid"

	"go.etcd.io/bbolt"
)

const (
	downloadsBucket = "downloads"
	metadataBucket  = "metadata"
	schemaVersion   = 1
)

var (
	// ErrDownloadNotFound is returned when a download cannot be found
	ErrDownloadNotFound = errors.New("download not found")
)

// BboltRepository implements the downloader.DownloadRepository interface
type BboltRepository struct {
	db *bbolt.DB
}

// NewBboltRepository creates a new bbolt repository
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

	if err := repo.initialize(); err != nil {
		db.Close()
		return nil, err
	}

	return repo, nil
}

// initialize sets up buckets and schema
func (r *BboltRepository) initialize() error {
	return r.db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(downloadsBucket))
		if err != nil {
			return fmt.Errorf("failed to create downloads bucket: %w", err)
		}

		metadataBucket, err := tx.CreateBucketIfNotExists([]byte(metadataBucket))
		if err != nil {
			return fmt.Errorf("failed to create metadata bucket: %w", err)
		}

		versionBytes := []byte(fmt.Sprintf("%d", schemaVersion))
		err = metadataBucket.Put([]byte("schema_version"), versionBytes)
		if err != nil {
			return fmt.Errorf("failed to store schema version: %w", err)
		}

		return nil
	})
}

// Save persists a download to storage
func (r *BboltRepository) Save(download *downloader.Download) error {
	if download == nil {
		return errors.New("cannot save nil download")
	}

	download.PrepareForSerialization()

	return r.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(downloadsBucket))
		if bucket == nil {
			return fmt.Errorf("bucket not found: %s", downloadsBucket)
		}

		data, err := json.Marshal(download)
		if err != nil {
			return fmt.Errorf("failed to marshal download: %w", err)
		}

		err = bucket.Put([]byte(download.ID.String()), data)
		if err != nil {
			return fmt.Errorf("failed to save download: %w", err)
		}

		return nil
	})
}

// Find retrieves a download by ID
func (r *BboltRepository) Find(id uuid.UUID) (*downloader.Download, error) {
	if id == uuid.Nil {
		return nil, errors.New("download ID cannot be empty")
	}

	var data []byte
	err := r.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(downloadsBucket))
		if bucket == nil {
			return fmt.Errorf("bucket not found: %s", downloadsBucket)
		}

		// Get data from the database
		data = bucket.Get([]byte(id.String()))
		if data == nil {
			return ErrDownloadNotFound
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	download := &downloader.Download{}

	if err := json.Unmarshal(data, download); err != nil {
		return nil, fmt.Errorf("failed to unmarshal download: %w", err)
	}

	download.RestoreFromSerialization()

	return download, nil
}

// FindAll retrieves all downloads
func (r *BboltRepository) FindAll() ([]*downloader.Download, error) {
	var downloads []*downloader.Download

	err := r.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(downloadsBucket))
		if bucket == nil {
			return fmt.Errorf("bucket not found: %s", downloadsBucket)
		}

		return bucket.ForEach(func(k, v []byte) error {
			download := &downloader.Download{}

			if err := json.Unmarshal(v, download); err != nil {
				return fmt.Errorf("failed to unmarshal download: %w", err)
			}

			download.RestoreFromSerialization()

			downloads = append(downloads, download)
			return nil
		})
	})

	if err != nil {
		return nil, err
	}

	return downloads, nil
}

// Delete removes a download
func (r *BboltRepository) Delete(id uuid.UUID) error {
	if id == uuid.Nil {
		return errors.New("download ID cannot be empty")
	}

	return r.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(downloadsBucket))
		if bucket == nil {
			return fmt.Errorf("bucket not found: %s", downloadsBucket)
		}

		if bucket.Get([]byte(id.String())) == nil {
			return errors.New("download not found")
		}

		return bucket.Delete([]byte(id.String()))
	})
}

// Close closes the database
func (r *BboltRepository) Close() error {
	return r.db.Close()
}
