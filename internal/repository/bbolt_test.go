package repository_test

//
// import (
//	"encoding/json"
//	"path/filepath"
//	"strings"
//	"testing"
//
//	"github.com/NamanBalaji/tdm/internal/downloader"
//	"github.com/NamanBalaji/tdm/internal/repository"
//	"github.com/google/uuid"
//)
//
//func newTestRepository(t *testing.T) *repository.BboltRepository {
//	t.Helper()
//	tmpDir := t.TempDir()
//	dbPath := filepath.Join(tmpDir, "test.db")
//	repo, err := repository.NewBboltRepository(dbPath)
//	if err != nil {
//		t.Fatalf("failed to create repository: %v", err)
//	}
//	return repo
//}
//
//func TestNewBboltRepository(t *testing.T) {
//	repo := newTestRepository(t)
//	defer repo.Close()
//
//	// Verify that a valid repository instance is returned.
//	if repo == nil {
//		t.Fatal("expected repository to be non-nil")
//	}
//
//	// Use a public method to verify the repository works.
//	// For example, try to find a download with an empty UUID, which should return an error.
//	_, err := repo.Find(uuid.Nil)
//	if err == nil {
//		t.Error("expected error when finding download with empty UUID, got nil")
//	}
//}
//
//func TestSaveAndFindDownload(t *testing.T) {
//	repo := newTestRepository(t)
//	defer repo.Close()
//
//	d := &downloader.Download{
//		ID: uuid.New(),
//	}
//
//	if err := repo.Save(d); err != nil {
//		t.Fatalf("failed to save download: %v", err)
//	}
//
//	found, err := repo.Find(d.ID)
//	if err != nil {
//		t.Fatalf("failed to find download: %v", err)
//	}
//
//	if found.ID != d.ID {
//		t.Errorf("expected download ID %v, got %v", d.ID, found.ID)
//	}
//
//	orig, _ := json.Marshal(d)
//	retrieved, _ := json.Marshal(found)
//	if string(orig) != string(retrieved) {
//		t.Errorf("saved and retrieved downloads differ\nexpected: %s\ngot: %s", orig, retrieved)
//	}
//}
//
//func TestFindAllDownloads(t *testing.T) {
//	repo := newTestRepository(t)
//	defer repo.Close()
//
//	downloads := []*downloader.Download{
//		{ID: uuid.New()},
//		{ID: uuid.New()},
//		{ID: uuid.New()},
//	}
//
//	for _, d := range downloads {
//		if err := repo.Save(d); err != nil {
//			t.Fatalf("failed to save download with ID %v: %v", d.ID, err)
//		}
//	}
//
//	all, err := repo.FindAll()
//	if err != nil {
//		t.Fatalf("failed to find all downloads: %v", err)
//	}
//
//	if len(all) != len(downloads) {
//		t.Errorf("expected %d downloads, got %d", len(downloads), len(all))
//	}
//
//	idMap := make(map[uuid.UUID]bool)
//	for _, d := range downloads {
//		idMap[d.ID] = true
//	}
//	for _, d := range all {
//		if !idMap[d.ID] {
//			t.Errorf("found unexpected download with ID %v", d.ID)
//		}
//	}
//}
//
//func TestDeleteDownload(t *testing.T) {
//	repo := newTestRepository(t)
//	defer repo.Close()
//
//	d := &downloader.Download{
//		ID: uuid.New(),
//	}
//
//	if err := repo.Save(d); err != nil {
//		t.Fatalf("failed to save download: %v", err)
//	}
//
//	if err := repo.Delete(d.ID); err != nil {
//		t.Fatalf("failed to delete download: %v", err)
//	}
//
//	_, err := repo.Find(d.ID)
//	if err == nil {
//		t.Fatal("expected error when finding deleted download, got nil")
//	}
//	if !strings.Contains(err.Error(), "download not found") {
//		t.Errorf("expected error to mention 'download not found', got: %v", err)
//	}
//}
//
//func TestSaveNilDownload(t *testing.T) {
//	repo := newTestRepository(t)
//	defer repo.Close()
//
//	err := repo.Save(nil)
//	if err == nil {
//		t.Fatal("expected error when saving nil download, got nil")
//	}
//}
//
//func TestFindEmptyID(t *testing.T) {
//	repo := newTestRepository(t)
//	defer repo.Close()
//
//	_, err := repo.Find(uuid.Nil)
//	if err == nil {
//		t.Fatal("expected error when finding download with empty UUID, got nil")
//	}
//}
//
//func TestDeleteEmptyID(t *testing.T) {
//	repo := newTestRepository(t)
//	defer repo.Close()
//
//	err := repo.Delete(uuid.Nil)
//	if err == nil {
//		t.Fatal("expected error when deleting download with empty UUID, got nil")
//	}
//}
//
//func TestCloseRepository(t *testing.T) {
//	repo := newTestRepository(t)
//	if err := repo.Close(); err != nil {
//		t.Fatalf("failed to close repository: %v", err)
//	}
//
//	d := &downloader.Download{
//		ID: uuid.New(),
//	}
//	err := repo.Save(d)
//	if err == nil {
//		t.Error("expected error when saving after closing repository, got nil")
//	}
//}
