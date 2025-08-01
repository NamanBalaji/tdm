package metainfo_test

import (
	"encoding/hex"
	"github.com/NamanBalaji/tdm/pkg/torrent/metainfo"
	"strings"
	"testing"
)

func TestParseMagnet(t *testing.T) {
	validHex := "aabbccddeeff00112233445566778899aabbccdd"
	validBase32 := "VK54ZXPO74ABCIRTIRKWM54ITGVLXTG5"

	expectedHash := [20]byte{}
	hex.Decode(expectedHash[:], []byte(validHex))

	tests := []struct {
		name        string
		input       string
		expected    *metainfo.Magnet
		expectError bool
		errorMsg    string
	}{
		{
			name:  "valid magnet with hex hash",
			input: "magnet:?xt=urn:btih:" + validHex,
			expected: &metainfo.Magnet{
				InfoHash:    expectedHash,
				DisplayName: "",
				Trackers:    []string{},
			},
			expectError: false,
		},
		{
			name:  "valid magnet with base32 hash",
			input: "magnet:?xt=urn:btih:" + validBase32,
			expected: &metainfo.Magnet{
				InfoHash:    expectedHash,
				DisplayName: "",
				Trackers:    []string{},
			},
			expectError: false,
		},
		{
			name:  "valid magnet with display name",
			input: "magnet:?xt=urn:btih:" + validHex + "&dn=Test%20File",
			expected: &metainfo.Magnet{
				InfoHash:    expectedHash,
				DisplayName: "Test File",
				Trackers:    []string{},
			},
			expectError: false,
		},
		{
			name:  "valid magnet with single tracker",
			input: "magnet:?xt=urn:btih:" + validHex + "&tr=http://tracker.example.com:8080/announce",
			expected: &metainfo.Magnet{
				InfoHash:    expectedHash,
				DisplayName: "",
				Trackers:    []string{"http://tracker.example.com:8080/announce"},
			},
			expectError: false,
		},
		{
			name:  "valid magnet with multiple trackers",
			input: "magnet:?xt=urn:btih:" + validHex + "&tr=http://tracker1.example.com:8080/announce&tr=http://tracker2.example.com:8080/announce",
			expected: &metainfo.Magnet{
				InfoHash:    expectedHash,
				DisplayName: "",
				Trackers: []string{
					"http://tracker1.example.com:8080/announce",
					"http://tracker2.example.com:8080/announce",
				},
			},
			expectError: false,
		},
		{
			name:  "valid magnet with all parameters",
			input: "magnet:?xt=urn:btih:" + validHex + "&dn=Complete%20Test&tr=http://tracker1.example.com:8080/announce&tr=http://tracker2.example.com:8080/announce",
			expected: &metainfo.Magnet{
				InfoHash:    expectedHash,
				DisplayName: "Complete Test",
				Trackers: []string{
					"http://tracker1.example.com:8080/announce",
					"http://tracker2.example.com:8080/announce",
				},
			},
			expectError: false,
		},
		{
			name:  "valid magnet with empty tracker (should be filtered)",
			input: "magnet:?xt=urn:btih:" + validHex + "&tr=http://tracker.example.com:8080/announce&tr=",
			expected: &metainfo.Magnet{
				InfoHash:    expectedHash,
				DisplayName: "",
				Trackers:    []string{"http://tracker.example.com:8080/announce"},
			},
			expectError: false,
		},
		{
			name:  "valid magnet with unicode display name",
			input: "magnet:?xt=urn:btih:" + validHex + "&dn=%E6%B8%AC%E8%A9%A6", // "測試" in UTF-8
			expected: &metainfo.Magnet{
				InfoHash:    expectedHash,
				DisplayName: "測試",
				Trackers:    []string{},
			},
			expectError: false,
		},
		{
			name:        "not a magnet link",
			input:       "http://example.com",
			expected:    nil,
			expectError: true,
			errorMsg:    "not a magnet link",
		},
		{
			name:        "empty string",
			input:       "",
			expected:    nil,
			expectError: true,
			errorMsg:    "not a magnet link",
		},
		{
			name:        "missing xt parameter",
			input:       "magnet:?dn=Test",
			expected:    nil,
			expectError: true,
			errorMsg:    "missing or invalid xt parameter",
		},
		{
			name:        "invalid xt parameter format",
			input:       "magnet:?xt=invalid:" + validHex,
			expected:    nil,
			expectError: true,
			errorMsg:    "missing or invalid xt parameter",
		},
		{
			name:        "empty xt parameter",
			input:       "magnet:?xt=",
			expected:    nil,
			expectError: true,
			errorMsg:    "missing or invalid xt parameter",
		},
		{
			name:        "invalid hash - too short",
			input:       "magnet:?xt=urn:btih:aabbcc",
			expected:    nil,
			expectError: true,
			errorMsg:    "info_hash length invalid",
		},
		{
			name:        "invalid hash - too long",
			input:       "magnet:?xt=urn:btih:aabbccddeeff00112233445566778899aabbccddee",
			expected:    nil,
			expectError: true,
			errorMsg:    "info_hash length invalid",
		},
		{
			name:        "invalid hex characters",
			input:       "magnet:?xt=urn:btih:zzbccddeeff00112233445566778899aabbccdd",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "invalid base32 characters",
			input:       "magnet:?xt=urn:btih:1KV4Y3XPYBARA4I4HFJHQVP3RGMU6VLN", // '1' is invalid in base32
			expected:    nil,
			expectError: true,
		},
		{
			name:  "lowercase base32 (should work after uppercase conversion)",
			input: "magnet:?xt=urn:btih:" + strings.ToLower(validBase32),
			expected: &metainfo.Magnet{
				InfoHash:    expectedHash,
				DisplayName: "",
				Trackers:    []string{},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := metainfo.ParseMagnet(tt.input)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
					return
				}
				if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result == nil {
				t.Errorf("expected result but got nil")
				return
			}

			if result.InfoHash != tt.expected.InfoHash {
				t.Errorf("InfoHash mismatch:\nexpected: %x\ngot:      %x", tt.expected.InfoHash, result.InfoHash)
			}

			if result.DisplayName != tt.expected.DisplayName {
				t.Errorf("DisplayName mismatch:\nexpected: %q\ngot:      %q", tt.expected.DisplayName, result.DisplayName)
			}

			if len(result.Trackers) != len(tt.expected.Trackers) {
				t.Errorf("Trackers length mismatch:\nexpected: %d\ngot:      %d", len(tt.expected.Trackers), len(result.Trackers))
			} else {
				for i, tracker := range result.Trackers {
					if tracker != tt.expected.Trackers[i] {
						t.Errorf("Tracker[%d] mismatch:\nexpected: %q\ngot:      %q", i, tt.expected.Trackers[i], tracker)
					}
				}
			}
		})
	}
}
