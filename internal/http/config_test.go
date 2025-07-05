package http

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()

	assert.Equal(t, 8, cfg.Connections, "Default connections should be 8")
	assert.Equal(t, 32, cfg.MaxChunks, "Default max chunks should be 32")
	assert.Equal(t, 3, cfg.MaxRetries, "Default max retries should be 3")
	assert.Equal(t, 2*time.Second, cfg.RetryDelay, "Default retry delay should be 2 seconds")
	assert.NotNil(t, cfg.Headers, "Default headers map should not be nil")
	assert.Empty(t, cfg.Headers, "Default headers map should be empty")
}

func TestConfigOptions(t *testing.T) {
	testCases := []struct {
		name     string
		option   ConfigOption
		expected func(*Config)
	}{
		{
			name:   "WithMaxChunks valid",
			option: WithMaxChunks(64),
			expected: func(c *Config) {
				c.MaxChunks = 64
			},
		},
		{
			name:   "WithMaxChunks zero",
			option: WithMaxChunks(0),
			expected: func(c *Config) {
				c.MaxChunks = 32 // Should fall back to default
			},
		},
		{
			name:   "WithMaxChunks negative",
			option: WithMaxChunks(-10),
			expected: func(c *Config) {
				c.MaxChunks = 32 // Should fall back to default
			},
		},
		{
			name:   "WithConnections",
			option: WithConnections(16),
			expected: func(c *Config) {
				c.Connections = 16
			},
		},
		{
			name:   "WithHeaders",
			option: WithHeaders(map[string]string{"User-Agent": "TestAgent"}),
			expected: func(c *Config) {
				c.Headers = map[string]string{"User-Agent": "TestAgent"}
			},
		},
		{
			name:   "WithMaxRetries",
			option: WithMaxRetries(5),
			expected: func(c *Config) {
				c.MaxRetries = 5
			},
		},
		{
			name:   "WithRetryDelay",
			option: WithRetryDelay(5 * time.Second),
			expected: func(c *Config) {
				c.RetryDelay = 5 * time.Second
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Start with a default config
			cfg := defaultConfig()

			// Apply the option being tested
			tc.option(cfg)

			// Create the expected config state
			expectedCfg := defaultConfig()
			tc.expected(expectedCfg)

			// Compare the modified config with the expected state
			if !reflect.DeepEqual(cfg, expectedCfg) {
				t.Errorf("Config mismatch:\nGot:    %+v\nWanted: %+v", cfg, expectedCfg)
			}
		})
	}
}
