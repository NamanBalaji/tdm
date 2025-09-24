package torrent_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

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
			name: "GetMetainfoFromMagnet on nil client",
			call: func(cl *torrent.Client) error {
				_, err := cl.GetMetainfoFromMagnet(context.Background(), "magnet:?xt=urn:btih:deadbeef")
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

func TestGetMetainfoFromMagnet_AddMagnetError(t *testing.T) {
	dir, err := os.MkdirTemp("", "tdm-torrent-*")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	c, err := torrent.NewClient(dir)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	tests := []struct {
		name string
		in   string
	}{
		{name: "not a magnet", in: "http://example.com/file"},
		{name: "empty string", in: ""},
		{name: "invalid scheme", in: "file:///tmp/foo"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.GetMetainfoFromMagnet(ctx, tc.in); err == nil {
				t.Fatalf("expected error for input %q, got nil", tc.in)
			}
		})
	}
}
