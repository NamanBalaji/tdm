package download_test

import (
	"encoding/json/v2"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NamanBalaji/tdm/internal/download"
)

// v1Fixture is byte-for-byte encoding/json (v1) output from before the
// json/v2 migration; existing bolt records must keep unmarshaling identically.
const v1Fixture = `{"id":"6ba7b810-9dad-11d1-80b4-00c04fd430c8","url":"https://example.com/f.iso","filename":"f.iso","dir":"/downloads","status":2,"priority":5,"type":"http","totalSize":1000,"downloaded":512,"startTime":"2025-08-01T10:00:00Z","endTime":"0001-01-01T00:00:00Z","createdAt":"2025-08-01T09:59:00.123456789Z","backendState":{"chunks":[{"id":"7d444840-9dc0-11d1-b245-5ffdce74fad2","startByte":0,"endByte":999,"downloaded":512,"completed":false,"tempFilePath":"/tmp/tdm/c0"}],"supportsRanges":true,"tempDir":"/tmp/tdm"}}`

func TestDownloadUnmarshalsV1Records(t *testing.T) {
	t.Parallel()

	var dl download.Download
	require.NoError(t, json.Unmarshal([]byte(v1Fixture), &dl))

	assert.Equal(t, uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8"), dl.ID)
	assert.Equal(t, "https://example.com/f.iso", dl.URL)
	assert.Equal(t, "f.iso", dl.Filename)
	assert.Equal(t, "/downloads", dl.Dir)
	assert.Equal(t, download.Paused, dl.Status)
	assert.Equal(t, 5, dl.Priority)
	assert.Equal(t, "http", dl.Type)
	assert.Equal(t, int64(1000), dl.TotalSize)
	assert.Equal(t, int64(512), dl.Downloaded)
	assert.Equal(t, time.Date(2025, 8, 1, 10, 0, 0, 0, time.UTC), dl.StartTime)
	assert.True(t, dl.EndTime.IsZero())
	assert.Equal(t, time.Date(2025, 8, 1, 9, 59, 0, 123456789, time.UTC), dl.CreatedAt)

	assert.JSONEq(t, `{"chunks":[{"id":"7d444840-9dc0-11d1-b245-5ffdce74fad2","startByte":0,"endByte":999,"downloaded":512,"completed":false,"tempFilePath":"/tmp/tdm/c0"}],"supportsRanges":true,"tempDir":"/tmp/tdm"}`, string(dl.State))
}

func TestDownloadRoundTrip(t *testing.T) {
	t.Parallel()

	var original download.Download
	require.NoError(t, json.Unmarshal([]byte(v1Fixture), &original))

	data, err := json.Marshal(&original)
	require.NoError(t, err)

	var restored download.Download
	require.NoError(t, json.Unmarshal(data, &restored))
	assert.Equal(t, original, restored)
}
