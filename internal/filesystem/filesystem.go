package filesystem

import (
	"io"
	"os"
	"path/filepath"
)

// OSFileSystem implements the FileSystem interface using OS file operations
type OSFileSystem struct{}

// NewOSFileSystem creates a new OS filesystem
func NewOSFileSystem() *OSFileSystem {
	return &OSFileSystem{}
}

// CreateFile creates a new file
func (fs *OSFileSystem) CreateFile(path string) (io.WriteCloser, error) {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	return os.Create(path)
}

// OpenFile opens an existing file
func (fs *OSFileSystem) OpenFile(path string) (io.ReadCloser, error) {
	return os.Open(path)
}

// DeleteFile deletes a file
func (fs *OSFileSystem) DeleteFile(path string) error {
	return os.Remove(path)
}

// EnsureDirectory ensures a directory exists
func (fs *OSFileSystem) EnsureDirectory(path string) error {
	return os.MkdirAll(path, 0o755)
}

// FileExists checks if a file exists
func (fs *OSFileSystem) FileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// GetFileInfo gets file info
func (fs *OSFileSystem) GetFileInfo(path string) (os.FileInfo, error) {
	return os.Stat(path)
}
