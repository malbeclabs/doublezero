package main

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_RequiresBrainPath(t *testing.T) {
	// Ensure MEMVID_BRAIN_PATH is not set
	os.Unsetenv("MEMVID_BRAIN_PATH")
	os.Unsetenv("SYNC_INTERVAL")

	_, err := loadConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MEMVID_BRAIN_PATH")
}

func TestLoadConfig_DefaultSyncInterval(t *testing.T) {
	os.Setenv("MEMVID_BRAIN_PATH", "/tmp/test.mv2")
	os.Unsetenv("SYNC_INTERVAL")
	defer os.Unsetenv("MEMVID_BRAIN_PATH")

	cfg, err := loadConfig()
	require.NoError(t, err)
	assert.Equal(t, "/tmp/test.mv2", cfg.MemvidBrainPath)
	assert.Equal(t, 5*time.Hour, cfg.SyncInterval)
}

func TestLoadConfig_CustomSyncInterval(t *testing.T) {
	os.Setenv("MEMVID_BRAIN_PATH", "/tmp/test.mv2")
	os.Setenv("SYNC_INTERVAL", "30m")
	defer os.Unsetenv("MEMVID_BRAIN_PATH")
	defer os.Unsetenv("SYNC_INTERVAL")

	cfg, err := loadConfig()
	require.NoError(t, err)
	assert.Equal(t, 30*time.Minute, cfg.SyncInterval)
}

func TestLoadConfig_InvalidSyncInterval(t *testing.T) {
	os.Setenv("MEMVID_BRAIN_PATH", "/tmp/test.mv2")
	os.Setenv("SYNC_INTERVAL", "invalid")
	defer os.Unsetenv("MEMVID_BRAIN_PATH")
	defer os.Unsetenv("SYNC_INTERVAL")

	_, err := loadConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid SYNC_INTERVAL")
}

func TestLoadConfig_NegativeSyncInterval(t *testing.T) {
	os.Setenv("MEMVID_BRAIN_PATH", "/tmp/test.mv2")
	os.Setenv("SYNC_INTERVAL", "-1h")
	defer os.Unsetenv("MEMVID_BRAIN_PATH")
	defer os.Unsetenv("SYNC_INTERVAL")

	_, err := loadConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be positive")
}

func TestTruncateForLog(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "short string unchanged",
			input:  "hello",
			maxLen: 10,
			want:   "hello",
		},
		{
			name:   "long string truncated",
			input:  "hello world this is a long string",
			maxLen: 10,
			want:   "hello worl...",
		},
		{
			name:   "exact length unchanged",
			input:  "hello",
			maxLen: 5,
			want:   "hello",
		},
		{
			name:   "whitespace trimmed",
			input:  "  hello  ",
			maxLen: 10,
			want:   "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateForLog(tt.input, tt.maxLen)
			assert.Equal(t, tt.want, got)
		})
	}
}
