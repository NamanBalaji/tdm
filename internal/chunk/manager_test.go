package chunk_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/chunk"
	"github.com/NamanBalaji/tdm/internal/common"
)

func TestNewManager_CustomDir(t *testing.T) {
	id := uuid.New()
	tmp := t.TempDir()
	m, err := chunk.NewManager(id.String(), tmp)
	if err != nil {
		t.Fatalf("Expected no error creating manager, got %v", err)
	}
	_, err = m.CreateChunks(id, chunk.MinChunkSize, false, 1, nil)
	if err != nil {
		t.Fatalf("Expected CreateChunks to succeed, got %v", err)
	}
	sub := filepath.Join(tmp, id.String())
	if fi, err := os.Stat(sub); err != nil || !fi.IsDir() {
		t.Errorf("Expected temp subdir %s to exist, got error %v", sub, err)
	}
}

func TestSetDefaultChunkSize(t *testing.T) {
	m, _ := chunk.NewManager("id", t.TempDir())
	if err := m.SetDefaultChunkSize(chunk.MinChunkSize); err != nil {
		t.Errorf("Expected no error for valid size, got %v", err)
	}
	if err := m.SetDefaultChunkSize(chunk.MinChunkSize - 1); !errors.Is(err, chunk.ErrInvalidChunkSize) {
		t.Errorf("Expected ErrInvalidChunkSize for too small size, got %v", err)
	}
	if err := m.SetDefaultChunkSize(chunk.MaxChunkSize + 1); !errors.Is(err, chunk.ErrInvalidChunkSize) {
		t.Errorf("Expected ErrInvalidChunkSize for too large size, got %v", err)
	}
}

func TestCreateChunks_SingleChunkCases(t *testing.T) {
	dlID := uuid.New()
	m, _ := chunk.NewManager(dlID.String(), t.TempDir())
	chunks, err := m.CreateChunks(dlID, chunk.DefaultChunkSize*2, false, 5, nil)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if len(chunks) != 1 {
		t.Errorf("Expected 1 chunk when no range, got %d", len(chunks))
	}
	if !chunks[0].SequentialDownload {
		t.Errorf("Expected SequentialDownload true when no range")
	}
	small := chunk.MinChunkSize - 10
	chunks2, err := m.CreateChunks(dlID, small, true, 3, nil)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if len(chunks2) != 1 {
		t.Errorf("Expected 1 chunk for small file, got %d", len(chunks2))
	}
	if chunks2[0].SequentialDownload {
		t.Errorf("Expected SequentialDownload false when range supported even if small file")
	}
}

func TestCreateChunks_MultipleChunksAndMinAdjust(t *testing.T) {
	dlID := uuid.New()
	m, _ := chunk.NewManager(dlID.String(), t.TempDir())
	filesize := chunk.DefaultChunkSize * 3
	chs, err := m.CreateChunks(dlID, filesize, true, 3, nil)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if len(chs) != 3 {
		t.Errorf("Expected 3 chunks, got %d", len(chs))
	}
	var total int64
	for _, c := range chs {
		total += c.EndByte - c.StartByte + 1
	}
	if total != filesize {
		t.Errorf("Chunks total size %d does not equal filesize %d", total, filesize)
	}
	smallSize := chunk.MinChunkSize*2 + 10
	chs2, err := m.CreateChunks(dlID, smallSize, true, 10, nil)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	expected := int((smallSize + chunk.MinChunkSize - 1) / chunk.MinChunkSize)
	if len(chs2) != expected {
		t.Errorf("Expected %d chunks after adjust, got %d", expected, len(chs2))
	}
}

func TestMergeChunks(t *testing.T) {
	dlID := uuid.New()
	tmp := t.TempDir()
	m, _ := chunk.NewManager(dlID.String(), tmp)
	if err := m.MergeChunks(nil, filepath.Join(tmp, "out.bin")); err != nil {
		t.Errorf("Expected no error merging empty, got %v", err)
	}
	c := &chunk.Chunk{DownloadID: dlID, Status: common.StatusPending}
	if err := m.MergeChunks([]*chunk.Chunk{c}, filepath.Join(tmp, "out.bin")); !errors.Is(err, chunk.ErrMergeIncomplete) {
		t.Errorf("Expected ErrMergeIncomplete, got %v", err)
	}
	var files []string
	var chunks []*chunk.Chunk
	for i := range 2 {
		ch := &chunk.Chunk{DownloadID: dlID, Status: common.StatusCompleted}
		path := filepath.Join(tmp, dlID.String(), uuid.New().String())
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		content := []byte{byte('A' + i)}
		err := os.WriteFile(path, content, 0o644)
		if err != nil {
			t.Fatalf("failed write chunk file: %v", err)
		}
		ch.TempFilePath = path
		chunks = append(chunks, ch)
		files = append(files, string(content))
	}
	out := filepath.Join(tmp, "merged.bin")
	err := m.MergeChunks(chunks, out)
	if err != nil {
		t.Fatalf("Expected merge success, got %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read merged file: %v", err)
	}
	if string(data) != files[0]+files[1] {
		t.Errorf("Merged content mismatch: got %s", string(data))
	}
}

func TestCleanupChunks(t *testing.T) {
	dlID := uuid.New()
	tmp := t.TempDir()
	m, _ := chunk.NewManager(dlID.String(), tmp)
	if err := m.CleanupChunks(nil); err != nil {
		t.Errorf("Expected no error cleaning empty, got %v", err)
	}
	var chunks []*chunk.Chunk
	for range 2 {
		ch := &chunk.Chunk{DownloadID: dlID}
		path := filepath.Join(tmp, dlID.String(), uuid.New().String())
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte{1, 2, 3}, 0o644); err != nil {
			t.Fatalf("write chunk file: %v", err)
		}
		ch.TempFilePath = path
		chunks = append(chunks, ch)
	}
	dir := filepath.Join(tmp, dlID.String())
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("dir missing before cleanup: %v", err)
	}
	err := m.CleanupChunks(chunks)
	if err != nil {
		t.Errorf("Expected cleanup no error, got %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("Expected dir removed, got %v", err)
	}
}
