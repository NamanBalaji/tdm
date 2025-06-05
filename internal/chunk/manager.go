package chunk

import (
	"github.com/NamanBalaji/tdm/internal/common"
	"github.com/google/uuid"
)

// Manager defines the interface for managing chunks in a download process.
type Manager interface {
	// CreateChunks creates chunks for the given downloadID.
	CreateChunks(downloadID uuid.UUID, info *common.DownloadInfo, config *common.Config, progressFn func(int64)) ([]*Chunk, error)
	// MergeChunks merges the downloaded chunks into a meaningful structure.
	MergeChunks(chunks []*Chunk, targetPath string) error
	// CleanupChunks removes temporary files and cleans up resources used by the chunks.
	CleanupChunks(chunks []*Chunk) error
}
