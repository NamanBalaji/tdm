package protocol_test

import (
	"github.com/NamanBalaji/tdm/internal/common"
	"github.com/NamanBalaji/tdm/internal/errors"
	"testing"
	"time"

	"github.com/NamanBalaji/tdm/internal/chunk"
	"github.com/NamanBalaji/tdm/internal/connection"
	"github.com/NamanBalaji/tdm/internal/downloader"
	proto "github.com/NamanBalaji/tdm/internal/protocol"
	httpProtocol "github.com/NamanBalaji/tdm/internal/protocol/http"
)

type dummyConn struct{}

func (d *dummyConn) Connect() error                   { return nil }
func (d *dummyConn) Read(p []byte) (int, error)       { return 0, nil }
func (d *dummyConn) Close() error                     { return nil }
func (d *dummyConn) IsAlive() bool                    { return true }
func (d *dummyConn) Reset() error                     { return nil }
func (d *dummyConn) GetURL() string                   { return "http://dummy" }
func (d *dummyConn) GetHeaders() map[string]string    { return map[string]string{} }
func (d *dummyConn) SetTimeout(timeout time.Duration) {}

type fakeProtocol struct {
	canHandle        bool
	initializeCalled bool
	info             *common.DownloadInfo
	initErr          error
	createConnFunc   func(urlStr string, ck *chunk.Chunk, options *downloader.Config) (connection.Connection, error)
}

func (f *fakeProtocol) CanHandle(url string) bool {
	return f.canHandle
}

func (f *fakeProtocol) Initialize(url string, options *downloader.Config) (*common.DownloadInfo, error) {
	f.initializeCalled = true
	return f.info, f.initErr
}

func (f *fakeProtocol) CreateConnection(urlStr string, ck *chunk.Chunk, options *downloader.Config) (connection.Connection, error) {
	if f.createConnFunc != nil {
		return f.createConnFunc(urlStr, ck, options)
	}
	return nil, nil
}

func TestNewHandler(t *testing.T) {
	h := proto.NewHandler()
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
	_, err := h.Initialize("http://127.0.0.1:0", &downloader.Config{})
	if err == nil {
		t.Error("expected error from HTTP handler in test environment, got nil")
	}
}

func TestRegisterProtocol(t *testing.T) {
	h := proto.NewHandler()
	fp := &fakeProtocol{canHandle: true}
	h.RegisterProtocol(fp)
	_, err := h.Initialize("custom://example.com", &downloader.Config{})
	if err != nil {
		t.Errorf("expected fake protocol to handle the URL, got error: %v", err)
	}
}

func TestInitialize_EmptyURL(t *testing.T) {
	h := proto.NewHandler()
	_, err := h.Initialize("", &downloader.Config{})
	if err == nil || err.Error() != errors.ErrInvalidURL.Error() {
		t.Errorf("expected error %q for empty URL, got %v", errors.ErrInvalidURL, err)
	}
}

func TestInitialize_Unsupported(t *testing.T) {
	h := proto.NewHandler()
	_, err := h.Initialize("ftp://example.com/file", &downloader.Config{})
	if err == nil || err.Error() != errors.ErrUnsupportedProtocol.Error() {
		t.Errorf("expected error %q for unsupported protocol, got %v", errors.ErrUnsupportedProtocol, err)
	}
}

func TestInitialize_UsesFakeProtocol(t *testing.T) {
	h := proto.NewHandler()
	fp := &fakeProtocol{
		canHandle: true,
		info: &common.DownloadInfo{
			URL:       "custom://example.com/file",
			MimeType:  "application/fake",
			TotalSize: 999,
		},
	}
	h.RegisterProtocol(fp)
	info, err := h.Initialize("custom://example.com/file", &downloader.Config{})
	if err != nil {
		t.Fatalf("Initialize returned unexpected error: %v", err)
	}
	if info.URL != "custom://example.com/file" {
		t.Errorf("expected URL %q, got %q", "custom://example.com/file", info.URL)
	}
	if info.MimeType != "application/fake" {
		t.Errorf("expected MimeType %q, got %q", "application/fake", info.MimeType)
	}
	if info.TotalSize != 999 {
		t.Errorf("expected TotalSize 999, got %d", info.TotalSize)
	}
	if !fp.initializeCalled {
		t.Error("expected fake protocol's Initialize method to be called")
	}
}

func TestInitialize_UsesHTTPHandler(t *testing.T) {
	h := proto.NewHandler()
	_, err := h.Initialize("http://127.0.0.1:0", &downloader.Config{})
	if err == nil {
		t.Error("expected error from HTTP handler in test environment, got nil")
	}
}

func TestCreateConnection_Delegation(t *testing.T) {
	h := proto.NewHandler()
	dummy := &dummyConn{}
	fp := &fakeProtocol{
		canHandle: true,
		createConnFunc: func(urlStr string, ck *chunk.Chunk, options *downloader.Config) (connection.Connection, error) {
			return dummy, nil
		},
	}
	h.RegisterProtocol(fp)
	conn, err := fp.CreateConnection("custom://example.com/file", &chunk.Chunk{}, &downloader.Config{})
	if err != nil {
		t.Fatalf("CreateConnection returned unexpected error: %v", err)
	}
	if conn == nil {
		t.Fatal("expected non-nil connection")
	}
	if conn != dummy {
		t.Errorf("expected dummy connection, got %v", conn)
	}
}

func TestHTTPHandler_CanHandle(t *testing.T) {
	h := httpProtocol.NewHandler()
	if !h.CanHandle("http://example.com") {
		t.Error("expected HTTP handler to handle http URLs")
	}
	if !h.CanHandle("https://example.com") {
		t.Error("expected HTTP handler to handle https URLs")
	}
	if h.CanHandle("ftp://example.com") {
		t.Error("expected HTTP handler not to handle ftp URLs")
	}
}
