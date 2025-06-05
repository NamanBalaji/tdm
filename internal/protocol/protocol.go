package protocol

import (
	"context"
	"errors"
	"sync"

	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/chunk"
	"github.com/NamanBalaji/tdm/internal/common"
	"github.com/NamanBalaji/tdm/internal/connection"
	httpPkg "github.com/NamanBalaji/tdm/internal/http"
)

var (
	ErrUnsupportedProtocol = errors.New("unsupported protocol")
	ErrInvalidURL          = errors.New("invalid URL")
)

// Protocol defines the interface for handling different protocols.
type Protocol interface {
	// CanHandle checks if this handler can handle the given URL
	CanHandle(url string) bool
	// Initialize gathers information about the download resource
	Initialize(ctx context.Context, url string, config *common.Config) (*common.DownloadInfo, error)
	// CreateConnection creates a new connection for chunk download
	CreateConnection(urlStr string, chunk *chunk.Chunk, downloadConfig *common.Config) (connection.Connection, error)
	// UpdateConnection updates the connection with new parameters
	UpdateConnection(conn connection.Connection, chunk *chunk.Chunk)
	// GetChunkManager returns the chunk manager for this protocol
	GetChunkManager(downloadID uuid.UUID, tempDir string) (chunk.Manager, error)
}

type Handler struct {
	mu        sync.RWMutex
	protocols []Protocol
}

func NewHandler() *Handler {
	return &Handler{
		protocols: []Protocol{
			httpPkg.NewHandler(),
		},
	}
}

// RegisterProtocol adds a protocol to the handler.
func (h *Handler) RegisterProtocol(p Protocol) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.protocols = append(h.protocols, p)
}

// Initialize initializes a download by finding the appropriate handler and gathering information.
func (h *Handler) Initialize(ctx context.Context, url string, config *common.Config) (*common.DownloadInfo, error) {
	handler, err := h.getProtocolHandler(url)
	if err != nil {
		return nil, err
	}

	return handler.Initialize(ctx, url, config)
}

func (h *Handler) getProtocolHandler(url string) (Protocol, error) {
	if url == "" {
		return nil, ErrInvalidURL
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, handler := range h.protocols {
		if handler.CanHandle(url) {
			return handler, nil
		}
	}

	return nil, ErrUnsupportedProtocol
}

// GetHandler returns a handler that can handle the given URL.
func (h *Handler) GetHandler(url string) (Protocol, error) {
	return h.getProtocolHandler(url)
}
