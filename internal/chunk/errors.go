package chunk

import "errors"

var (
	ErrInvalidChunkSize   = errors.New("invalid chunk size (must be between MinChunkSize and MaxChunkSize)")
	ErrMergeIncomplete    = errors.New("cannot merge incomplete download: a chunk is not completed")
	ErrChunkTempDirCreate = errors.New("failed to create chunk temp directory")
	ErrChunkFileOpen      = errors.New("failed to open chunk file")
	ErrChunkFileCopy      = errors.New("failed to copy chunk data")
	ErrTargetFileCreate   = errors.New("failed to create target file for merging")
	ErrChunkDirCreate     = errors.New("failed to create chunk directory")
	ErrChunkFileRemove    = errors.New("failed to remove chunk file during cleanup")
	ErrChunkFileWrite     = errors.New("failed to flush chunk data to file")
	ErrFileSeek           = errors.New("failed to seek in file")
	ErrFileWrite          = errors.New("failed to write to file")
)
