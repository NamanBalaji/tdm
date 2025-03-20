package repository_test

import (
	"errors"
	"github.com/NamanBalaji/tdm/internal/downloader"
	"github.com/NamanBalaji/tdm/internal/repository"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestRepository(t *testing.T) *repository.BoltDBRepository {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	repo, err := repository.NewBoltDBRepository(dbPath)
	if err != nil {
		t.Fatalf("failed to create repository: %v", err)
	}
	return repo
}

func TestNewBoltDBRepository(t *testing.T) {
	repo := newTestRepository(t)
	defer repo.Close()

	if repo == nil {
		t.Fatal("expected a valid repository, got nil")
	}
}

func TestSaveAndFindDownload(t *testing.T) {
	repo := newTestRepository(t)
	defer repo.Close()

	download := &downloader.Download{
		ID: uuid.New(),
	}

	// Save the download.
	if err := repo.Save(download); err != nil {
		t.Fatalf("failed to save download: %v", err)
	}

	// Retrieve the download by ID.
	found, err := repo.Find(download.ID)
	if err != nil {
		t.Fatalf("failed to find download: %v", err)
	}

	if found.ID != download.ID {
		t.Errorf("expected download ID %v, got %v", download.ID, found.ID)
	}
}

func TestFindAllDownloads(t *testing.T) {
	repo := newTestRepository(t)
	defer repo.Close()

	// Create several dummy downloads.
	downloads := []*downloader.Download{
		{ID: uuid.New()},
		{ID: uuid.New()},
		{ID: uuid.New()},
	}

	// Save all downloads.
	for _, d := range downloads {
		if err := repo.Save(d); err != nil {
			t.Fatalf("failed to save download with ID %v: %v", d.ID, err)
		}
	}

	// Retrieve all downloads.
	found, err := repo.FindAll()
	if err != nil {
		t.Fatalf("failed to find all downloads: %v", err)
	}

	if len(found) != len(downloads) {
		t.Errorf("expected %d downloads, found %d", len(downloads), len(found))
	}

	// Verify that each saved download is present.
	idMap := make(map[uuid.UUID]bool)
	for _, d := range downloads {
		idMap[d.ID] = true
	}
	for _, d := range found {
		if !idMap[d.ID] {
			t.Errorf("found unexpected download with ID %v", d.ID)
		}
	}
}

func TestDeleteDownload(t *testing.T) {
	repo := newTestRepository(t)
	defer repo.Close()

	download := &downloader.Download{ID: uuid.New()}

	if err := repo.Save(download); err != nil {
		t.Fatalf("failed to save download: %v", err)
	}

	if err := repo.Delete(download.ID); err != nil {
		t.Fatalf("failed to delete download: %v", err)
	}

	_, err := repo.Find(download.ID)
	if err == nil {
		t.Fatal("expected an error when finding a deleted download, got nil")
	}
	if !strings.Contains(err.Error(), "download not found") && !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected error to indicate 'download not found', got: %v", err)
	}
}

func TestCloseRepository(t *testing.T) {
	repo := newTestRepository(t)

	if err := repo.Close(); err != nil {
		t.Errorf("failed to close repository: %v", err)
	}

	_, err := repo.Find(uuid.New())
	if err == nil {
		t.Error("expected an error after closing the repository, got nil")
	}
}
