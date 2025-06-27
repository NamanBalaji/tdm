package repository_test

//
//import (
//	"path/filepath"
//	"testing"
//
//	"github.com/google/uuid"
//
//	"github.com/NamanBalaji/tdm/internal/downloader"
//	"github.com/NamanBalaji/tdm/internal/repository"
//)
//
//func TestNewBboltRepository_OpenError(t *testing.T) {
//	dir := t.TempDir()
//	_, err := repository.NewBboltRepository(dir)
//	if err == nil {
//		t.Errorf("Expected error when opening DB on directory path, got nil")
//	}
//}
//
//func TestSaveNilDownload(t *testing.T) {
//	dbPath := filepath.Join(t.TempDir(), "test.db")
//	repo, err := repository.NewBboltRepository(dbPath)
//	if err != nil {
//		t.Fatalf("Failed to create repository: %v", err)
//	}
//	defer repo.Close()
//
//	err = repo.Save(nil)
//	if err == nil || err.Error() != "cannot save nil download" {
//		t.Errorf("Expected error 'cannot save nil download', got %v", err)
//	}
//}
//
//func TestSaveFindAllDelete(t *testing.T) {
//	dbPath := filepath.Join(t.TempDir(), "test.db")
//	repo, err := repository.NewBboltRepository(dbPath)
//	if err != nil {
//		t.Fatalf("Failed to create repository: %v", err)
//	}
//	defer repo.Close()
//
//	list, err := repo.FindAll()
//	if err != nil {
//		t.Fatalf("FindAll error: %v", err)
//	}
//	if len(list) != 0 {
//		t.Errorf("Expected empty list, got %d items", len(list))
//	}
//
//	id := uuid.New()
//	dl := &downloader.Download{ID: id}
//	err = repo.Save(dl)
//	if err != nil {
//		t.Fatalf("Save error: %v", err)
//	}
//
//	list, err = repo.FindAll()
//	if err != nil {
//		t.Fatalf("FindAll error: %v", err)
//	}
//	if len(list) != 1 || list[0].ID != id {
//		t.Errorf("FindAll returned wrong data: %+v", list)
//	}
//
//	err = repo.Delete(uuid.Nil)
//	if err == nil {
//		t.Errorf("Expected error deleting Nil Id, got nil")
//	}
//
//	randID := uuid.New()
//	err = repo.Delete(randID)
//	if err == nil {
//		t.Errorf("Expected error deleting non-existent Id, got nil")
//	}
//
//	err = repo.Delete(id)
//	if err != nil {
//		t.Errorf("Delete error for existing Id: %v", err)
//	}
//
//	list, err = repo.FindAll()
//	if err != nil {
//		t.Fatalf("FindAll error after delete: %v", err)
//	}
//	if len(list) != 0 {
//		t.Errorf("Expected empty list after delete, got %d items", len(list))
//	}
//}
//
//func TestCloseBehavior(t *testing.T) {
//	dbPath := filepath.Join(t.TempDir(), "test.db")
//	repo, err := repository.NewBboltRepository(dbPath)
//	if err != nil {
//		t.Fatalf("Failed to create repository: %v", err)
//	}
//
//	err = repo.Close()
//	if err != nil {
//		t.Fatalf("Close error: %v", err)
//	}
//
//	err = repo.Save(&downloader.Download{ID: uuid.New()})
//	if err == nil {
//		t.Errorf("Expected error Save after Close, got nil")
//	}
//	_, err = repo.FindAll()
//	if err == nil {
//		t.Errorf("Expected error FindAll after Close, got nil")
//	}
//
//	err = repo.Delete(uuid.New())
//	if err == nil {
//		t.Errorf("Expected error Delete after Close, got nil")
//	}
//}
