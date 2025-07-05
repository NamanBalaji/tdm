package http

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	httpPkg "github.com/NamanBalaji/tdm/pkg/http"
)

func TestConnection_ConnectAndRead(t *testing.T) {
	const testData = "0123456789abcdefghijklmnopqrstuvwxyz"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(testData)))
			w.WriteHeader(http.StatusOK)
			_, err := io.WriteString(w, testData)
			assert.NoError(t, err)
			return
		}

		var start, end int
		_, err := fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end)
		require.NoError(t, err, "Server should parse range header")

		if start > len(testData) || end >= len(testData) || start > end {
			http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
			return
		}

		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(testData)))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", end-start+1))
		w.WriteHeader(http.StatusPartialContent)
		_, err = io.WriteString(w, testData[start:end+1])
		assert.NoError(t, err)
	}))
	defer server.Close()

	client := httpPkg.NewClient()

	t.Run("successful connection and read with range", func(t *testing.T) {
		headers := map[string]string{"Range": "bytes=10-19"}
		conn := newConnection(server.URL, headers, client, 10, 19)
		defer assert.NoError(t, conn.close())

		err := conn.connect(context.Background())
		require.NoError(t, err)
		require.NotNil(t, conn.response)
		assert.Equal(t, http.StatusPartialContent, conn.response.StatusCode)

		buffer := make([]byte, 10)
		n, err := io.ReadFull(conn.response.Body, buffer)
		require.NoError(t, err)
		assert.Equal(t, 10, n)
		assert.Equal(t, "abcdefghij", string(buffer))
	})

	t.Run("read entire content in chunks", func(t *testing.T) {
		conn := newConnection(server.URL, nil, client, 0, int64(len(testData)-1))
		defer assert.NoError(t, conn.close())

		err := conn.connect(context.Background())
		require.NoError(t, err)
		require.NotNil(t, conn.response)

		content, err := io.ReadAll(conn.response.Body)
		require.NoError(t, err)
		assert.Equal(t, testData, string(content))
	})
}

func TestConnection_FallbackNoRangeSupport(t *testing.T) {
	const testData = "0123456789abcdefghijklmnopqrstuvwxyz"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(testData)))
		w.WriteHeader(http.StatusOK)
		_, err := io.WriteString(w, testData)
		assert.NoError(t, err)
	}))
	defer server.Close()

	client := httpPkg.NewClient()

	t.Run("skips bytes to start reading from the correct offset", func(t *testing.T) {
		startByte := int64(10)
		endByte := int64(19)
		headers := map[string]string{"Range": fmt.Sprintf("bytes=%d-%d", startByte, endByte)}

		conn := newConnection(server.URL, headers, client, startByte, endByte)
		defer assert.NoError(t, conn.close())

		err := conn.connect(context.Background())
		require.NoError(t, err)
		require.NotNil(t, conn.response)
		assert.Equal(t, http.StatusOK, conn.response.StatusCode)

		buffer := make([]byte, 10)
		n, err := io.ReadFull(conn.response.Body, buffer)
		require.NoError(t, err)
		assert.Equal(t, 10, n)
		assert.Equal(t, "abcdefghij", string(buffer))
	})
}

func TestConnection_Errors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "notfound") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if strings.Contains(r.URL.Path, "slow") {
			time.Sleep(200 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := httpPkg.NewClient()

	t.Run("server returns 404", func(t *testing.T) {
		conn := newConnection(server.URL+"/notfound", nil, client, 0, 0)
		err := conn.connect(context.Background())
		require.Error(t, err)
		assert.ErrorIs(t, err, httpPkg.ErrResourceNotFound)
	})

	t.Run("server returns 500", func(t *testing.T) {
		conn := newConnection(server.URL+"/servererror", nil, client, 0, 0)
		err := conn.connect(context.Background())
		require.Error(t, err)
		assert.ErrorIs(t, err, httpPkg.ErrServerProblem)
	})

	t.Run("context cancelled during slow request", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		conn := newConnection(server.URL+"/slow", nil, client, 0, 0)
		err := conn.connect(ctx)
		require.Error(t, err)
		assert.ErrorIs(t, err, httpPkg.ErrTimeout)
	})

	t.Run("invalid URL", func(t *testing.T) {
		conn := newConnection("://invalid-url", nil, client, 0, 0)
		err := conn.connect(context.Background())
		require.Error(t, err)
		assert.ErrorIs(t, err, httpPkg.ErrRequestCreation)
	})
}

func TestConnection_Close(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := httpPkg.NewClient()

	t.Run("close before connect is a no-op", func(t *testing.T) {
		conn := newConnection(server.URL, nil, client, 0, 0)
		err := conn.close()
		assert.NoError(t, err)
	})

	t.Run("close after connect closes the body", func(t *testing.T) {
		conn := newConnection(server.URL, nil, client, 0, 0)
		err := conn.connect(context.Background())
		require.NoError(t, err)

		err = conn.close()
		assert.NoError(t, err)

		buffer := make([]byte, 1)
		_, err = conn.response.Body.Read(buffer)
		assert.Error(t, err)
	})
}
