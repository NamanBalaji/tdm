package engine_test

import (
	"errors"
	"github.com/NamanBalaji/tdm/internal/common"
	"github.com/NamanBalaji/tdm/internal/engine"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func newEngineNoInit(t *testing.T) *engine.Engine {
	t.Helper()
	dir := t.TempDir()
	cfg := &engine.Config{
		DownloadDir:               filepath.Join(dir, "dl"),
		TempDir:                   filepath.Join(dir, "tmp"),
		ConfigDir:                 filepath.Join(dir, "cfg"),
		MaxConcurrentDownloads:    1,
		MaxConnectionsPerDownload: 1,
		MaxConnectionsPerHost:     1,
		MaxRetries:                1,
		RetryDelay:                1,
		SaveInterval:              1,
		AutoStartDownloads:        false,
	}
	e, err := engine.New(cfg)
	if err != nil {
		t.Fatalf("New engine failed: %v", err)
	}
	t.Cleanup(func() {
		if err := e.Shutdown(); err != nil {
			t.Errorf("Shutdown error: %v", err)
		}
	})
	return e
}

func newEngine(t *testing.T) *engine.Engine {
	t.Helper()
	e := newEngineNoInit(t)
	if err := e.Init(); err != nil {
		t.Fatalf("Init engine failed: %v", err)
	}
	return e
}

func TestAddDownload_BeforeInit(t *testing.T) {
	e := newEngineNoInit(t)
	_, err := e.AddDownload("http://example.com/file", &common.Config{Connections: 1})
	if !errors.Is(err, engine.ErrEngineNotRunning) {
		t.Errorf("expected ErrEngineNotRunning, got %v", err)
	}
}

func TestAddDownload_EmptyURL(t *testing.T) {
	e := newEngine(t)
	_, err := e.AddDownload("", &common.Config{Connections: 1})
	if !errors.Is(err, engine.ErrInvalidURL) {
		t.Errorf("expected ErrInvalidURL, got %v", err)
	}
}

func TestAddDownload_SuccessAndDuplicate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", "0")
			w.Header().Set("Accept-Ranges", "bytes")
		}
	}))
	defer srv.Close()

	e := newEngine(t)
	url := srv.URL + "/file"
	id1, err := e.AddDownload(url, &common.Config{Connections: 1})
	if err != nil {
		t.Fatalf("first AddDownload failed: %v", err)
	}
	_, err2 := e.AddDownload(url, &common.Config{Connections: 1})
	if !errors.Is(err2, engine.ErrDownloadExists) {
		t.Errorf("expected ErrDownloadExists, got %v", err2)
	}
	list := e.ListDownloads()
	found := false
	for _, dl := range list {
		if dl.ID == id1 {
			found = true
		}
	}
	if !found {
		t.Errorf("ListDownloads missing id %v", id1)
	}
}

func TestStartDownloadAndLifecycle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", "0")
			w.Header().Set("Accept-Ranges", "bytes")
		}
	}))
	defer srv.Close()

	e := newEngine(t)
	url := srv.URL + "/f"
	id, err := e.AddDownload(url, &common.Config{Connections: 1})
	if err != nil {
		t.Fatalf("AddDownload failed: %v", err)
	}
	if err := e.StartDownload(id); err != nil {
		t.Errorf("StartDownload error: %v", err)
	}

	dl, err := e.GetDownload(id)
	if err != nil {
		t.Fatalf("GetDownload after start: %v", err)
	}
	if status := dl.GetStatus(); status != common.StatusCompleted {
		t.Errorf("expected Completed, got %v", status)
	}
	if err := e.PauseDownload(id); err != nil {
		t.Errorf("PauseDownload error: %v", err)
	}
	if err := e.CancelDownload(id, false); err != nil {
		t.Errorf("CancelDownload error: %v", err)
	}

	dl.SetStatus(common.StatusPaused)
	if err := e.ResumeDownload(id); err != nil {
		t.Errorf("ResumeDownload error: %v", err)
	}

	dl2, _ := e.GetDownload(id)
	if status := dl2.GetStatus(); status != common.StatusQueued {
		t.Errorf("expected Queued after resume, got %v", status)
	}
}

func TestRemoveDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	e := newEngine(t)
	url := srv.URL + "/x"
	id, _ := e.AddDownload(url, &common.Config{Connections: 1})
	if err := e.RemoveDownload(id, true); err != nil {
		t.Errorf("RemoveDownload failed: %v", err)
	}
	if _, err := e.GetDownload(id); !errors.Is(err, engine.ErrDownloadNotFound) {
		t.Errorf("expected ErrDownloadNotFound, got %v", err)
	}
}

func TestShutdown_Idempotent(t *testing.T) {
	e := newEngine(t)
	if err := e.Shutdown(); err != nil {
		t.Errorf("Shutdown error: %v", err)
	}
	if err := e.Shutdown(); err != nil {
		t.Errorf("Second Shutdown error: %v", err)
	}
}
