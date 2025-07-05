package components_test

import (
	"strings"
	"testing"

	"github.com/NamanBalaji/tdm/internal/status"
	"github.com/NamanBalaji/tdm/internal/tui/components"
)

func TestProgressBar(t *testing.T) {
	testCases := []struct {
		name           string
		width          int
		percent        float64
		status         status.Status
		expectedFilled int
		expectedEmpty  int
	}{
		{
			name:           "0 percent",
			width:          20,
			percent:        0.0,
			status:         status.Active,
			expectedFilled: 0,
			expectedEmpty:  20,
		},
		{
			name:           "50 percent",
			width:          20,
			percent:        0.5,
			status:         status.Paused,
			expectedFilled: 10,
			expectedEmpty:  10,
		},
		{
			name:           "100 percent",
			width:          20,
			percent:        1.0,
			status:         status.Completed,
			expectedFilled: 20,
			expectedEmpty:  0,
		},
		{
			name:           "Negative percent (clamps to 0)",
			width:          10,
			percent:        -0.5,
			status:         status.Failed,
			expectedFilled: 0,
			expectedEmpty:  10,
		},
		{
			name:           "Over 100 percent (clamps to 1.0)",
			width:          10,
			percent:        1.5,
			status:         status.Cancelled,
			expectedFilled: 10,
			expectedEmpty:  0,
		},
		{
			name:           "Zero width",
			width:          0,
			percent:        0.5,
			status:         status.Queued,
			expectedFilled: 0,
			expectedEmpty:  0,
		},
		{
			name:           "Odd width, 33 percent",
			width:          15,
			percent:        0.33,
			status:         status.Active,
			expectedFilled: 4,
			expectedEmpty:  11,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			filledChar := "█"
			emptyChar := "░"

			output := components.ProgressBar(tc.width, tc.percent, tc.status)

			gotFilled := strings.Count(output, filledChar)
			gotEmpty := strings.Count(output, emptyChar)

			if gotFilled != tc.expectedFilled {
				t.Errorf("expected %d filled characters, but got %d", tc.expectedFilled, gotFilled)
			}
			if gotEmpty != tc.expectedEmpty {
				t.Errorf("expected %d empty characters, but got %d", tc.expectedEmpty, gotEmpty)
			}

			if tc.width == 0 && output != "" {
				t.Error("expected empty string for zero width, but got output")
			}
		})
	}
}
