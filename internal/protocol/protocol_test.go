package protocol_test

//
// import (
//	"context"
//	"testing"
//	"time"
//
//	"github.com/NamanBalaji/tdm/internal/chunk"
//	"github.com/NamanBalaji/tdm/internal/common"
//	"github.com/NamanBalaji/tdm/internal/connection"
//	"github.com/NamanBalaji/tdm/internal/downloader"
//	"github.com/NamanBalaji/tdm/internal/errors"
//	"github.com/NamanBalaji/tdm/internal/protocol"
//)
//
//// mockConnection implements the connection.Connection interface for testing
//type mockConnection struct{}
//
//func (m *mockConnection) Connect(ctx context.Context) error               { return nil }
//func (m *mockConnection) Read(ctx context.Context, p []byte) (int, error) { return 0, nil }
//func (m *mockConnection) Close() error                                    { return nil }
//func (m *mockConnection) IsAlive() bool                                   { return true }
//func (m *mockConnection) Reset(ctx context.Context) error                 { return nil }
//func (m *mockConnection) GetURL() string                                  { return "http://example.com" }
//func (m *mockConnection) GetHeaders() map[string]string                   { return nil }
//func (m *mockConnection) SetTimeout(timeout time.Duration)                {}
//
//// mockProtocol implements the Protocol interface for testing
//type mockProtocol struct {
//	canHandleFunc        func(url string) bool
//	initializeFunc       func(ctx context.Context, url string, config *downloader.Config) (*common.DownloadInfo, error)
//	createConnectionFunc func(ctx context.Context, url string, chunk *chunk.Chunk, config *downloader.Config) (connection.Connection, error)
//}
//
//func (m *mockProtocol) CanHandle(url string) bool {
//	if m.canHandleFunc != nil {
//		return m.canHandleFunc(url)
//	}
//	return true
//}
//
//func (m *mockProtocol) Initialize(ctx context.Context, url string, config *downloader.Config) (*common.DownloadInfo, error) {
//	if m.initializeFunc != nil {
//		return m.initializeFunc(ctx, url, config)
//	}
//	return &common.DownloadInfo{
//		URL:            url,
//		Filename:       "test.txt",
//		TotalSize:      1000,
//		SupportsRanges: true,
//	}, nil
//}
//
//func (m *mockProtocol) CreateConnection(ctx context.Context, url string, c *chunk.Chunk, config *downloader.Config) (connection.Connection, error) {
//	if m.createConnectionFunc != nil {
//		return m.createConnectionFunc(ctx, url, c, config)
//	}
//	return &mockConnection{}, nil
//}
//
//func TestNewHandler(t *testing.T) {
//	h := protocol.NewHandler()
//	if h == nil {
//		t.Fatal("expected non-nil handler")
//	}
//
//	// The default handler should have at least the HTTP protocol registered
//	handler, err := h.GetHandler("http://example.com")
//	if err != nil {
//		t.Errorf("expected a handler for HTTP, got error: %v", err)
//	}
//	if handler == nil {
//		t.Error("expected a non-nil HTTP handler")
//	}
//}
//
//func TestRegisterProtocol(t *testing.T) {
//	h := protocol.NewHandler()
//
//	// Create a mock protocol that handles a custom scheme
//	mockProto := &mockProtocol{
//		canHandleFunc: func(url string) bool {
//			return url == "custom://example.com" || url == "special://test.com"
//		},
//	}
//
//	h.RegisterProtocol(mockProto)
//
//	// Test with a URL the mock protocol can handle
//	handler, err := h.GetHandler("custom://example.com")
//	if err != nil {
//		t.Errorf("expected no error, got: %v", err)
//	}
//	if handler != mockProto {
//		t.Error("expected to get our registered mock protocol")
//	}
//
//	// Test with another URL the mock protocol can handle
//	handler, err = h.GetHandler("special://test.com")
//	if err != nil {
//		t.Errorf("expected no error, got: %v", err)
//	}
//	if handler != mockProto {
//		t.Error("expected to get our registered mock protocol")
//	}
//}
//
//func TestGetProtocolHandler(t *testing.T) {
//	h := protocol.NewHandler()
//
//	testCases := []struct {
//		name          string
//		url           string
//		expectError   bool
//		expectedError error
//	}{
//		{
//			name:          "Empty URL",
//			url:           "",
//			expectError:   true,
//			expectedError: errors.ErrInvalidURL,
//		},
//		{
//			name:        "HTTP URL",
//			url:         "http://example.com",
//			expectError: false,
//		},
//		{
//			name:        "HTTPS URL",
//			url:         "https://example.com",
//			expectError: false,
//		},
//		{
//			name:          "Unsupported Protocol",
//			url:           "ftp://example.com",
//			expectError:   true,
//			expectedError: errors.ErrUnsupportedProtocol,
//		},
//	}
//
//	for _, tc := range testCases {
//		t.Run(tc.name, func(t *testing.T) {
//			handler, err := h.GetHandler(tc.url)
//
//			if tc.expectError {
//				if err == nil {
//					t.Error("expected an error, got nil")
//					return
//				}
//
//				if tc.expectedError != nil && err.Error() != tc.expectedError.Error() {
//					t.Errorf("expected error %q, got %q", tc.expectedError, err)
//				}
//
//				if handler != nil {
//					t.Errorf("expected nil handler, got %T", handler)
//				}
//			} else {
//				if err != nil {
//					t.Errorf("unexpected error: %v", err)
//					return
//				}
//
//				if handler == nil {
//					t.Error("expected a handler, got nil")
//				}
//			}
//		})
//	}
//}
//
//func TestInitialize(t *testing.T) {
//	h := protocol.NewHandler()
//	ctx := context.Background()
//
//	// Test with invalid URL
//	_, err := h.Initialize(ctx, "", nil)
//	if err == nil || err.Error() != errors.ErrInvalidURL.Error() {
//		t.Errorf("expected error %q for empty URL, got %v", errors.ErrInvalidURL, err)
//	}
//
//	// Test with unsupported protocol
//	_, err = h.Initialize(ctx, "ftp://example.com", nil)
//	if err == nil || err.Error() != errors.ErrUnsupportedProtocol.Error() {
//		t.Errorf("expected error %q for unsupported protocol, got %v", errors.ErrUnsupportedProtocol, err)
//	}
//
//	// Test with custom protocol
//	customInfo := &common.DownloadInfo{
//		URL:            "custom://example.com",
//		Filename:       "custom.dat",
//		MimeType:       "application/octet-stream",
//		TotalSize:      5000,
//		SupportsRanges: true,
//	}
//
//	mockProto := &mockProtocol{
//		canHandleFunc: func(url string) bool {
//			return url == "custom://example.com"
//		},
//		initializeFunc: func(ctx context.Context, url string, config *downloader.Config) (*common.DownloadInfo, error) {
//			return customInfo, nil
//		},
//	}
//
//	h.RegisterProtocol(mockProto)
//
//	info, err := h.Initialize(ctx, "custom://example.com", nil)
//	if err != nil {
//		t.Errorf("Initialize returned unexpected error: %v", err)
//	}
//
//	if info != customInfo {
//		t.Error("expected Initialize to return our custom download info")
//	}
//}
//
//func TestMultipleProtocolHandlers(t *testing.T) {
//	h := protocol.NewHandler()
//
//	// Register multiple protocols with overlapping handling capabilities
//	mockProto1 := &mockProtocol{
//		canHandleFunc: func(url string) bool {
//			return url == "custom://example.com" || url == "shared://example.com"
//		},
//	}
//
//	mockProto2 := &mockProtocol{
//		canHandleFunc: func(url string) bool {
//			return url == "unique://example.com" || url == "shared://example.com"
//		},
//	}
//
//	h.RegisterProtocol(mockProto1)
//	h.RegisterProtocol(mockProto2)
//
//	// First registered protocol should handle shared URL
//	handler, err := h.GetHandler("shared://example.com")
//	if err != nil {
//		t.Errorf("expected no error, got: %v", err)
//	}
//	if handler != mockProto1 {
//		t.Error("expected first registered protocol to handle shared URL")
//	}
//
//	// First protocol should handle its unique URL
//	handler, err = h.GetHandler("custom://example.com")
//	if err != nil {
//		t.Errorf("expected no error, got: %v", err)
//	}
//	if handler != mockProto1 {
//		t.Error("expected first protocol to handle its unique URL")
//	}
//
//	// Second protocol should handle its unique URL
//	handler, err = h.GetHandler("unique://example.com")
//	if err != nil {
//		t.Errorf("expected no error, got: %v", err)
//	}
//	if handler != mockProto2 {
//		t.Error("expected second protocol to handle its unique URL")
//	}
//}
//
//func TestConcurrentAccess(t *testing.T) {
//	h := protocol.NewHandler()
//
//	// Register a mock protocol
//	mockProto := &mockProtocol{
//		canHandleFunc: func(url string) bool {
//			return url == "custom://example.com"
//		},
//	}
//
//	h.RegisterProtocol(mockProto)
//
//	// Run multiple goroutines that access the handler concurrently
//	const goroutineCount = 10
//	errChan := make(chan error, goroutineCount)
//
//	for i := 0; i < goroutineCount; i++ {
//		go func(index int) {
//			var err error
//
//			// Alternate between HTTP and custom protocol
//			if index%2 == 0 {
//				_, err = h.GetHandler("http://example.com")
//			} else {
//				_, err = h.GetHandler("custom://example.com")
//			}
//
//			errChan <- err
//		}(i)
//	}
//
//	// Collect results
//	for i := 0; i < goroutineCount; i++ {
//		if err := <-errChan; err != nil {
//			t.Errorf("concurrent access error: %v", err)
//		}
//	}
//}
//
//func TestProtocolInitializeError(t *testing.T) {
//	h := protocol.NewHandler()
//	ctx := context.Background()
//
//	expectedErr := errors.New("initialization failed")
//
//	mockProto := &mockProtocol{
//		canHandleFunc: func(url string) bool {
//			return url == "error://example.com"
//		},
//		initializeFunc: func(ctx context.Context, url string, config *downloader.Config) (*common.DownloadInfo, error) {
//			return nil, expectedErr
//		},
//	}
//
//	h.RegisterProtocol(mockProto)
//
//	info, err := h.Initialize(ctx, "error://example.com", nil)
//	if err == nil {
//		t.Error("expected an error, got nil")
//	}
//	if !errors.Is(err, expectedErr) {
//		t.Errorf("expected error %q, got %q", expectedErr, err)
//	}
//	if info != nil {
//		t.Errorf("expected nil info, got %+v", info)
//	}
//}
//
//func TestProtocolCreateConnection(t *testing.T) {
//	h := protocol.NewHandler()
//	ctx := context.Background()
//
//	mockChunk := &chunk.Chunk{
//		StartByte: 0,
//		EndByte:   999,
//	}
//
//	mockConn := &mockConnection{}
//
//	mockProto := &mockProtocol{
//		canHandleFunc: func(url string) bool {
//			return url == "conn://example.com"
//		},
//		createConnectionFunc: func(ctx context.Context, url string, c *chunk.Chunk, config *downloader.Config) (connection.Connection, error) {
//			return mockConn, nil
//		},
//	}
//
//	h.RegisterProtocol(mockProto)
//
//	// Get the mock protocol via GetHandler
//	handler, err := h.GetHandler("conn://example.com")
//	if err != nil {
//		t.Fatalf("GetHandler error: %v", err)
//	}
//
//	// Create a connection using the handler
//	conn, err := handler.CreateConnection(ctx, "conn://example.com", mockChunk, nil)
//	if err != nil {
//		t.Fatalf("CreateConnection error: %v", err)
//	}
//
//	if conn != mockConn {
//		t.Error("expected to get our mock connection")
//	}
//}
//
//func TestCreateConnectionError(t *testing.T) {
//	h := protocol.NewHandler()
//	ctx := context.Background()
//
//	mockChunk := &chunk.Chunk{
//		StartByte: 0,
//		EndByte:   999,
//	}
//
//	expectedErr := errors.New("connection failed")
//
//	mockProto := &mockProtocol{
//		canHandleFunc: func(url string) bool {
//			return url == "conn-error://example.com"
//		},
//		createConnectionFunc: func(ctx context.Context, url string, c *chunk.Chunk, config *downloader.Config) (connection.Connection, error) {
//			return nil, expectedErr
//		},
//	}
//
//	h.RegisterProtocol(mockProto)
//
//	// Get the mock protocol
//	handler, err := h.GetHandler("conn-error://example.com")
//	if err != nil {
//		t.Fatalf("GetHandler error: %v", err)
//	}
//
//	// Try to create a connection
//	conn, err := handler.CreateConnection(ctx, "conn-error://example.com", mockChunk, nil)
//	if err == nil {
//		t.Error("expected an error, got nil")
//	}
//	if err != expectedErr {
//		t.Errorf("expected error %q, got %q", expectedErr, err)
//	}
//	if conn != nil {
//		t.Error("expected nil connection, got non-nil")
//	}
//}
