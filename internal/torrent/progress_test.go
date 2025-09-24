package torrent_test

import (
	"testing"
	"time"

	"github.com/NamanBalaji/tdm/internal/torrent"
)

func TestProgress_Getters(t *testing.T) {
	p := torrent.Progress{
		TotalSize:  12345,
		Downloaded: 6789,
		Percentage: 54.3,
		SpeedBPS:   9999,
		ETA:        90 * time.Second,
	}

	t.Run("GetTotalSize", func(t *testing.T) {
		if got := p.GetTotalSize(); got != 12345 {
			t.Fatalf("GetTotalSize=%d want=12345", got)
		}
	})
	t.Run("GetDownloaded", func(t *testing.T) {
		if got := p.GetDownloaded(); got != 6789 {
			t.Fatalf("GetDownloaded=%d want=6789", got)
		}
	})
	t.Run("GetPercentage", func(t *testing.T) {
		if got := p.GetPercentage(); got != 54.3 {
			t.Fatalf("GetPercentage=%v want=54.3", got)
		}
	})
	t.Run("GetSpeedBPS", func(t *testing.T) {
		if got := p.GetSpeedBPS(); got != 9999 {
			t.Fatalf("GetSpeedBPS=%d want=9999", got)
		}
	})
}

func TestProgress_GetETA_Table(t *testing.T) {
	cases := []struct {
		name string
		eta  time.Duration
		want string
	}{
		{name: "unknown when zero", eta: 0, want: "unknown"},
		{name: "seconds only", eta: 45 * time.Second, want: "45s"},
		{name: "exact one minute", eta: 60 * time.Second, want: "1m 0s"},
		{name: "minutes and seconds", eta: 2*time.Minute + 5*time.Second, want: "2m 5s"},
		{name: "exact one hour", eta: 1 * time.Hour, want: "1h 0m 0s"},
		{name: "hours mins secs", eta: 3*time.Hour + 4*time.Minute + 5*time.Second, want: "3h 4m 5s"},
		{name: "minutes truncates seconds fraction", eta: 2*time.Minute + 59*time.Second + 900*time.Millisecond, want: "2m 59s"},
		{name: "seconds truncates sub-second", eta: 9*time.Second + 800*time.Millisecond, want: "9s"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			p := torrent.Progress{ETA: tc.eta}
			got := p.GetETA()
			if got != tc.want {
				t.Fatalf("GetETA(%v)=%q want=%q", tc.eta, got, tc.want)
			}
		})
	}
}
