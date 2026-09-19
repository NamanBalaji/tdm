package http

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"uuid"

	"github.com/NamanBalaji/tdm/internal/download"
)

// merge assembles the chunk files into the final file. It writes to a
// temporary .part file first and atomically renames it into place under a
// collision-safe name (never clobbering an existing file), returning the
// final filename. It aborts between chunks when ctx is cancelled so pause
// and remove stay responsive during long merges.
func (d *Downloader) merge(ctx context.Context, st *httpState, dir, filename string, id uuid.UUID) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create output dir: %w", err)
	}

	// A merge interrupted by a crash leaves its random-named part file
	// behind; clean this download's strays before creating a new one.
	removeStaleParts(dir, filename, id)

	outFile, err := os.CreateTemp(dir, partPrefix(filename, id)+"*.part")
	if err != nil {
		return "", fmt.Errorf("failed to create part file: %w", err)
	}

	partPath := outFile.Name()

	defer func() {
		if outFile != nil {
			_ = outFile.Close()
		}
		// No-op after a successful rename.
		_ = os.Remove(partPath)
	}()

	bufWriter := bufio.NewWriterSize(outFile, 4*1024*1024) // 4MB buffer

	// Sort chunks by start byte
	sorted := slices.Clone(st.Chunks)
	slices.SortFunc(sorted, func(a, b chunkState) int {
		return cmp.Compare(a.StartByte, b.StartByte)
	})

	for _, c := range sorted {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		chunkFile, err := os.Open(c.TempFilePath)
		if err != nil {
			return "", fmt.Errorf("failed to open chunk file %s: %w", c.TempFilePath, err)
		}

		// Copy exactly the chunk's byte range; a shorter file means the
		// chunk is truncated and merging it would corrupt the output.
		_, err = io.CopyN(bufWriter, chunkFile, c.EndByte-c.StartByte+1)
		_ = chunkFile.Close()

		if err != nil {
			return "", fmt.Errorf("failed to copy chunk data: %w", err)
		}
	}

	if err := bufWriter.Flush(); err != nil {
		return "", fmt.Errorf("failed to flush output: %w", err)
	}

	if err := outFile.Sync(); err != nil {
		return "", fmt.Errorf("failed to sync output: %w", err)
	}

	if err := outFile.Close(); err != nil {
		outFile = nil
		return "", fmt.Errorf("failed to close output: %w", err)
	}

	outFile = nil

	targetPath, err := reserveTargetPath(dir, filename)
	if err != nil {
		return "", err
	}

	if err := os.Rename(partPath, targetPath); err != nil {
		_ = os.Remove(targetPath) // release the reserved (empty) name

		return "", fmt.Errorf("failed to move output file into place: %w", err)
	}

	slog.Debug("merged chunks", "count", len(sorted), "path", targetPath)

	return filepath.Base(targetPath), nil
}

// maxCollisionCandidates bounds how many collision-renamed names
// (name.1.ext … name.N.ext) recovery considers when looking for the output
// of a merge whose new filename never reached the store.
const maxCollisionCandidates = 16

// findMergedOutput looks for evidence that a previous run's merge already
// produced the final file — under the recorded filename or one of merge's
// collision-rename variants. A candidate must have exactly the expected
// size, and any chunk files still on disk must match its sampled contents.
func findMergedOutput(dl *download.Download, st *httpState) (string, bool) {
	matches := findMergedOutputs(dl, st)
	if len(matches) == 0 {
		return "", false
	}

	return matches[0], true
}

// findMergedOutputs returns every candidate that matches, ordered highest
// collision index first: a collision-renamed output exists only because the
// base name was occupied by a FOREIGN file at merge time, so when several
// candidates match, the highest-numbered one is most likely ours. Checking
// the base name first would adopt (and later Remove would delete) that
// foreign file whenever its content happens to match — e.g. the completed
// output of an earlier download of the same URL.
func findMergedOutputs(dl *download.Download, st *httpState) []string {
	ext := filepath.Ext(dl.Filename)
	base := strings.TrimSuffix(dl.Filename, ext)

	candidates := make([]string, 0, maxCollisionCandidates+1)
	for i := maxCollisionCandidates; i >= 1; i-- {
		candidates = append(candidates, fmt.Sprintf("%s.%d%s", base, i, ext))
	}

	candidates = append(candidates, dl.Filename)

	var matches []string

	for _, name := range candidates {
		path := filepath.Join(dl.Dir, name)

		fi, err := os.Stat(path)
		if err != nil || fi.IsDir() || fi.Size() != dl.TotalSize {
			continue
		}

		if !chunksMatchFile(path, st.Chunks) {
			continue
		}

		matches = append(matches, name)
	}

	return matches
}

// chunksMatchFile reports whether the candidate file's contents match the
// still-available chunk files (the head of each chunk is sampled). Chunks
// whose temp file is gone can't disprove a match; if none are readable the
// caller's size check alone decides.
func chunksMatchFile(path string, chunks []chunkState) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}

	defer func() { _ = f.Close() }()

	const sampleSize = 4096

	for _, c := range chunks {
		cf, err := os.Open(c.TempFilePath)
		if err != nil {
			continue
		}

		n := min(int64(sampleSize), c.EndByte-c.StartByte+1)
		want := make([]byte, n)

		readN, errChunk := io.ReadFull(cf, want)
		_ = cf.Close()

		// A chunk file that is short or unreadable proves nothing about the
		// candidate (chunk temps aren't fsynced — after power loss a valid,
		// fsynced merged output routinely coexists with truncated temps).
		// Only bytes actually read can disprove a match.
		if errChunk != nil && !errors.Is(errChunk, io.ErrUnexpectedEOF) {
			continue
		}

		if readN == 0 {
			continue
		}

		got := make([]byte, readN)
		if _, err := f.ReadAt(got, c.StartByte); err != nil {
			return false
		}

		if !bytes.Equal(want[:readN], got) {
			return false
		}
	}

	return true
}

// partPrefix names this download's merge part files. The download ID keeps
// the pattern unique per download, so cleanup can never unlink a same-named
// sibling download's live part file.
func partPrefix(filename string, id uuid.UUID) string {
	return filename + ".tdm" + strings.ReplaceAll(id.String(), "-", "")[:8] + "."
}

// removeStaleParts deletes this download's leftover merge part files; an
// interrupted merge orphans its random-named part temp in the download
// directory and nothing else ever references it. Plain prefix matching —
// not filepath.Glob — because sanitized filenames may still contain glob
// metacharacters like '[' that would match the wrong files (or nothing).
func removeStaleParts(dir, filename string, id uuid.UUID) {
	prefix := partPrefix(filename, id)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".part") {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

// reserveTargetPath finds a name that doesn't clobber an existing file
// (file.ext, file.1.ext, file.2.ext, …) and reserves it by creating it
// exclusively, so a concurrent merge can't race for the same name.
func reserveTargetPath(dir, filename string) (string, error) {
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)

	for i := 0; ; i++ {
		name := filename
		if i > 0 {
			name = fmt.Sprintf("%s.%d%s", base, i, ext)
		}

		path := filepath.Join(dir, name)

		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			_ = f.Close()

			return path, nil
		}

		if !errors.Is(err, fs.ErrExist) {
			return "", fmt.Errorf("failed to reserve output path: %w", err)
		}
	}
}
