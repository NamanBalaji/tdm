package http

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
	"uuid"

	"golang.org/x/sync/errgroup"

	"github.com/NamanBalaji/tdm/internal/config"
	"github.com/NamanBalaji/tdm/internal/download"
	httpPkg "github.com/NamanBalaji/tdm/pkg/http"
)

// torrentDownloaderType is the torrent downloader's Type(). Type strings are
// persisted with every download, so this value never changes.
const torrentDownloaderType = "torrent"

type Downloader struct {
	cfg    *config.HTTPConfig
	client *httpPkg.Client
}

func New(cfg *config.HTTPConfig) *Downloader {
	return &Downloader{
		cfg:    cfg,
		client: httpPkg.NewClient(cfg.Connections),
	}
}

func (d *Downloader) Type() string { return "http" }

func (d *Downloader) CanHandle(url string) bool {
	return httpPkg.IsDownloadable(url)
}

func (d *Downloader) Init(ctx context.Context, url string, priority int) (*download.Download, error) {
	id := uuid.New()
	tempDir := filepath.Join(d.cfg.TempDir, id.String())

	meta, err := d.probe(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("failed to probe URL: %w", err)
	}

	// Links like download.php?id=123 serve a .torrent without saying so in
	// the URL, so CanHandle sent them here. The user wants the torrent's
	// contents, not the .torrent file itself.
	if isTorrentContentType(meta.contentType) {
		return nil, &download.HandoffError{
			Type:   torrentDownloaderType,
			Reason: "server returned content type " + meta.contentType,
		}
	}

	if meta.totalSize <= 0 {
		return nil, fmt.Errorf("%w: %s", httpPkg.ErrUnknownSize, url)
	}

	chunks := makeChunks(meta, tempDir, d.cfg.Chunks)

	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	st := httpState{
		Chunks:         chunks,
		SupportsRanges: meta.supportsRanges,
		TempDir:        tempDir,
	}

	stateBytes, err := json.Marshal(&st)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal state: %w", err)
	}

	return &download.Download{
		ID:        id,
		URL:       url,
		Filename:  meta.filename,
		Dir:       d.cfg.DownloadDir,
		Status:    download.Pending,
		Priority:  priority,
		Type:      "http",
		TotalSize: meta.totalSize,
		State:     stateBytes,
	}, nil
}

func (d *Downloader) Start(ctx context.Context, dl *download.Download, onProgress func(int64, int64, jsontext.Value)) error {
	var st httpState
	if err := json.Unmarshal(dl.State, &st); err != nil {
		return fmt.Errorf("failed to unmarshal state: %w", err)
	}

	if len(st.Chunks) == 0 {
		return fmt.Errorf("%w: no chunks in download state", httpPkg.ErrUnknownSize)
	}

	// If every chunk was recorded complete, a previous run may already have
	// merged the output (crash before the Completed status was persisted).
	// Adopt that file — possibly under a collision-renamed name — instead of
	// re-merging into a duplicate or failing on deleted chunk files.
	if allChunksComplete(&st) {
		if name, ok := findMergedOutput(dl, &st); ok {
			dl.Filename = name
			_ = os.RemoveAll(st.TempDir)

			return nil
		}
	}

	// Persisted chunk progress can be ahead of what actually reached disk:
	// chunk files aren't fsynced (power loss), and the temp dir may have
	// been wiped (/tmp on reboot). Trusting stale counters would seek past
	// EOF and silently zero-fill the file, so clamp to the on-disk truth.
	reconcileChunks(&st)

	// Find incomplete chunks
	var pending []int

	for i, c := range st.Chunks {
		if !c.Completed {
			pending = append(pending, i)
		}
	}

	if len(pending) > 0 {
		// The temp dir may be gone on resume (wiped /tmp after a reboot).
		if err := os.MkdirAll(st.TempDir, 0o755); err != nil {
			return fmt.Errorf("failed to create temp dir: %w", err)
		}

		if err := d.downloadPending(ctx, dl, &st, pending, onProgress); err != nil {
			return err
		}
	}

	// All chunks complete — merge into the final file. This must also run
	// when pending was empty: a crash after the chunks finished but before
	// the merge would otherwise mark the download Completed with no file.
	return d.finalize(ctx, dl, &st)
}

func (d *Downloader) Remove(dl *download.Download) error {
	var st httpState

	stErr := json.Unmarshal(dl.State, &st)

	// Decide about the final file BEFORE deleting the temp chunks — the
	// content verification below reads them, and destroying the evidence
	// first would degrade every check to a bare name+size match.
	//
	// Deleting the final path for a download that never produced it would
	// destroy a pre-existing user file of the same name — the very file
	// merge deliberately never overwrites.
	switch {
	case dl.Status == download.Completed:
		_ = os.Remove(filepath.Join(dl.Dir, dl.Filename))
	case stErr == nil && len(st.Chunks) > 0 && allChunksComplete(&st):
		// Crash window: the merge may have finished without the Completed
		// status ever being persisted. Delete only an UNAMBIGUOUS verified
		// match AT THE RECORDED NAME: a collision-renamed candidate
		// (file.1.ext) that this record never learned about may equally be
		// ANOTHER record's byte-identical output of the same URL, and
		// leaking our file is safer than deleting theirs.
		if matches := findMergedOutputs(dl, &st); len(matches) == 1 && matches[0] == dl.Filename {
			_ = os.Remove(filepath.Join(dl.Dir, matches[0]))
		}
	}

	if stErr == nil {
		_ = os.RemoveAll(st.TempDir)
	}

	removeStaleParts(dl.Dir, dl.Filename, dl.ID)

	return nil
}

// downloadPending transfers the given chunks concurrently and saves the
// resulting state back into dl (whether success, cancel, or error).
func (d *Downloader) downloadPending(ctx context.Context, dl *download.Download, st *httpState, pending []int, onProgress func(int64, int64, jsontext.Value)) error {
	// stateMu guards all chunk mutations so the checkpoint marshaller sees a
	// consistent snapshot.
	var stateMu sync.Mutex

	// Shared counter for progress reporting
	var totalDownloaded atomic.Int64
	for _, c := range st.Chunks {
		totalDownloaded.Add(c.Downloaded)
	}

	checkpoint := func() jsontext.Value {
		stateMu.Lock()
		defer stateMu.Unlock()

		data, err := json.Marshal(st)
		if err != nil {
			return nil
		}

		return data
	}

	// Progress reporting goroutine
	reportCtx, reportCancel := context.WithCancel(ctx)
	reportDone := make(chan struct{})

	go func() {
		defer close(reportDone)

		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-reportCtx.Done():
				return
			case <-ticker.C:
				onProgress(totalDownloaded.Load(), dl.TotalSize, checkpoint())
			}
		}
	}()

	// Download chunks concurrently with errgroup + semaphore
	g, gCtx := errgroup.WithContext(ctx)
	sem := make(chan struct{}, d.cfg.Connections)

	for _, idx := range pending {
		g.Go(func() error {
			select {
			case <-gCtx.Done():
				return gCtx.Err()
			case sem <- struct{}{}:
				defer func() { <-sem }()
			}

			return d.downloadChunk(gCtx, &st.Chunks[idx], dl.URL, st.SupportsRanges, dl.TotalSize, &totalDownloaded, &stateMu)
		})
	}

	err := g.Wait()

	// Stop progress reporter and wait for it to exit
	reportCancel()
	<-reportDone

	// Save state for resume (whether success, cancel, or error)
	d.saveState(dl, st)

	return err
}

// finalize merges completed chunks into the final file, updates dl.Filename
// with the (possibly collision-renamed) result, and removes the temp dir.
func (d *Downloader) finalize(ctx context.Context, dl *download.Download, st *httpState) error {
	finalName, err := d.merge(ctx, st, dl.Dir, dl.Filename, dl.ID)
	if err != nil {
		return err
	}

	dl.Filename = finalName

	// Clean up temp directory
	_ = os.RemoveAll(st.TempDir)

	return nil
}

// allChunksComplete reports whether the persisted state records every chunk
// as fully downloaded.
func allChunksComplete(st *httpState) bool {
	for _, c := range st.Chunks {
		if !c.Completed {
			return false
		}
	}

	return true
}

// reconcileChunks clamps persisted chunk progress to what actually reached
// disk. The checkpoint DB is fsynced but chunk files are not, so after a
// power loss (or a wiped temp dir) the persisted counters can claim more
// bytes than the files hold — seeking to such an offset would zero-fill.
func reconcileChunks(st *httpState) {
	for i := range st.Chunks {
		c := &st.Chunks[i]

		var size int64
		if fi, err := os.Stat(c.TempFilePath); err == nil {
			size = fi.Size()
		}

		if c.Downloaded > size {
			c.Downloaded = size
			c.Completed = false
		}

		if c.Completed && c.Downloaded < c.EndByte-c.StartByte+1 {
			c.Completed = false
		}
	}
}

// downloadChunk downloads a single chunk with retry logic. Retryable
// failures that still advanced the chunk reset the retry budget: servers
// that cap bytes-per-connection would otherwise fail chunks larger than
// MaxRetries × cap despite steady, monotonic progress.
func (d *Downloader) downloadChunk(ctx context.Context, chunk *chunkState, url string, supportsRanges bool, totalSize int64, downloaded *atomic.Int64, stateMu *sync.Mutex) error {
	var lastErr error

	// Guarantee at least one transfer attempt even if MaxRetries is 0.
	maxFailures := max(d.cfg.MaxRetries, 1)
	failures := 0

	for failures < maxFailures {
		before := chunk.Downloaded

		err := d.transferChunk(ctx, chunk, url, supportsRanges, totalSize, downloaded, stateMu)
		if err == nil {
			stateMu.Lock()
			chunk.Completed = true
			stateMu.Unlock()

			return nil
		}

		lastErr = err
		if errors.Is(err, context.Canceled) || !isRetryableError(err) {
			return err
		}

		// Only ranged transfers make durable progress across attempts — a
		// non-range attempt restarts from byte 0 every time, so resetting
		// its budget on "progress" would retry forever.
		if supportsRanges && chunk.Downloaded > before {
			failures = 0
		} else {
			failures++
			if failures >= maxFailures {
				break
			}
		}

		backoff := calculateBackoff(max(failures-1, 0), d.cfg.RetryDelay)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
			slog.Debug("retrying chunk", "chunk", chunk.ID, "failures", failures)
		}
	}

	return fmt.Errorf("chunk %s failed after %d retries: %w", chunk.ID, maxFailures, lastErr)
}

// transferChunk performs the actual byte transfer for a single chunk.
func (d *Downloader) transferChunk(ctx context.Context, chunk *chunkState, url string, supportsRanges bool, totalSize int64, downloaded *atomic.Int64, stateMu *sync.Mutex) error {
	// A server without range support always sends the file from byte 0, so
	// partial progress from a previous attempt must be discarded.
	if !supportsRanges && chunk.Downloaded > 0 {
		downloaded.Add(-chunk.Downloaded)
		stateMu.Lock()
		chunk.Downloaded = 0
		stateMu.Unlock()
	}

	currentStart := chunk.StartByte + chunk.Downloaded

	headers := map[string]string{"User-Agent": httpPkg.DefaultUserAgent}
	if supportsRanges {
		headers["Range"] = fmt.Sprintf("bytes=%d-%d", currentStart, chunk.EndByte)
	}

	conn := newConnection(url, headers, d.client, currentStart, chunk.EndByte, totalSize)

	defer func() { _ = conn.close() }()

	file, err := os.OpenFile(chunk.TempFilePath, os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open chunk file: %w", err)
	}

	defer func() { _ = file.Close() }()

	if _, err := file.Seek(chunk.Downloaded, 0); err != nil {
		return fmt.Errorf("failed to seek: %w", err)
	}

	buf := make([]byte, 32*1024)
	chunkSize := chunk.EndByte - chunk.StartByte + 1
	remaining := chunkSize - chunk.Downloaded

	for remaining > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, readErr := conn.Read(ctx, buf)
		if n > 0 {
			writeN := min(int64(n), remaining)

			if _, err := file.Write(buf[:writeN]); err != nil {
				return fmt.Errorf("failed to write: %w", err)
			}

			stateMu.Lock()
			chunk.Downloaded += writeN
			stateMu.Unlock()
			downloaded.Add(writeN)
			remaining -= writeN
		}

		if readErr != nil {
			if err := classifyReadError(readErr, remaining, chunk.ID); err != nil {
				return err
			}

			break // clean EOF with the chunk fully transferred
		}
	}

	return nil
}

// classifyReadError converts a body-read error into what transferChunk
// should return; nil means a clean EOF with no bytes remaining.
func classifyReadError(readErr error, remaining int64, chunkID uuid.UUID) error {
	if errors.Is(readErr, context.Canceled) || errors.Is(readErr, context.DeadlineExceeded) {
		return readErr
	}

	if readErr.Error() == "EOF" || errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF) {
		// A premature close must NOT be treated as success — the chunk
		// would be silently truncated and merged into a corrupt file.
		if remaining > 0 {
			return fmt.Errorf("%w: server closed connection with %d bytes left in chunk %s",
				httpPkg.ErrUnexpectedEOF, remaining, chunkID)
		}

		return nil
	}

	// Classify so transient transport failures (e.g. a mid-body TCP reset,
	// which surfaces as a raw *net.OpError) are retryable just like a clean
	// early close. Already-classified errors pass through unchanged.
	return fmt.Errorf("failed to read chunk body: %w", httpPkg.ClassifyError(readErr))
}

// saveState serializes the current chunk progress back to dl.State.
func (d *Downloader) saveState(dl *download.Download, st *httpState) {
	if data, err := json.Marshal(st); err == nil {
		dl.State = data
	}
}

func makeChunks(meta *probeResult, tempDir string, numChunks int) []chunkState {
	if meta.totalSize <= 0 {
		return nil
	}

	if !meta.supportsRanges {
		id := uuid.New()

		return []chunkState{{
			ID:           id,
			StartByte:    0,
			EndByte:      meta.totalSize - 1,
			TempFilePath: filepath.Join(tempDir, id.String()),
		}}
	}

	chunkSize := meta.totalSize / int64(numChunks)
	if chunkSize <= 0 {
		id := uuid.New()

		return []chunkState{{
			ID:           id,
			StartByte:    0,
			EndByte:      meta.totalSize - 1,
			TempFilePath: filepath.Join(tempDir, id.String()),
		}}
	}

	var (
		chunks []chunkState
		start  int64
	)

	for start < meta.totalSize {
		end := min(start+chunkSize-1, meta.totalSize-1)

		id := uuid.New()
		chunks = append(chunks, chunkState{
			ID:           id,
			StartByte:    start,
			EndByte:      end,
			TempFilePath: filepath.Join(tempDir, id.String()),
		})
		start = end + 1
	}

	return chunks
}
