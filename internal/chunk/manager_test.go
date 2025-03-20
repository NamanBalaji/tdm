package chunk_test

import (
	"github.com/NamanBalaji/tdm/internal/chunk"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewManager(t *testing.T) {
	tempDir := t.TempDir()
	mgr, err := chunk.NewManager(tempDir)
	if err != nil {
		t.Fatalf("failed to create chunk manager: %v", err)
	}
	if mgr == nil {
		t.Fatal("NewManager returned nil")
	}

	downloadID := uuid.New()
	filesize := int64(100 * 1024)
	chunks, err := mgr.CreateChunks(downloadID, filesize, true, 4)
	if err != nil {
		t.Fatalf("CreateChunks returned error: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0].TempFilePath, tempDir) {
		t.Errorf("TempFilePath %q does not contain tempDir %q", chunks[0].TempFilePath, tempDir)
	}
}

func TestSetDefaultChunkSize(t *testing.T) {
	tempDir := t.TempDir()
	mgr, err := chunk.NewManager(tempDir)
	if err != nil {
		t.Fatalf("failed to create chunk manager: %v", err)
	}

	err = mgr.SetDefaultChunkSize(chunk.MinChunkSize - 1)
	if err == nil {
		t.Error("expected error for chunk size below minimum")
	}

	err = mgr.SetDefaultChunkSize(chunk.MaxChunkSize + 1)
	if err == nil {
		t.Error("expected error for chunk size above maximum")
	}

	err = mgr.SetDefaultChunkSize(chunk.DefaultChunkSize)
	if err != nil {
		t.Errorf("unexpected error for valid chunk size: %v", err)
	}
}

func TestCreateChunks_SingleChunk(t *testing.T) {
	tempDir := t.TempDir()
	mgr, err := chunk.NewManager(tempDir)
	if err != nil {
		t.Fatalf("failed to create chunk manager: %v", err)
	}
	downloadID := uuid.New()
	filesize := int64(100 * 1024) // 100 KB (< 256 KB)
	chunks, err := mgr.CreateChunks(downloadID, filesize, true, 4)
	if err != nil {
		t.Fatalf("CreateChunks returned error: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].SequentialDownload {
		t.Error("expected SequentialDownload to be false when supportsRange is true")
	}
	if chunks[0].StartByte != 0 || chunks[0].EndByte != filesize-1 {
		t.Errorf("unexpected chunk boundaries: got %d-%d, expected 0-%d", chunks[0].StartByte, chunks[0].EndByte, filesize-1)
	}
}

func TestCreateChunks_MultipleChunks(t *testing.T) {
	tempDir := t.TempDir()
	mgr, err := chunk.NewManager(tempDir)
	if err != nil {
		t.Fatalf("failed to create chunk manager: %v", err)
	}
	downloadID := uuid.New()
	filesize := int64(10 * 1024 * 1024) // 10 MB
	maxConns := 4
	chunks, err := mgr.CreateChunks(downloadID, filesize, true, maxConns)
	if err != nil {
		t.Fatalf("CreateChunks returned error: %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	if chunks[0].StartByte != 0 {
		t.Errorf("first chunk should start at 0, got %d", chunks[0].StartByte)
	}
	lastChunk := chunks[len(chunks)-1]
	if lastChunk.EndByte != filesize-1 {
		t.Errorf("last chunk should end at %d, got %d", filesize-1, lastChunk.EndByte)
	}
	for i := 0; i < len(chunks)-1; i++ {
		if chunks[i].EndByte+1 != chunks[i+1].StartByte {
			t.Errorf("chunks not contiguous: chunk %d end %d, next start %d", i, chunks[i].EndByte, chunks[i+1].StartByte)
		}
	}
}

func TestMergeChunks_Success(t *testing.T) {
	tempDir := t.TempDir()
	mgr, err := chunk.NewManager(tempDir)
	if err != nil {
		t.Fatalf("failed to create chunk manager: %v", err)
	}
	downloadID := uuid.New()

	data1 := []byte("Hello ")
	data2 := []byte("World!")
	chunk1 := chunk.NewChunk(downloadID, 0, int64(len(data1))-1)
	chunk2 := chunk.NewChunk(downloadID, int64(len(data1)), int64(len(data1)+len(data2))-1)

	downloadTempDir := filepath.Join(tempDir, downloadID.String())
	err = os.MkdirAll(downloadTempDir, 0755)
	if err != nil {
		t.Fatalf("failed to create download temp directory: %v", err)
	}

	file1 := filepath.Join(downloadTempDir, chunk1.ID.String())
	file2 := filepath.Join(downloadTempDir, chunk2.ID.String())

	if err := os.WriteFile(file1, data1, 0644); err != nil {
		t.Fatalf("failed to write chunk1 file: %v", err)
	}
	if err := os.WriteFile(file2, data2, 0644); err != nil {
		t.Fatalf("failed to write chunk2 file: %v", err)
	}

	chunk1.TempFilePath = file1
	chunk2.TempFilePath = file2
	chunk1.Status = chunk.Completed
	chunk2.Status = chunk.Completed

	chunks := []*chunk.Chunk{chunk1, chunk2}
	targetFile := filepath.Join(tempDir, "merged.txt")
	err = mgr.MergeChunks(chunks, targetFile)
	if err != nil {
		t.Fatalf("MergeChunks returned error: %v", err)
	}

	mergedData, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("failed to read merged file: %v", err)
	}
	expected := string(data1) + string(data2)
	if string(mergedData) != expected {
		t.Errorf("expected merged data %q, got %q", expected, mergedData)
	}
}

func TestMergeChunks_IncompleteChunk(t *testing.T) {
	tempDir := t.TempDir()
	mgr, err := chunk.NewManager(tempDir)
	if err != nil {
		t.Fatalf("failed to create chunk manager: %v", err)
	}
	downloadID := uuid.New()
	data := []byte("Incomplete data")
	chunk1 := chunk.NewChunk(downloadID, 0, int64(len(data))-1)
	downloadTempDir := filepath.Join(tempDir, downloadID.String())
	err = os.MkdirAll(downloadTempDir, 0755)
	if err != nil {
		t.Fatalf("failed to create download temp directory: %v", err)
	}
	file1 := filepath.Join(downloadTempDir, chunk1.ID.String())
	if err := os.WriteFile(file1, data, 0644); err != nil {
		t.Fatalf("failed to write chunk file: %v", err)
	}
	chunk1.TempFilePath = file1
	chunk1.Status = chunk.Failed
	chunks := []*chunk.Chunk{chunk1}
	targetFile := filepath.Join(tempDir, "merged.txt")
	err = mgr.MergeChunks(chunks, targetFile)
	if err == nil {
		t.Error("expected error when merging incomplete chunks, got nil")
	}
}

func TestCleanupChunks(t *testing.T) {
	tempDir := t.TempDir()
	mgr, err := chunk.NewManager(tempDir)
	if err != nil {
		t.Fatalf("failed to create chunk manager: %v", err)
	}
	downloadID := uuid.New()
	downloadTempDir := filepath.Join(tempDir, downloadID.String())

	err = os.MkdirAll(downloadTempDir, 0755)
	if err != nil {
		t.Fatalf("failed to create download temp directory: %v", err)
	}
	chunk1 := chunk.NewChunk(downloadID, 0, 99)
	file1 := filepath.Join(downloadTempDir, chunk1.ID.String())
	if err := os.WriteFile(file1, []byte("dummy"), 0644); err != nil {
		t.Fatalf("failed to write dummy chunk file: %v", err)
	}
	chunk1.TempFilePath = file1
	chunks := []*chunk.Chunk{chunk1}
	err = mgr.CleanupChunks(chunks)
	if err != nil {
		t.Fatalf("CleanupChunks returned error: %v", err)
	}
	if _, err := os.Stat(file1); !os.IsNotExist(err) {
		t.Errorf("expected chunk file %q to be removed", file1)
	}
	if _, err := os.Stat(downloadTempDir); !os.IsNotExist(err) {
		t.Errorf("expected download directory %q to be removed", downloadTempDir)
	}
}

func TestCleanupChunks_Empty(t *testing.T) {
	tempDir := t.TempDir()
	mgr, err := chunk.NewManager(tempDir)
	if err != nil {
		t.Fatalf("failed to create chunk manager: %v", err)
	}
	err = mgr.CleanupChunks(nil)
	if err != nil {
		t.Errorf("expected nil error when cleaning up empty chunk slice, got %v", err)
	}
}
