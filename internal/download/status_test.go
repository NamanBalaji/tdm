package download_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/NamanBalaji/tdm/internal/download"
)

func TestStatus_IsTerminal(t *testing.T) {
	tests := []struct {
		name   string
		status download.Status
		want   bool
	}{
		{"Pending is not terminal", download.Pending, false},
		{"Active is not terminal", download.Active, false},
		{"Paused is not terminal", download.Paused, false},
		{"Queued is not terminal", download.Queued, false},
		{"Completed is terminal", download.Completed, true},
		{"Failed is terminal", download.Failed, true},
		{"Cancelled is terminal", download.Cancelled, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.status.IsTerminal())
		})
	}
}

func TestStatus_String(t *testing.T) {
	tests := []struct {
		name   string
		status download.Status
		want   string
	}{
		{"Pending", download.Pending, "pending"},
		{"Active", download.Active, "active"},
		{"Paused", download.Paused, "paused"},
		{"Completed", download.Completed, "completed"},
		{"Failed", download.Failed, "failed"},
		{"Queued", download.Queued, "queued"},
		{"Cancelled", download.Cancelled, "cancelled"},
		{"Unknown status", download.Status(99), "unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.status.String())
		})
	}
}
