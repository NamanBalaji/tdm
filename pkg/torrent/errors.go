package torrent

import (
	"errors"
	"fmt"
)

// Sentinel errors for validation.
var (
	ErrInvalidTorrentStructure = errors.New("invalid torrent structure")
	ErrInvalidAnnounceURL      = errors.New("invalid announce URL")
	ErrInvalidAnnounceList     = errors.New("invalid announce list")
	ErrInvalidInfoDict         = errors.New("invalid info dictionary")
	ErrInvalidName             = errors.New("invalid name")
	ErrInvalidPieceLength      = errors.New("invalid piece length")
	ErrInvalidPieces           = errors.New("invalid pieces")
	ErrInvalidFileStructure    = errors.New("invalid file structure")
	ErrInvalidSingleFile       = errors.New("invalid single file")
	ErrInvalidMultiFile        = errors.New("invalid multi file")
	ErrInvalidFilePath         = errors.New("invalid file path")
	ErrInconsistentData        = errors.New("inconsistent data")
)

// ValidationError wraps sentinel errors with custom messages.
type ValidationError struct {
	Type    error  // Sentinel error type
	Field   string // Field that caused the error
	Message string // Custom message
}

func (e *ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%v in field '%s': %s", e.Type, e.Field, e.Message)
	}

	return fmt.Sprintf("%v: %s", e.Type, e.Message)
}

func (e *ValidationError) Unwrap() error {
	return e.Type
}

// Helper function to create validation errors.
func newValidationError(errType error, field, message string) *ValidationError {
	return &ValidationError{
		Type:    errType,
		Field:   field,
		Message: message,
	}
}
