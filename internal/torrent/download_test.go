package torrent_test

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NamanBalaji/tdm/internal/status"
	torrent "github.com/NamanBalaji/tdm/internal/torrent"
)

func Test_Type_And_GetID(t *testing.T) {
	t.Parallel()

	d := &torrent.Download{Id: uuid.New()}

	t.Run("Type", func(t *testing.T) {
		t.Parallel()
		if got := d.Type(); got != "torrent" {
			t.Fatalf("Type()=%q want %q", got, "torrent")
		}
	})

	t.Run("GetID concurrent reads", func(t *testing.T) {
		t.Parallel()
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if got := d.GetID(); got != d.Id {
					t.Errorf("GetID()=%v want %v", got, d.Id)
				}
			}()
		}
		wg.Wait()
	})
}

func Test_JSON_RoundTrip(t *testing.T) {
	t.Parallel()

	start := time.Now().UTC().Truncate(time.Millisecond)
	end := start.Add(75 * time.Minute)

	orig := &torrent.Download{
		Id:         uuid.New(),
		Name:       "ubuntu-24.04.iso",
		IsMagnet:   true,
		Url:        "magnet:?xt=urn:btih:deadbeef",
		TotalSize:  123456789,
		Downloaded: 55555,
		Uploaded:   777,
		Status:     status.Active,
		StartTime:  start,
		EndTime:    end,
		Protocol:   "torrent",
		Dir:        "/tmp",
		Priority:   3,
		InfoHash:   "deadbeefcafebabe",
	}

	// Marshal under light concurrency to exercise the read lock.
	var wg sync.WaitGroup
	var blobs [][]byte
	var mu sync.Mutex
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, err := orig.MarshalJSON()
			if err != nil {
				t.Errorf("MarshalJSON err: %v", err)
				return
			}
			mu.Lock()
			blobs = append(blobs, b)
			mu.Unlock()
		}()
	}
	wg.Wait()
	if len(blobs) == 0 {
		t.Fatal("no JSON blobs produced")
	}

	var got torrent.Download
	if err := got.UnmarshalJSON(blobs[len(blobs)-1]); err != nil {
		t.Fatalf("UnmarshalJSON err: %v", err)
	}

	gb, _ := json.Marshal(got)
	ob, _ := json.Marshal(orig)
	if string(gb) != string(ob) {
		t.Fatalf("round-trip mismatch\ngot : %s\nwant: %s", gb, ob)
	}
}

func Test_UnmarshalJSON_Error(t *testing.T) {
	t.Parallel()

	var d torrent.Download
	if err := d.UnmarshalJSON([]byte("{")); err == nil {
		t.Fatalf("expected error for invalid JSON")
	}
}
