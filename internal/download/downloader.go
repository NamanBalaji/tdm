package download

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
)

// HandoffError is returned by Init when probing shows that a different
// Downloader must handle the URL — e.g. an HTTP link that serves a .torrent
// file, whose contents the torrent downloader must fetch rather than the
// HTTP downloader saving the .torrent itself. CanHandle can't know this
// because it must not do network I/O, so the Manager retries Init with the
// Downloader whose Type() matches.
type HandoffError struct {
	Type   string // Type() of the Downloader that should handle the URL
	Reason string
}

func (e *HandoffError) Error() string {
	return fmt.Sprintf("URL should be handled by the %s downloader: %s", e.Type, e.Reason)
}

// Downloader defines the contract for a protocol-specific download strategy.
//
// Implementations handle ONLY the byte-transfer logic for their protocol.
// They do NOT manage goroutines for persistence or progress tracking — the
// Manager handles those cross-cutting concerns.
//
// The lifecycle is:
//  1. CanHandle(url) → true means this Downloader claims the URL
//  2. Init(ctx, url, priority) → creates a Download with BackendState populated
//  3. Start(ctx, dl, onProgress) → blocks until done/cancelled, calls onProgress periodically
//  4. Remove(dl) → cleans up files when user deletes a download
//
// Each Downloader receives its configuration (including download directory)
// at construction time. This avoids the Manager needing per-type config knowledge.
//
// Start is a blocking call. The Manager runs it in a goroutine and uses
// context cancellation to pause or stop it. When context is cancelled,
// Start should save resumable state into dl.State and return ctx.Err().
// Start receives a private copy of the Download; the Manager merges State,
// TotalSize, and Filename back when it returns.
type Downloader interface {
	// Type returns the protocol identifier (e.g., "http", "torrent").
	// Used to find the right Downloader when reloading persisted downloads.
	Type() string

	// CanHandle reports whether this Downloader can handle the given URL.
	// Called in registration order; first match wins. It must judge from
	// the URL alone — no network I/O.
	CanHandle(url string) bool

	// Init creates a new Download by probing the URL for metadata.
	// It populates Filename, TotalSize, Type, Dir, and BackendState.
	// The download directory comes from the Downloader's own config.
	// This may make network requests (e.g., HTTP HEAD, torrent metadata fetch).
	// If the probe shows another Downloader must handle the URL, Init
	// returns a *HandoffError naming it.
	Init(ctx context.Context, url string, priority int) (*Download, error)

	// Start downloads the file. It blocks until the download completes,
	// fails, or ctx is cancelled (pause/cancel).
	//
	// It calls onProgress(downloaded, totalSize, state) periodically to
	// report byte counts. The Manager uses these to compute speed and ETA.
	// state, when non-nil, is a crash-safe checkpoint of the protocol's
	// resume state that the Manager persists on its next save tick — a
	// SIGKILL loses at most the last checkpoint interval. After a power
	// loss the persisted counters may be ahead of what reached disk, so
	// Start must re-validate them against the actual files on resume.
	//
	// On context cancellation, Start MUST update dl.State with current
	// progress so the download can be resumed later, then return ctx.Err().
	//
	// On success, Start returns nil. On permanent failure, it returns the error.
	Start(ctx context.Context, dl *Download, onProgress func(downloaded, totalSize int64, state jsontext.Value)) error

	// Remove deletes all files (temp and final) associated with the download.
	// Called when the user removes a download from the list.
	Remove(dl *Download) error
}
