package telemetry

import (
	"testing"
	"time"
)

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  CollectorConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "Valid config",
			config: CollectorConfig{
				LocalDevicePubkey:         "device1",
				LocalLocationPubkey:       "location1",
				ListenPort:                19999,
				SamplingIntervalSeconds:   10,
				SubmissionIntervalSeconds: 60,
				StoragePath:               "/tmp/telemetry",
				MaxSamplesPerLink:         1000,
			},
			wantErr: false,
		},
		{
			name: "Empty device pubkey",
			config: CollectorConfig{
				LocalLocationPubkey:       "location1",
				ListenPort:                19999,
				SamplingIntervalSeconds:   10,
				SubmissionIntervalSeconds: 60,
				StoragePath:               "/tmp/telemetry",
				MaxSamplesPerLink:         1000,
			},
			wantErr: true,
			errMsg:  "local device pubkey cannot be empty",
		},
		{
			name: "Empty location pubkey",
			config: CollectorConfig{
				LocalDevicePubkey:         "device1",
				ListenPort:                19999,
				SamplingIntervalSeconds:   10,
				SubmissionIntervalSeconds: 60,
				StoragePath:               "/tmp/telemetry",
				MaxSamplesPerLink:         1000,
			},
			wantErr: true,
			errMsg:  "local location pubkey cannot be empty",
		},
		{
			name: "Invalid port - too low",
			config: CollectorConfig{
				LocalDevicePubkey:         "device1",
				LocalLocationPubkey:       "location1",
				ListenPort:                0,
				SamplingIntervalSeconds:   10,
				SubmissionIntervalSeconds: 60,
				StoragePath:               "/tmp/telemetry",
				MaxSamplesPerLink:         1000,
			},
			wantErr: true,
			errMsg:  "listen port must be between 1 and 65535",
		},
		{
			name: "Invalid port - too high",
			config: CollectorConfig{
				LocalDevicePubkey:         "device1",
				LocalLocationPubkey:       "location1",
				ListenPort:                70000,
				SamplingIntervalSeconds:   10,
				SubmissionIntervalSeconds: 60,
				StoragePath:               "/tmp/telemetry",
				MaxSamplesPerLink:         1000,
			},
			wantErr: true,
			errMsg:  "listen port must be between 1 and 65535",
		},
		{
			name: "Invalid sampling interval",
			config: CollectorConfig{
				LocalDevicePubkey:         "device1",
				LocalLocationPubkey:       "location1",
				ListenPort:                19999,
				SamplingIntervalSeconds:   0,
				SubmissionIntervalSeconds: 60,
				StoragePath:               "/tmp/telemetry",
				MaxSamplesPerLink:         1000,
			},
			wantErr: true,
			errMsg:  "sampling interval must be at least 1 second",
		},
		{
			name: "Submission interval less than sampling",
			config: CollectorConfig{
				LocalDevicePubkey:         "device1",
				LocalLocationPubkey:       "location1",
				ListenPort:                19999,
				SamplingIntervalSeconds:   60,
				SubmissionIntervalSeconds: 30,
				StoragePath:               "/tmp/telemetry",
				MaxSamplesPerLink:         1000,
			},
			wantErr: true,
			errMsg:  "submission interval must be >= sampling interval",
		},
		{
			name: "Max samples too low",
			config: CollectorConfig{
				LocalDevicePubkey:         "device1",
				LocalLocationPubkey:       "location1",
				ListenPort:                19999,
				SamplingIntervalSeconds:   10,
				SubmissionIntervalSeconds: 60,
				StoragePath:               "/tmp/telemetry",
				MaxSamplesPerLink:         50,
			},
			wantErr: true,
			errMsg:  "max samples per link must be at least 100",
		},
		{
			name: "Empty storage path",
			config: CollectorConfig{
				LocalDevicePubkey:         "device1",
				LocalLocationPubkey:       "location1",
				ListenPort:                19999,
				SamplingIntervalSeconds:   10,
				SubmissionIntervalSeconds: 60,
				MaxSamplesPerLink:         1000,
			},
			wantErr: true,
			errMsg:  "storage path cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errMsg != "" && err.Error() != tt.errMsg {
				t.Errorf("ValidateConfig() error = %v, want %v", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.ListenPort != DefaultListenPort {
		t.Errorf("Expected default port %d, got %d", DefaultListenPort, config.ListenPort)
	}

	if config.SamplingIntervalSeconds != int(DefaultSamplingInterval.Seconds()) {
		t.Errorf("Expected default sampling interval %d, got %d",
			int(DefaultSamplingInterval.Seconds()), config.SamplingIntervalSeconds)
	}

	if config.SubmissionIntervalSeconds != int(DefaultSubmissionInterval.Seconds()) {
		t.Errorf("Expected default submission interval %d, got %d",
			int(DefaultSubmissionInterval.Seconds()), config.SubmissionIntervalSeconds)
	}

	if config.StoragePath != DefaultStoragePath {
		t.Errorf("Expected default storage path %s, got %s", DefaultStoragePath, config.StoragePath)
	}

	if config.MaxSamplesPerLink != DefaultMaxSamplesPerLink {
		t.Errorf("Expected default max samples %d, got %d",
			DefaultMaxSamplesPerLink, config.MaxSamplesPerLink)
	}

	if !config.EnableInternetProbes {
		t.Error("Expected internet probes to be enabled by default")
	}
}

func TestCalculateEpoch(t *testing.T) {
	tests := []struct {
		name      string
		timestamp time.Time
		expected  uint64
	}{
		{
			name:      "Unix epoch",
			timestamp: time.Unix(0, 0),
			expected:  0,
		},
		{
			name:      "One day",
			timestamp: time.Unix(86400, 0), // 1 day in seconds
			expected:  0,                   // Still in epoch 0
		},
		{
			name:      "Two days",
			timestamp: time.Unix(172800, 0), // 2 days in seconds
			expected:  1,                    // Start of epoch 1
		},
		{
			name:      "Ten days",
			timestamp: time.Unix(864000, 0), // 10 days in seconds
			expected:  5,                    // 10/2 = 5
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateEpoch(tt.timestamp)
			if result != tt.expected {
				t.Errorf("CalculateEpoch(%v) = %d, want %d", tt.timestamp, result, tt.expected)
			}
		})
	}
}

func TestGetEpochBounds(t *testing.T) {
	tests := []struct {
		name      string
		epoch     uint64
		wantStart time.Time
		wantEnd   time.Time
	}{
		{
			name:      "Epoch 0",
			epoch:     0,
			wantStart: time.Unix(0, 0),
			wantEnd:   time.Unix(172800, 0), // 2 days
		},
		{
			name:      "Epoch 1",
			epoch:     1,
			wantStart: time.Unix(172800, 0), // 2 days
			wantEnd:   time.Unix(345600, 0), // 4 days
		},
		{
			name:      "Epoch 10",
			epoch:     10,
			wantStart: time.Unix(1728000, 0), // 20 days
			wantEnd:   time.Unix(1900800, 0), // 22 days
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := GetEpochBounds(tt.epoch)
			if !start.Equal(tt.wantStart) {
				t.Errorf("GetEpochBounds(%d) start = %v, want %v", tt.epoch, start, tt.wantStart)
			}
			if !end.Equal(tt.wantEnd) {
				t.Errorf("GetEpochBounds(%d) end = %v, want %v", tt.epoch, end, tt.wantEnd)
			}
		})
	}
}

func TestCalculatePercentile(t *testing.T) {
	tests := []struct {
		name       string
		samples    []uint32
		percentile float64
		expected   uint32
	}{
		{
			name:       "Empty samples",
			samples:    []uint32{},
			percentile: 50,
			expected:   0,
		},
		{
			name:       "Single sample",
			samples:    []uint32{100},
			percentile: 50,
			expected:   100,
		},
		{
			name:       "50th percentile (median)",
			samples:    []uint32{100, 200, 300, 400, 500},
			percentile: 50,
			expected:   300,
		},
		{
			name:       "95th percentile",
			samples:    []uint32{100, 200, 300, 400, 500, 600, 700, 800, 900, 1000},
			percentile: 95,
			expected:   1000,
		},
		{
			name:       "0th percentile (minimum)",
			samples:    []uint32{100, 200, 300, 400, 500},
			percentile: 0,
			expected:   100,
		},
		{
			name:       "100th percentile (maximum)",
			samples:    []uint32{100, 200, 300, 400, 500},
			percentile: 100,
			expected:   500,
		},
		{
			name:       "Invalid percentile negative",
			samples:    []uint32{100, 200, 300},
			percentile: -10,
			expected:   0,
		},
		{
			name:       "Invalid percentile over 100",
			samples:    []uint32{100, 200, 300},
			percentile: 110,
			expected:   0,
		},
		{
			name:       "Unsorted samples",
			samples:    []uint32{500, 100, 400, 200, 300},
			percentile: 50,
			expected:   300,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculatePercentile(tt.samples, tt.percentile)
			if result != tt.expected {
				t.Errorf("CalculatePercentile(%v, %.1f) = %d, want %d",
					tt.samples, tt.percentile, result, tt.expected)
			}
		})
	}
}

func TestFormatMicroseconds(t *testing.T) {
	tests := []struct {
		micros   uint32
		expected string
	}{
		{100, "100µs"},
		{999, "999µs"},
		{1000, "1.00ms"},
		{1500, "1.50ms"},
		{999999, "1000.00ms"},
		{1000000, "1.00s"},
		{2500000, "2.50s"},
	}

	for _, tt := range tests {
		result := FormatMicroseconds(tt.micros)
		if result != tt.expected {
			t.Errorf("FormatMicroseconds(%d) = %s, want %s", tt.micros, result, tt.expected)
		}
	}
}

func TestConstants(t *testing.T) {
	// Verify constants match telemetry.md specifications
	if PacketSize != 2048 {
		t.Errorf("Expected packet size 2048, got %d", PacketSize)
	}

	if len(InternetLinkPubkey) != 32 {
		t.Errorf("Expected internet link pubkey length 32, got %d", len(InternetLinkPubkey))
	}

	// Verify all 1s
	for _, ch := range InternetLinkPubkey {
		if ch != '1' {
			t.Errorf("Internet link pubkey should be all 1s, found %c", ch)
			break
		}
	}
}
