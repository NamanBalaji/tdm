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
	schemaVersion   = 1 // For future schema migrations
)

// BboltRepository implements the downloader.DownloadRepository interface
type BboltRepository struct {
	db *bbolt.DB
}

// NewBboltRepository creates a new bbolt repository
func NewBboltRepository(dbPath string) (*BboltRepository, error) {
	// Create options for bbolt
	options := &bbolt.Options{
		Timeout: 1 * time.Second,
	}

	// Open the database with bbolt
	db, err := bbolt.Open(dbPath, 0o600, options)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Initialize the repository
	repo := &BboltRepository{
		db: db,
	}

	// Create buckets and initialize schema
	if err := repo.initialize(); err != nil {
		// Close DB if initialization fails
		db.Close()
		return nil, err
	}

	return repo, nil
}

// initialize sets up buckets and schema
func (r *BboltRepository) initialize() error {
	return r.db.Update(func(tx *bbolt.Tx) error {
		// Create downloads bucket
		_, err := tx.CreateBucketIfNotExists([]byte(downloadsBucket))
		if err != nil {
			return fmt.Errorf("failed to create downloads bucket: %w", err)
		}

		// Create metadata bucket
		metadataBucket, err := tx.CreateBucketIfNotExists([]byte(metadataBucket))
		if err != nil {
			return fmt.Errorf("failed to create metadata bucket: %w", err)
		}

		// Store schema version in metadata
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

	return r.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(downloadsBucket))
		if bucket == nil {
			return fmt.Errorf("bucket not found: %s", downloadsBucket)
		}

		// Serialize the download - errors handled by marshaling methods
		data, err := json.Marshal(download)
		if err != nil {
			return fmt.Errorf("failed to marshal download: %w", err)
		}

		// Save to the database
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
			return errors.New("download not found")
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Create new download instance
	download := &downloader.Download{}

	// Deserialize the download
	if err := json.Unmarshal(data, download); err != nil {
		return nil, fmt.Errorf("failed to unmarshal download: %w", err)
	}

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

		// Iterate through all downloads
		return bucket.ForEach(func(k, v []byte) error {
			// Create a new download for each entry
			download := &downloader.Download{}

			// Deserialize
			if err := json.Unmarshal(v, download); err != nil {
				return fmt.Errorf("failed to unmarshal download: %w", err)
			}

			// Add to results
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

		// Check if it exists first
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
