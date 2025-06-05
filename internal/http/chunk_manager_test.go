package http_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/chunk"
	"github.com/NamanBalaji/tdm/internal/common"
	httpChunk "github.com/NamanBalaji/tdm/internal/http"
)

func TestNewChunkManager(t *testing.T) {
	tests := []struct {
		name       string
		downloadID string
		tempDir    string
		expectErr  bool
		checkDir   bool
	}{
		{
			name:       "with custom temp directory",
			downloadID: "test-download-1",
			tempDir:    "",
			expectErr:  false,
			checkDir:   true,
		},
		{
			name:       "with specified temp directory",
			downloadID: "test-download-2",
			tempDir:    filepath.Join(os.TempDir(), "custom-tdm"),
			expectErr:  false,
			checkDir:   true,
		},
		{
			name:       "with empty download ID",
			downloadID: "",
			tempDir:    "",
			expectErr:  false,
			checkDir:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := tt.tempDir
			if tempDir == "" {
				tempDir = t.TempDir()
				tt.tempDir = tempDir
			}

			manager, err := httpChunk.NewChunkManager(tt.downloadID, tempDir)

			if tt.expectErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("NewChunkManager failed: %v", err)
			}

			if manager == nil {
				t.Fatal("Expected non-nil manager")
			}

			if tt.checkDir {
				if _, err := os.Stat(tempDir); os.IsNotExist(err) {
					t.Errorf("Expected temp directory %s to be created", tempDir)
				}
			}
		})
	}
}

func TestCreateChunks(t *testing.T) {
	tests := []struct {
		name            string
		fileSize        int64
		supportsRanges  bool
		maxConn         int
		expectedChunks  int
		checkSequential bool
	}{
		{
			name:            "small file with range support",
			fileSize:        100 * 1024,
			supportsRanges:  true,
			maxConn:         4,
			expectedChunks:  1,
			checkSequential: false,
		},
		{
			name:            "small file without range support",
			fileSize:        100 * 1024,
			supportsRanges:  false,
			maxConn:         4,
			expectedChunks:  1,
			checkSequential: true,
		},
		{
			name:            "large file with range support",
			fileSize:        10 * 1024 * 1024,
			supportsRanges:  true,
			maxConn:         4,
			expectedChunks:  4,
			checkSequential: false,
		},
		{
			name:            "large file without range support",
			fileSize:        10 * 1024 * 1024,
			supportsRanges:  false,
			maxConn:         4,
			expectedChunks:  1,
			checkSequential: true,
		},
		{
			name:            "file size causes chunk adjustment",
			fileSize:        2*256*1024 + 10,
			supportsRanges:  true,
			maxConn:         10,
			expectedChunks:  3,
			checkSequential: false,
		},
		{
			name:            "zero file size",
			fileSize:        0,
			supportsRanges:  true,
			maxConn:         4,
			expectedChunks:  1,
			checkSequential: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			downloadID := uuid.New()
			tempDir := t.TempDir()

			manager, err := httpChunk.NewChunkManager(downloadID.String(), tempDir)
			if err != nil {
				t.Fatalf("NewChunkManager failed: %v", err)
			}

			downloadInfo := &common.DownloadInfo{
				TotalSize:      tt.fileSize,
				SupportsRanges: tt.supportsRanges,
			}

			config := &common.Config{
				Connections: tt.maxConn,
			}

			progressCallCount := 0
			progressFn := func(int64) { progressCallCount++ }

			chunks, err := manager.CreateChunks(downloadID, downloadInfo, config, progressFn)
			if err != nil {
				t.Fatalf("CreateChunks failed: %v", err)
			}

			if len(chunks) != tt.expectedChunks {
				t.Errorf("Expected %d chunks, got %d", tt.expectedChunks, len(chunks))
			}

			var totalSize int64
			for i, c := range chunks {
				if c.DownloadID != downloadID {
					t.Errorf("Chunk %d has wrong download ID", i)
				}

				if c.TempFilePath == "" {
					t.Errorf("Chunk %d has empty temp file path", i)
				}

				expectedDir := filepath.Join(tempDir, downloadID.String())
				if !filepath.HasPrefix(c.TempFilePath, expectedDir) {
					t.Errorf("Chunk %d temp file path %s not in expected directory %s", i, c.TempFilePath, expectedDir)
				}

				if tt.checkSequential && c.SequentialDownload != tt.supportsRanges == false {
					t.Errorf("Chunk %d SequentialDownload=%v, expected %v", i, c.SequentialDownload, !tt.supportsRanges)
				}

				chunkSize := c.EndByte - c.StartByte + 1
				totalSize += chunkSize

				if tt.fileSize == 0 {
					if chunkSize != 0 {
						t.Errorf("Chunk %d should have size 0 for zero-sized file, got %d", i, chunkSize)
					}
				} else if chunkSize <= 0 {
					t.Errorf("Chunk %d has invalid size %d", i, chunkSize)
				}
			}

			if tt.fileSize == 0 {
				if totalSize != 0 {
					t.Errorf("Total chunk size %d should be 0 for zero-sized file", totalSize)
				}
			} else if totalSize != tt.fileSize {
				t.Errorf("Total chunk size %d doesn't match file size %d", totalSize, tt.fileSize)
			}

			if len(chunks) > 1 && tt.fileSize > 0 {
				for i := 1; i < len(chunks); i++ {
					prevEnd := chunks[i-1].EndByte
					currStart := chunks[i].StartByte

					if currStart != prevEnd+1 {
						t.Errorf("Gap between chunk %d (ends at %d) and chunk %d (starts at %d)", i-1, prevEnd, i, currStart)
					}
				}
			}

			chunkDir := filepath.Join(tempDir, downloadID.String())
			if _, err := os.Stat(chunkDir); os.IsNotExist(err) {
				t.Errorf("Expected chunk directory %s to be created", chunkDir)
			}
		})
	}
}

func TestMergeChunks(t *testing.T) {
	tests := []struct {
		name        string
		setupChunks func(t *testing.T, tempDir string, downloadID uuid.UUID) []*chunk.Chunk
		expectError bool
		checkResult func(t *testing.T, outputPath string)
	}{
		{
			name: "merge empty chunk list",
			setupChunks: func(t *testing.T, tempDir string, downloadID uuid.UUID) []*chunk.Chunk {
				return []*chunk.Chunk{}
			},
			expectError: false,
			checkResult: func(t *testing.T, outputPath string) {
				if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
					t.Error("Output file should not exist for empty chunk list")
				}
			},
		},
		{
			name: "merge single completed chunk",
			setupChunks: func(t *testing.T, tempDir string, downloadID uuid.UUID) []*chunk.Chunk {
				c := chunk.NewChunk(downloadID, 0, 9, nil)
				c.Status = common.StatusCompleted

				chunkDir := filepath.Join(tempDir, downloadID.String())
				os.MkdirAll(chunkDir, 0o755)

				chunkFile := filepath.Join(chunkDir, c.ID.String())
				c.TempFilePath = chunkFile

				err := os.WriteFile(chunkFile, []byte("0123456789"), 0o644)
				if err != nil {
					t.Fatalf("Failed to create chunk file: %v", err)
				}

				return []*chunk.Chunk{c}
			},
			expectError: false,
			checkResult: func(t *testing.T, outputPath string) {
				data, err := os.ReadFile(outputPath)
				if err != nil {
					t.Fatalf("Failed to read output file: %v", err)
				}
				if string(data) != "0123456789" {
					t.Errorf("Expected '0123456789', got %s", string(data))
				}
			},
		},
		{
			name: "merge multiple completed chunks",
			setupChunks: func(t *testing.T, tempDir string, downloadID uuid.UUID) []*chunk.Chunk {
				chunks := []*chunk.Chunk{}
				chunkDir := filepath.Join(tempDir, downloadID.String())
				os.MkdirAll(chunkDir, 0o755)

				data := []string{"chunk3", "chunk1", "chunk2"}
				starts := []int64{20, 0, 10}
				ends := []int64{25, 9, 19}

				for i, content := range data {
					c := chunk.NewChunk(downloadID, starts[i], ends[i], nil)
					c.Status = common.StatusCompleted

					chunkFile := filepath.Join(chunkDir, c.ID.String())
					c.TempFilePath = chunkFile

					err := os.WriteFile(chunkFile, []byte(content), 0o644)
					if err != nil {
						t.Fatalf("Failed to create chunk file %d: %v", i, err)
					}

					chunks = append(chunks, c)
				}

				return chunks
			},
			expectError: false,
			checkResult: func(t *testing.T, outputPath string) {
				data, err := os.ReadFile(outputPath)
				if err != nil {
					t.Fatalf("Failed to read output file: %v", err)
				}
				expected := "chunk1chunk2chunk3"
				if string(data) != expected {
					t.Errorf("Expected %s, got %s", expected, string(data))
				}
			},
		},
		{
			name: "attempt to merge incomplete chunk",
			setupChunks: func(t *testing.T, tempDir string, downloadID uuid.UUID) []*chunk.Chunk {
				c := chunk.NewChunk(downloadID, 0, 9, nil)
				c.Status = common.StatusPending

				chunkDir := filepath.Join(tempDir, downloadID.String())
				os.MkdirAll(chunkDir, 0o755)

				chunkFile := filepath.Join(chunkDir, c.ID.String())
				c.TempFilePath = chunkFile

				return []*chunk.Chunk{c}
			},
			expectError: true,
		},
		{
			name: "merge chunks with missing file",
			setupChunks: func(t *testing.T, tempDir string, downloadID uuid.UUID) []*chunk.Chunk {
				c := chunk.NewChunk(downloadID, 0, 9, nil)
				c.Status = common.StatusCompleted

				chunkDir := filepath.Join(tempDir, downloadID.String())
				c.TempFilePath = filepath.Join(chunkDir, c.ID.String())

				return []*chunk.Chunk{c}
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			downloadID := uuid.New()
			tempDir := t.TempDir()

			manager, err := httpChunk.NewChunkManager(downloadID.String(), tempDir)
			if err != nil {
				t.Fatalf("NewChunkManager failed: %v", err)
			}

			chunks := tt.setupChunks(t, tempDir, downloadID)
			outputPath := filepath.Join(tempDir, "output.bin")

			err = manager.MergeChunks(chunks, outputPath)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("MergeChunks failed: %v", err)
			}

			if tt.checkResult != nil {
				tt.checkResult(t, outputPath)
			}
		})
	}
}

func TestCleanupChunks(t *testing.T) {
	tests := []struct {
		name         string
		setupChunks  func(t *testing.T, tempDir string, downloadID uuid.UUID) []*chunk.Chunk
		expectError  bool
		checkCleanup func(t *testing.T, tempDir string, downloadID uuid.UUID)
	}{
		{
			name: "cleanup empty chunk list",
			setupChunks: func(t *testing.T, tempDir string, downloadID uuid.UUID) []*chunk.Chunk {
				return []*chunk.Chunk{}
			},
			expectError: false,
		},
		{
			name: "cleanup existing chunks",
			setupChunks: func(t *testing.T, tempDir string, downloadID uuid.UUID) []*chunk.Chunk {
				chunks := []*chunk.Chunk{}
				chunkDir := filepath.Join(tempDir, downloadID.String())
				os.MkdirAll(chunkDir, 0o755)

				for i := range 3 {
					c := chunk.NewChunk(downloadID, int64(i*10), int64(i*10+9), nil)
					chunkFile := filepath.Join(chunkDir, c.ID.String())
					c.TempFilePath = chunkFile

					err := os.WriteFile(chunkFile, []byte("test data"), 0o644)
					if err != nil {
						t.Fatalf("Failed to create chunk file %d: %v", i, err)
					}

					chunks = append(chunks, c)
				}

				return chunks
			},
			expectError: false,
			checkCleanup: func(t *testing.T, tempDir string, downloadID uuid.UUID) {
				chunkDir := filepath.Join(tempDir, downloadID.String())
				if _, err := os.Stat(chunkDir); !os.IsNotExist(err) {
					t.Error("Expected chunk directory to be removed after cleanup")
				}
			},
		},
		{
			name: "cleanup with some missing files",
			setupChunks: func(t *testing.T, tempDir string, downloadID uuid.UUID) []*chunk.Chunk {
				chunks := []*chunk.Chunk{}
				chunkDir := filepath.Join(tempDir, downloadID.String())
				os.MkdirAll(chunkDir, 0o755)

				for i := range 3 {
					c := chunk.NewChunk(downloadID, int64(i*10), int64(i*10+9), nil)
					chunkFile := filepath.Join(chunkDir, c.ID.String())
					c.TempFilePath = chunkFile

					// Only create file for first chunk
					if i == 0 {
						err := os.WriteFile(chunkFile, []byte("test data"), 0o644)
						if err != nil {
							t.Fatalf("Failed to create chunk file %d: %v", i, err)
						}
					}

					chunks = append(chunks, c)
				}

				return chunks
			},
			expectError: false, // Should not error on missing files
			checkCleanup: func(t *testing.T, tempDir string, downloadID uuid.UUID) {
				chunkDir := filepath.Join(tempDir, downloadID.String())
				if _, err := os.Stat(chunkDir); !os.IsNotExist(err) {
					t.Error("Expected chunk directory to be removed after cleanup")
				}
			},
		},
		{
			name: "cleanup with directory removal failure",
			setupChunks: func(t *testing.T, tempDir string, downloadID uuid.UUID) []*chunk.Chunk {
				c := chunk.NewChunk(downloadID, 0, 9, nil)

				chunkFile := filepath.Join(tempDir, "some-file")
				c.TempFilePath = chunkFile

				err := os.WriteFile(chunkFile, []byte("test"), 0o644)
				if err != nil {
					t.Fatalf("Failed to create chunk file: %v", err)
				}

				return []*chunk.Chunk{c}
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			downloadID := uuid.New()
			tempDir := t.TempDir()

			manager, err := httpChunk.NewChunkManager(downloadID.String(), tempDir)
			if err != nil {
				t.Fatalf("NewChunkManager failed: %v", err)
			}

			chunks := tt.setupChunks(t, tempDir, downloadID)

			err = manager.CleanupChunks(chunks)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if tt.checkCleanup != nil {
				tt.checkCleanup(t, tempDir, downloadID)
			}
		})
	}
}

func TestChunkManagerIntegration(t *testing.T) {
	downloadID := uuid.New()
	tempDir := t.TempDir()

	manager, err := httpChunk.NewChunkManager(downloadID.String(), tempDir)
	if err != nil {
		t.Fatalf("NewChunkManager failed: %v", err)
	}

	fileSize := int64(1024 * 1024)
	downloadInfo := &common.DownloadInfo{
		TotalSize:      fileSize,
		SupportsRanges: true,
	}
	config := &common.Config{
		Connections: 4,
	}

	chunks, err := manager.CreateChunks(downloadID, downloadInfo, config, nil)
	if err != nil {
		t.Fatalf("CreateChunks failed: %v", err)
	}

	if len(chunks) != 4 {
		t.Fatalf("Expected 4 chunks, got %d", len(chunks))
	}

	for i, c := range chunks {
		chunkSize := c.EndByte - c.StartByte + 1
		testData := make([]byte, chunkSize)
		for j := range testData {
			testData[j] = byte('A' + i)
		}

		err := os.WriteFile(c.TempFilePath, testData, 0o644)
		if err != nil {
			t.Fatalf("Failed to write chunk file %d: %v", i, err)
		}

		c.Status = common.StatusCompleted
	}

	outputPath := filepath.Join(tempDir, "merged.bin")
	err = manager.MergeChunks(chunks, outputPath)
	if err != nil {
		t.Fatalf("MergeChunks failed: %v", err)
	}

	mergedData, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read merged file: %v", err)
	}

	if int64(len(mergedData)) != fileSize {
		t.Errorf("Merged file size %d doesn't match expected size %d", len(mergedData), fileSize)
	}

	expectedPattern := []byte{'A', 'B', 'C', 'D'}
	chunkSize := fileSize / 4
	for i, expectedByte := range expectedPattern {
		start := int64(i) * chunkSize
		end := start + chunkSize
		if i == len(expectedPattern)-1 {
			end = fileSize
		}

		for j := start; j < end; j++ {
			if mergedData[j] != expectedByte {
				t.Errorf("At position %d: expected %c, got %c", j, expectedByte, mergedData[j])
				break
			}
		}
	}

	err = manager.CleanupChunks(chunks)
	if err != nil {
		t.Errorf("CleanupChunks failed: %v", err)
	}

	chunkDir := filepath.Join(tempDir, downloadID.String())
	if _, err := os.Stat(chunkDir); !os.IsNotExist(err) {
		t.Error("Expected chunk directory to be removed after cleanup")
	}

	if _, err := os.Stat(outputPath); err != nil {
		t.Error("Merged file should still exist after chunk cleanup")
	}
}
