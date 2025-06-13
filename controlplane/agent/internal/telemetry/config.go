package telemetry

import (
	"fmt"
	"time"
)

const (
	// DefaultListenPort for UDP telemetry service
	DefaultListenPort = 19999

	// DefaultSamplingInterval between RTT measurements
	DefaultSamplingInterval = 10 * time.Second

	// DefaultSubmissionInterval for batching samples
	DefaultSubmissionInterval = 60 * time.Second

	// DefaultMaxSamplesPerLink before rotation
	DefaultMaxSamplesPerLink = 10000

	// DefaultStoragePath for telemetry data
	DefaultStoragePath = "/var/lib/doublezero/telemetry"

	// PacketSize for ping packets (spec: 2048 bytes)
	PacketSize = 2048

	// InternetLinkPubkey is all 1s to indicate internet measurements
	InternetLinkPubkey = "11111111111111111111111111111111"
)

// ValidateConfig checks if the collector configuration is valid
func ValidateConfig(config CollectorConfig) error {
	if config.LocalDevicePubkey == "" {
		return fmt.Errorf("local device pubkey cannot be empty")
	}

	if config.LocalLocationPubkey == "" {
		return fmt.Errorf("local location pubkey cannot be empty")
	}

	if config.ListenPort < 1 || config.ListenPort > 65535 {
		return fmt.Errorf("listen port must be between 1 and 65535")
	}

	if config.SamplingIntervalSeconds < 1 {
		return fmt.Errorf("sampling interval must be at least 1 second")
	}

	if config.SubmissionIntervalSeconds < config.SamplingIntervalSeconds {
		return fmt.Errorf("submission interval must be >= sampling interval")
	}

	if config.MaxSamplesPerLink < 100 {
		return fmt.Errorf("max samples per link must be at least 100")
	}

	if config.StoragePath == "" {
		return fmt.Errorf("storage path cannot be empty")
	}

	return nil
}

// DefaultConfig returns a collector configuration with default values
func DefaultConfig() CollectorConfig {
	return CollectorConfig{
		ListenPort:                DefaultListenPort,
		SamplingIntervalSeconds:   int(DefaultSamplingInterval.Seconds()),
		SubmissionIntervalSeconds: int(DefaultSubmissionInterval.Seconds()),
		StoragePath:               DefaultStoragePath,
		MaxSamplesPerLink:         DefaultMaxSamplesPerLink,
		EnableInternetProbes:      true,
	}
}

// CalculateEpoch returns the current epoch number based on 2-day intervals
func CalculateEpoch(timestamp time.Time) uint64 {
	// Epoch 0 starts at Unix epoch (January 1, 1970)
	epochStart := time.Unix(0, 0)
	duration := timestamp.Sub(epochStart)
	// 2 days = 48 hours = 172800 seconds
	epochDuration := 48 * time.Hour
	return uint64(duration / epochDuration)
}

// GetEpochBounds returns the start and end times for a given epoch
func GetEpochBounds(epoch uint64) (start, end time.Time) {
	epochStart := time.Unix(0, 0)
	epochDuration := 48 * time.Hour

	start = epochStart.Add(time.Duration(epoch) * epochDuration)
	end = start.Add(epochDuration)
	return start, end
}

// CalculatePercentile calculates the Nth percentile of RTT samples
func CalculatePercentile(samples []uint32, percentile float64) uint32 {
	if len(samples) == 0 {
		return 0
	}

	if percentile < 0 || percentile > 100 {
		return 0
	}

	// Sort samples (make a copy to avoid modifying original)
	sorted := make([]uint32, len(samples))
	copy(sorted, samples)

	// Simple bubble sort for small datasets
	for i := range len(sorted) - 1 {
		for j := range len(sorted) - i - 1 {
			if sorted[j] > sorted[j+1] {
				sorted[j], sorted[j+1] = sorted[j+1], sorted[j]
			}
		}
	}

	// Calculate percentile index
	// For percentile calculation, we use ceiling to ensure we get the appropriate value
	// For example, 95th percentile of 10 values should be at index 9 (10th value)
	index := int(float64(len(sorted)-1) * percentile / 100.0)
	if percentile > 0 && float64(index) < float64(len(sorted)-1)*percentile/100.0 {
		index++
	}
	return sorted[index]
}

// FormatMicroseconds converts microseconds to a human-readable format
func FormatMicroseconds(micros uint32) string {
	if micros < 1000 {
		return fmt.Sprintf("%dµs", micros)
	} else if micros < 1000000 {
		return fmt.Sprintf("%.2fms", float64(micros)/1000.0)
	}
	return fmt.Sprintf("%.2fs", float64(micros)/1000000.0)
}
