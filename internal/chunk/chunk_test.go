package chunk_test

//
// import (
//	"context"
//	"errors"
//	"io"
//	"os"
//	"path/filepath"
//	"sync/atomic"
//	"testing"
//	"time"
//
//	"github.com/NamanBalaji/tdm/internal/chunk"
//	"github.com/NamanBalaji/tdm/internal/common"
//	"github.com/google/uuid"
//)
//
//type mockConnection struct {
//	readData       []byte
//	readPos        int
//	readError      error
//	readDelayTime  time.Duration
//	connectError   error
//	closeWasCalled bool
//	resetWasCalled bool
//	resetError     error
//	isAlive        bool
//}
//
//func (m *mockConnection) Connect(ctx context.Context) error {
//	if m.connectError != nil {
//		return m.connectError
//	}
//	m.isAlive = true
//	return nil
//}
//
//func (m *mockConnection) Read(ctx context.Context, p []byte) (int, error) {
//	select {
//	case <-ctx.Done():
//		return 0, ctx.Err()
//	default:
//	}
//
//	if m.readDelayTime > 0 {
//		select {
//		case <-time.After(m.readDelayTime):
//		case <-ctx.Done():
//			return 0, ctx.Err()
//		}
//	}
//
//	if m.readError != nil {
//		return 0, m.readError
//	}
//
//	if m.readPos >= len(m.readData) {
//		return 0, io.EOF
//	}
//
//	n := copy(p, m.readData[m.readPos:])
//	m.readPos += n
//	return n, nil
//}
//
//func (m *mockConnection) Close() error {
//	m.closeWasCalled = true
//	m.isAlive = false
//	return nil
//}
//
//func (m *mockConnection) IsAlive() bool {
//	return m.isAlive
//}
//
//func (m *mockConnection) Reset(ctx context.Context) error {
//	m.resetWasCalled = true
//	if m.resetError != nil {
//		return m.resetError
//	}
//	m.isAlive = true
//	m.readPos = 0
//	return nil
//}
//
//func (m *mockConnection) GetURL() string {
//	return "http://example.com/test"
//}
//
//func (m *mockConnection) GetHeaders() map[string]string {
//	return nil
//}
//
//func (m *mockConnection) SetTimeout(timeout time.Duration) {}
//
//func createTempDir(t *testing.T) string {
//	tempDir, err := os.MkdirTemp("", "chunk-test-*")
//	if err != nil {
//		t.Fatalf("Failed to create temp dir: %v", err)
//	}
//	return tempDir
//}
//
//func createProgressTracker() (func(int64), *int64) {
//	var totalProgress int64
//	progressFn := func(n int64) {
//		atomic.AddInt64(&totalProgress, n)
//	}
//	return progressFn, &totalProgress
//}
//
//func TestNewChunk(t *testing.T) {
//	downloadID := uuid.New()
//	startByte := int64(0)
//	endByte := int64(1023)
//	progressFn, _ := createProgressTracker()
//
//	c := chunk.NewChunk(downloadID, startByte, endByte, progressFn)
//
//	if c == nil {
//		t.Fatal("NewChunk returned nil")
//	}
//
//	if c.ID == uuid.Nil {
//		t.Error("Chunk ID should not be nil")
//	}
//
//	if c.DownloadID != downloadID {
//		t.Errorf("Expected DownloadID %s, got %s", downloadID, c.DownloadID)
//	}
//
//	if c.StartByte != startByte {
//		t.Errorf("Expected StartByte %d, got %d", startByte, c.StartByte)
//	}
//
//	if c.EndByte != endByte {
//		t.Errorf("Expected EndByte %d, got %d", endByte, c.EndByte)
//	}
//
//	if c.Status != common.StatusPending {
//		t.Errorf("Expected Status %s, got %s", common.StatusPending, c.Status)
//	}
//
//	if c.Size() != 1024 {
//		t.Errorf("Expected Size %d, got %d", 1024, c.Size())
//	}
//}
//
//func TestSize(t *testing.T) {
//	testCases := []struct {
//		name      string
//		startByte int64
//		endByte   int64
//		expected  int64
//	}{
//		{"Zero length", 0, 0, 1},
//		{"Small chunk", 0, 99, 100},
//		{"Medium chunk", 100, 1099, 1000},
//		{"Large chunk", 1000, 1000999, 1000000},
//	}
//
//	for _, tc := range testCases {
//		t.Run(tc.name, func(t *testing.T) {
//			c := chunk.NewChunk(uuid.New(), tc.startByte, tc.endByte, nil)
//			size := c.Size()
//			if size != tc.expected {
//				t.Errorf("Expected size %d, got %d", tc.expected, size)
//			}
//		})
//	}
//}
//
//func TestDownload_Success(t *testing.T) {
//	tempDir := createTempDir(t)
//	defer os.RemoveAll(tempDir)
//
//	downloadID := uuid.New()
//	progressFn, progress := createProgressTracker()
//
//	c := chunk.NewChunk(downloadID, 0, 999, progressFn)
//	c.TempFilePath = filepath.Join(tempDir, c.ID.String())
//
//	testData := make([]byte, 1000)
//	for i := range testData {
//		testData[i] = byte(i % 256)
//	}
//
//	conn := &mockConnection{
//		readData: testData,
//	}
//	c.Connection = conn
//
//	ctx := context.Background()
//	err := c.Download(ctx)
//
//	if err != nil {
//		t.Fatalf("Download returned error: %v", err)
//	}
//
//	if c.Status != common.StatusCompleted {
//		t.Errorf("Expected status %s, got %s", common.StatusCompleted, c.Status)
//	}
//
//	if c.Downloaded != 1000 {
//		t.Errorf("Expected Downloaded count 1000, got %d", c.Downloaded)
//	}
//
//	if *progress != 1000 {
//		t.Errorf("Expected progress callback total 1000, got %d", *progress)
//	}
//
//	fileData, err := os.ReadFile(c.TempFilePath)
//	if err != nil {
//		t.Fatalf("Failed to read temp file: %v", err)
//	}
//
//	if len(fileData) != len(testData) {
//		t.Errorf("File size mismatch: expected %d, got %d", len(testData), len(fileData))
//	}
//
//	for i := range testData {
//		if i < len(fileData) && fileData[i] != testData[i] {
//			t.Errorf("Data mismatch at position %d: expected %d, got %d", i, testData[i], fileData[i])
//			break
//		}
//	}
//}
//
//func TestDownload_WithContextCancellation(t *testing.T) {
//	tempDir := createTempDir(t)
//	defer os.RemoveAll(tempDir)
//
//	downloadID := uuid.New()
//	progressFn, _ := createProgressTracker()
//
//	c := chunk.NewChunk(downloadID, 0, 999999, progressFn)
//	c.TempFilePath = filepath.Join(tempDir, c.ID.String())
//
//	testData := make([]byte, 100000)
//	for i := range testData {
//		testData[i] = byte(i % 256)
//	}
//
//	conn := &mockConnection{
//		readData:      testData,
//		readDelayTime: 50 * time.Millisecond,
//	}
//	c.Connection = conn
//
//	ctx, cancel := context.WithCancel(context.Background())
//
//	downloadStarted := make(chan struct{})
//
//	errCh := make(chan error)
//	go func() {
//		close(downloadStarted)
//		errCh <- c.Download(ctx)
//	}()
//
//	<-downloadStarted
//
//	time.Sleep(100 * time.Millisecond)
//	cancel()
//
//	var err error
//	select {
//	case err = <-errCh:
//	case <-time.After(2 * time.Second):
//		t.Fatal("Download did not stop after context cancellation")
//	}
//
//	if !errors.Is(err, context.Canceled) {
//		t.Errorf("Expected context.Canceled error, got: %v", err)
//	}
//
//	if c.Status != common.StatusFailed {
//		t.Errorf("Expected status %s, got %s", common.StatusPaused, c.Status)
//	}
//}
//
//func TestDownload_WithError(t *testing.T) {
//	tempDir := createTempDir(t)
//	defer os.RemoveAll(tempDir)
//
//	downloadID := uuid.New()
//	progressFn, _ := createProgressTracker()
//
//	c := chunk.NewChunk(downloadID, 0, 999, progressFn)
//	c.TempFilePath = filepath.Join(tempDir, c.ID.String())
//
//	expectedErr := errors.New("simulated read error")
//
//	conn := &mockConnection{
//		readData:  []byte("some data"),
//		readError: expectedErr,
//	}
//	c.Connection = conn
//
//	ctx := context.Background()
//	err := c.Download(ctx)
//
//	if err == nil {
//		t.Fatal("Expected an error, got nil")
//	}
//
//	if !errors.Is(err, expectedErr) {
//		t.Errorf("Expected error %v, got %v", expectedErr, err)
//	}
//
//	if c.Status != common.StatusFailed {
//		t.Errorf("Expected status %s, got %s", common.StatusFailed, c.Status)
//	}
//
//	if c.Error == nil || !errors.Is(c.Error, expectedErr) {
//		t.Errorf("Chunk error not set correctly: %v", c.Error)
//	}
//}
//
//func TestDownload_Resume(t *testing.T) {
//	tempDir := createTempDir(t)
//	defer os.RemoveAll(tempDir)
//
//	downloadID := uuid.New()
//	progressFn, progress := createProgressTracker()
//
//	c := chunk.NewChunk(downloadID, 0, 999, progressFn)
//	c.TempFilePath = filepath.Join(tempDir, c.ID.String())
//
//	initialDownloaded := int64(500)
//	c.Downloaded = initialDownloaded
//
//	testData := make([]byte, 1000)
//	for i := range testData {
//		testData[i] = byte(i % 256)
//	}
//
//	err := os.WriteFile(c.TempFilePath, testData[:initialDownloaded], 0644)
//	if err != nil {
//		t.Fatalf("Failed to create initial file: %v", err)
//	}
//
//	conn := &mockConnection{
//		readData: testData[initialDownloaded:],
//	}
//	c.Connection = conn
//
//	ctx := context.Background()
//	err = c.Download(ctx)
//
//	if err != nil {
//		t.Fatalf("Download returned error: %v", err)
//	}
//
//	if c.Status != common.StatusCompleted {
//		t.Errorf("Expected status %s, got %s", common.StatusCompleted, c.Status)
//	}
//
//	if c.Downloaded != 1000 {
//		t.Errorf("Expected Downloaded count 1000, got %d", c.Downloaded)
//	}
//
//	if *progress != 500 {
//		t.Errorf("Expected progress callback total 500, got %d", *progress)
//	}
//
//	fileData, err := os.ReadFile(c.TempFilePath)
//	if err != nil {
//		t.Fatalf("Failed to read temp file: %v", err)
//	}
//
//	if len(fileData) != len(testData) {
//		t.Errorf("File size mismatch: expected %d, got %d", len(testData), len(fileData))
//	}
//}
//
//func TestReset(t *testing.T) {
//	downloadID := uuid.New()
//	progressFn, _ := createProgressTracker()
//
//	c := chunk.NewChunk(downloadID, 0, 999, progressFn)
//	c.Status = common.StatusFailed
//	c.Error = errors.New("previous error")
//	c.RetryCount = 2
//	c.Downloaded = 500
//
//	conn := &mockConnection{}
//	c.Connection = conn
//
//	c.Reset()
//
//	if c.Status != common.StatusPending {
//		t.Errorf("Expected status %s, got %s", common.StatusPending, c.Status)
//	}
//
//	if c.Error != nil {
//		t.Errorf("Expected Error to be nil, got %v", c.Error)
//	}
//
//	if c.Connection != nil {
//		t.Error("Expected Connection to be nil after reset")
//	}
//
//	if c.RetryCount != 3 {
//		t.Errorf("Expected RetryCount to be incremented to 3, got %d", c.RetryCount)
//	}
//
//	if !conn.closeWasCalled {
//		t.Error("Expected connection Close() to be called")
//	}
//
//	if c.Downloaded != 500 {
//		t.Errorf("Expected Downloaded to remain 500, got %d", c.Downloaded)
//	}
//}
//
//func TestVerifyIntegrity(t *testing.T) {
//	downloadID := uuid.New()
//	progressFn, _ := createProgressTracker()
//
//	testCases := []struct {
//		name       string
//		startByte  int64
//		endByte    int64
//		downloaded int64
//		expected   bool
//	}{
//		{"Complete", 0, 999, 1000, true},
//		{"Incomplete", 0, 999, 500, false},
//		{"Excess", 0, 999, 1200, false},
//		{"Zero size", 0, 0, 1, true},
//		{"Zero size incomplete", 0, 0, 0, false},
//	}
//
//	for _, tc := range testCases {
//		t.Run(tc.name, func(t *testing.T) {
//			c := chunk.NewChunk(downloadID, tc.startByte, tc.endByte, progressFn)
//			c.Downloaded = tc.downloaded
//
//			result := c.VerifyIntegrity()
//			if result != tc.expected {
//				t.Errorf("VerifyIntegrity returned %v, expected %v", result, tc.expected)
//			}
//		})
//	}
//}
//
//func TestDownload_FileCreateError(t *testing.T) {
//	invalidPath := filepath.Join("/nonexistent", "path", "to", "file")
//
//	downloadID := uuid.New()
//	progressFn, _ := createProgressTracker()
//
//	c := chunk.NewChunk(downloadID, 0, 999, progressFn)
//	c.TempFilePath = invalidPath
//
//	conn := &mockConnection{
//		readData: []byte("test"),
//	}
//	c.Connection = conn
//
//	ctx := context.Background()
//	err := c.Download(ctx)
//
//	if err == nil {
//		t.Fatal("Expected error due to invalid file path, got nil")
//	}
//
//	if c.Status != common.StatusFailed {
//		t.Errorf("Expected status %s, got %s", common.StatusFailed, c.Status)
//	}
//}
