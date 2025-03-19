package filesystem_test

import (
	"github.com/NamanBalaji/tdm/internal/filesystem"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateFile(t *testing.T) {
	fs := filesystem.NewOSFileSystem()
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "subdir", "testfile.txt")

	file, err := fs.CreateFile(filePath)
	if err != nil {
		t.Fatalf("CreateFile failed: %v", err)
	}

	content := []byte("hello world")
	if _, err := file.Write(content); err != nil {
		t.Fatalf("Writing to file failed: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Closing file failed: %v", err)
	}

	exists, err := fs.FileExists(filePath)
	if err != nil {
		t.Fatalf("FileExists failed: %v", err)
	}
	if !exists {
		t.Fatalf("Expected file to exist after creation")
	}
}

func TestOpenFile(t *testing.T) {
	fs := filesystem.NewOSFileSystem()
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "testfile.txt")

	content := []byte("test content")
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	file, err := fs.OpenFile(filePath)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}
	defer file.Close()

	readContent, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("Reading file failed: %v", err)
	}

	if string(readContent) != string(content) {
		t.Errorf("Expected content %q, got %q", content, readContent)
	}
}

func TestDeleteFile(t *testing.T) {
	fs := filesystem.NewOSFileSystem()
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "testfile.txt")

	if err := os.WriteFile(filePath, []byte("to be deleted"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	if err := fs.DeleteFile(filePath); err != nil {
		t.Fatalf("DeleteFile failed: %v", err)
	}

	exists, err := fs.FileExists(filePath)
	if err != nil {
		t.Fatalf("FileExists failed: %v", err)
	}
	if exists {
		t.Fatalf("Expected file to be deleted")
	}
}

func TestEnsureDirectory(t *testing.T) {
	fs := filesystem.NewOSFileSystem()
	tempDir := t.TempDir()
	dirPath := filepath.Join(tempDir, "newdir")

	if err := fs.EnsureDirectory(dirPath); err != nil {
		t.Fatalf("EnsureDirectory failed: %v", err)
	}

	info, err := os.Stat(dirPath)
	if err != nil {
		t.Fatalf("Stat on directory failed: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("Expected %s to be a directory", dirPath)
	}
}

func TestFileExists(t *testing.T) {
	fs := filesystem.NewOSFileSystem()
	tempDir := t.TempDir()
	existingFile := filepath.Join(tempDir, "existing.txt")
	nonExistingFile := filepath.Join(tempDir, "nonexisting.txt")

	if err := os.WriteFile(existingFile, []byte("data"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	exists, err := fs.FileExists(existingFile)
	if err != nil {
		t.Fatalf("FileExists failed: %v", err)
	}
	if !exists {
		t.Fatalf("Expected file %s to exist", existingFile)
	}

	exists, err = fs.FileExists(nonExistingFile)
	if err != nil {
		t.Fatalf("FileExists failed for non-existing file: %v", err)
	}
	if exists {
		t.Fatalf("Expected file %s to not exist", nonExistingFile)
	}
}

func TestGetFileInfo(t *testing.T) {
	fs := filesystem.NewOSFileSystem()
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "testfile.txt")

	if err := os.WriteFile(filePath, []byte("info"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	info, err := fs.GetFileInfo(filePath)
	if err != nil {
		t.Fatalf("GetFileInfo failed: %v", err)
	}
	if info.Name() != "testfile.txt" {
		t.Errorf("Expected file name 'testfile.txt', got %s", info.Name())
	}
}
