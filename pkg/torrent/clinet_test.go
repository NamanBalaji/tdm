package torrent_test

import (
	"errors"
	"testing"

	"github.com/NamanBalaji/tdm/pkg/torrent"
	"github.com/anacrolix/torrent/metainfo"
)

func TestNewClient_GetClient_Close(t *testing.T) {
	t.Run("lifecycle", func(t *testing.T) {
		dir := t.TempDir()

		c, err := torrent.NewClient(dir)
		if err != nil {
			t.Fatalf("NewClient error: %v", err)
		}
		if c.GetClient() == nil {
			t.Fatalf("GetClient returned nil")
		}

		if err := c.Close(); err != nil {
			t.Fatalf("Close error: %v", err)
		}
		if err := c.Close(); !errors.Is(err, torrent.ErrNilClient) {
			t.Fatalf("expected ErrNilClient on second Close, got: %v", err)
		}
	})
}

func TestNilClientOperations(t *testing.T) {
	c := &torrent.Client{}

	type op func(*torrent.Client) error
	tests := []struct {
		name string
		call op
	}{
		{
			name: "AddTorrent on nil client",
			call: func(cl *torrent.Client) error {
				_, err := cl.AddTorrent((*metainfo.MetaInfo)(nil))
				return err
			},
		},
		{
			name: "AddMagnet on nil client",
			call: func(cl *torrent.Client) error {
				_, err := cl.AddMagnet("magnet:?xt=urn:btih:deadbeef")
				return err
			},
		},
		{
			name: "Close on nil client",
			call: func(cl *torrent.Client) error {
				return cl.Close()
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call(c)
			if !errors.Is(err, torrent.ErrNilClient) {
				t.Fatalf("expected ErrNilClient, got: %v", err)
			}
		})
	}
}
