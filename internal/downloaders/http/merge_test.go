package http_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NamanBalaji/tdm/internal/config"
	httpdl "github.com/NamanBalaji/tdm/internal/downloaders/http"
)

func TestMerge(t *testing.T) {
	newDownloader := func() *httpdl.Downloader {
		return httpdl.New(&config.HTTPConfig{})
	}

	t.Run("single chunk merges correctly", func(t *testing.T) {
		tmpDir := t.TempDir()
		outDir := t.TempDir()

		content := []byte("single chunk content here")
		chunkFile := filepath.Join(tmpDir, "chunk0")
		require.NoError(t, os.WriteFile(chunkFile, content, 0o644))

		st := &httpdl.HttpState{
			Chunks: []httpdl.ChunkState{
				{
					ID:           uuid.New(),
					StartByte:    0,
					EndByte:      int64(len(content) - 1),
					TempFilePath: chunkFile,
				},
			},
			TempDir: tmpDir,
		}

		d := newDownloader()
		err := d.TestMerge(st, outDir, "output.bin")
		require.NoError(t, err)

		data, err := os.ReadFile(filepath.Join(outDir, "output.bin"))
		require.NoError(t, err)
		assert.Equal(t, content, data)
	})

	t.Run("multiple chunks in order", func(t *testing.T) {
		tmpDir := t.TempDir()
		outDir := t.TempDir()

		chunks := [][]byte{
			[]byte("AAAA"),
			[]byte("BBBB"),
			[]byte("CCCC"),
		}

		var states []httpdl.ChunkState
		var offset int64
		for i, c := range chunks {
			chunkFile := filepath.Join(tmpDir, fmt.Sprintf("chunk%d", i))
			require.NoError(t, os.WriteFile(chunkFile, c, 0o644))
			states = append(states, httpdl.ChunkState{
				ID:           uuid.New(),
				StartByte:    offset,
				EndByte:      offset + int64(len(c)) - 1,
				TempFilePath: chunkFile,
			})
			offset += int64(len(c))
		}

		st := &httpdl.HttpState{
			Chunks:  states,
			TempDir: tmpDir,
		}

		d := newDownloader()
		err := d.TestMerge(st, outDir, "output.bin")
		require.NoError(t, err)

		data, err := os.ReadFile(filepath.Join(outDir, "output.bin"))
		require.NoError(t, err)
		assert.Equal(t, []byte("AAAABBBBCCCC"), data)
	})

	t.Run("chunks out of order are sorted by start byte", func(t *testing.T) {
		tmpDir := t.TempDir()
		outDir := t.TempDir()

		chunkA := filepath.Join(tmpDir, "chunkA")
		chunkB := filepath.Join(tmpDir, "chunkB")
		chunkC := filepath.Join(tmpDir, "chunkC")
		require.NoError(t, os.WriteFile(chunkA, []byte("FIRST"), 0o644))
		require.NoError(t, os.WriteFile(chunkB, []byte("SECOND"), 0o644))
		require.NoError(t, os.WriteFile(chunkC, []byte("THIRD"), 0o644))

		// Provide out of order: C, A, B
		st := &httpdl.HttpState{
			Chunks: []httpdl.ChunkState{
				{ID: uuid.New(), StartByte: 11, EndByte: 15, TempFilePath: chunkC},
				{ID: uuid.New(), StartByte: 0, EndByte: 4, TempFilePath: chunkA},
				{ID: uuid.New(), StartByte: 5, EndByte: 10, TempFilePath: chunkB},
			},
			TempDir: tmpDir,
		}

		d := newDownloader()
		err := d.TestMerge(st, outDir, "output.bin")
		require.NoError(t, err)

		data, err := os.ReadFile(filepath.Join(outDir, "output.bin"))
		require.NoError(t, err)
		assert.Equal(t, "FIRSTSECONDTHIRD", string(data))
	})

	t.Run("missing chunk file returns error", func(t *testing.T) {
		outDir := t.TempDir()

		st := &httpdl.HttpState{
			Chunks: []httpdl.ChunkState{
				{ID: uuid.New(), StartByte: 0, EndByte: 10, TempFilePath: "/nonexistent/path/chunk"},
			},
			TempDir: "/tmp",
		}

		d := newDownloader()
		err := d.TestMerge(st, outDir, "output.bin")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to open chunk file")
	})

	t.Run("creates output directory if not exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		outDir := filepath.Join(t.TempDir(), "nested", "dir")

		content := []byte("content")
		chunkFile := filepath.Join(tmpDir, "chunk0")
		require.NoError(t, os.WriteFile(chunkFile, content, 0o644))

		st := &httpdl.HttpState{
			Chunks: []httpdl.ChunkState{
				{ID: uuid.New(), StartByte: 0, EndByte: int64(len(content) - 1), TempFilePath: chunkFile},
			},
			TempDir: tmpDir,
		}

		d := newDownloader()
		err := d.TestMerge(st, outDir, "output.bin")
		require.NoError(t, err)

		data, err := os.ReadFile(filepath.Join(outDir, "output.bin"))
		require.NoError(t, err)
		assert.Equal(t, content, data)
	})
}
