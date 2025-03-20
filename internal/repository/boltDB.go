package repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/NamanBalaji/tdm/internal/downloader"
	"github.com/google/uuid"

	"github.com/boltdb/bolt"
)

const (
	downloadsBucket = "downloads"
	metadataBucket  = "metadata"
)

// BoltDBRepository implements the DownloadRepository interface using BoltDB
type BoltDBRepository struct {
	db *bolt.DB
}

// NewBoltDBRepository creates a new BoltDB repository
func NewBoltDBRepository(dbPath string) (*BoltDBRepository, error) {
	// Open the database
	db, err := bolt.Open(dbPath, 0o600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Create buckets if they don't exist
	err = db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists([]byte(downloadsBucket)); err != nil {
			return fmt.Errorf("failed to create downloads bucket: %w", err)
		}
		if _, err := tx.CreateBucketIfNotExists([]byte(metadataBucket)); err != nil {
			return fmt.Errorf("failed to create metadata bucket: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create buckets: %w", err)
	}

	return &BoltDBRepository{
		db: db,
	}, nil
}

// Save persists a download to storage
func (r *BoltDBRepository) Save(download *downloader.Download) error {
	return r.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(downloadsBucket))
		if bucket == nil {
			return fmt.Errorf("bucket not found: %s", downloadsBucket)
		}

		// Serialize the download
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
func (r *BoltDBRepository) Find(id uuid.UUID) (*downloader.Download, error) {
	var download *downloader.Download

	err := r.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(downloadsBucket))
		if bucket == nil {
			return fmt.Errorf("bucket not found: %s", downloadsBucket)
		}

		// Get data from the database
		data := bucket.Get([]byte(id.String()))
		if data == nil {
			return errors.New("download not found")
		}

		// Deserialize the download
		return json.Unmarshal(data, &download)
	})

	if err != nil {
		return nil, err
	}

	return download, nil
}

// FindAll retrieves all downloads
func (r *BoltDBRepository) FindAll() ([]*downloader.Download, error) {
	var downloads []*downloader.Download

	err := r.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(downloadsBucket))
		if bucket == nil {
			return fmt.Errorf("bucket not found: %s", downloadsBucket)
		}

		// Iterate through all downloads
		return bucket.ForEach(func(k, v []byte) error {
			var download downloader.Download
			if err := json.Unmarshal(v, &download); err != nil {
				return fmt.Errorf("failed to unmarshal download: %w", err)
			}
			downloads = append(downloads, &download)
			return nil
		})
	})

	if err != nil {
		return nil, err
	}

	return downloads, nil
}

// Delete removes a download
func (r *BoltDBRepository) Delete(id uuid.UUID) error {
	return r.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(downloadsBucket))
		if bucket == nil {
			return fmt.Errorf("bucket not found: %s", downloadsBucket)
		}

		return bucket.Delete([]byte(id.String()))
	})
}

// Close closes the database
func (r *BoltDBRepository) Close() error {
	return r.db.Close()
}
