package telemetry

import (
	"fmt"

	borsh "github.com/malbeclabs/doublezero/sdk/borsh-incremental/go"
)

type AccountType uint8

const (
	AccountTypeDeviceLatencySamplesV0   AccountType = 1
	AccountTypeInternetLatencySamplesV0 AccountType = 2
	AccountTypeDeviceLatencySamples     AccountType = 3
	AccountTypeInternetLatencySamples   AccountType = 4
	AccountTypeTimestampIndex           AccountType = 5
)

const (
	TelemetrySeedPrefix        = "telemetry"
	DeviceLatencySamplesSeed   = "dzlatency"
	InternetLatencySamplesSeed = "inetlatency"
	TimestampIndexSeed         = "tsindex"

	MaxDeviceLatencySamplesPerAccount   = 35_000
	MaxInternetLatencySamplesPerAccount = 3_000
	MaxTimestampIndexEntries            = 10_000

	deviceLatencyHeaderSize    = 1 + 8 + 32*6 + 8 + 8 + 4 + 128
	timestampIndexHeaderSize   = 1 + 32 + 4 + 64
	timestampIndexEntrySize    = 4 + 8
)

type DeviceLatencySamples struct {
	AccountType                  AccountType
	Epoch                        uint64
	OriginDeviceAgentPK          [32]byte
	OriginDevicePK               [32]byte
	TargetDevicePK               [32]byte
	OriginDeviceLocationPK       [32]byte
	TargetDeviceLocationPK       [32]byte
	LinkPK                       [32]byte
	SamplingIntervalMicroseconds uint64
	StartTimestampMicroseconds   uint64
	NextSampleIndex              uint32
	Samples                      []uint32
}

func DeserializeDeviceLatencySamples(data []byte) (*DeviceLatencySamples, error) {
	if len(data) < deviceLatencyHeaderSize {
		return nil, fmt.Errorf("data too short for device latency header: %d < %d", len(data), deviceLatencyHeaderSize)
	}

	r := borsh.NewReader(data)
	d := &DeviceLatencySamples{}

	v, _ := r.ReadU8()
	d.AccountType = AccountType(v)
	d.Epoch, _ = r.ReadU64()
	d.OriginDeviceAgentPK, _ = r.ReadPubkey()
	d.OriginDevicePK, _ = r.ReadPubkey()
	d.TargetDevicePK, _ = r.ReadPubkey()
	d.OriginDeviceLocationPK, _ = r.ReadPubkey()
	d.TargetDeviceLocationPK, _ = r.ReadPubkey()
	d.LinkPK, _ = r.ReadPubkey()
	d.SamplingIntervalMicroseconds, _ = r.ReadU64()
	d.StartTimestampMicroseconds, _ = r.ReadU64()
	d.NextSampleIndex, _ = r.ReadU32()

	_, _ = r.ReadBytes(128) // _unused

	count := int(d.NextSampleIndex)
	if count > MaxDeviceLatencySamplesPerAccount {
		return nil, fmt.Errorf("next_sample_index %d exceeds max %d", count, MaxDeviceLatencySamplesPerAccount)
	}

	d.Samples = make([]uint32, count)
	for i := range count {
		if r.Remaining() < 4 {
			break
		}
		d.Samples[i], _ = r.ReadU32()
	}

	return d, nil
}

type InternetLatencySamples struct {
	AccountType                  AccountType
	Epoch                        uint64
	DataProviderName             string
	OracleAgentPK                [32]byte
	OriginExchangePK             [32]byte
	TargetExchangePK             [32]byte
	SamplingIntervalMicroseconds uint64
	StartTimestampMicroseconds   uint64
	NextSampleIndex              uint32
	Samples                      []uint32
}

func DeserializeInternetLatencySamples(data []byte) (*InternetLatencySamples, error) {
	if len(data) < 10 {
		return nil, fmt.Errorf("data too short")
	}

	r := borsh.NewReader(data)
	d := &InternetLatencySamples{}

	v, _ := r.ReadU8()
	d.AccountType = AccountType(v)
	d.Epoch, _ = r.ReadU64()

	var err error
	d.DataProviderName, err = r.ReadString()
	if err != nil {
		return nil, fmt.Errorf("data_provider_name: %w", err)
	}

	d.OracleAgentPK, _ = r.ReadPubkey()
	d.OriginExchangePK, _ = r.ReadPubkey()
	d.TargetExchangePK, _ = r.ReadPubkey()
	d.SamplingIntervalMicroseconds, _ = r.ReadU64()
	d.StartTimestampMicroseconds, _ = r.ReadU64()
	d.NextSampleIndex, _ = r.ReadU32()

	_, _ = r.ReadBytes(128) // _unused

	count := int(d.NextSampleIndex)
	if count > MaxInternetLatencySamplesPerAccount {
		return nil, fmt.Errorf("next_sample_index %d exceeds max %d", count, MaxInternetLatencySamplesPerAccount)
	}

	d.Samples = make([]uint32, count)
	for i := range count {
		if r.Remaining() < 4 {
			break
		}
		d.Samples[i], _ = r.ReadU32()
	}

	return d, nil
}

type TimestampIndexEntry struct {
	SampleIndex           uint32
	TimestampMicroseconds uint64
}

type TimestampIndex struct {
	AccountType      AccountType
	SamplesAccountPK [32]byte
	NextEntryIndex   uint32
	Entries          []TimestampIndexEntry
}

func DeserializeTimestampIndex(data []byte) (*TimestampIndex, error) {
	if len(data) < timestampIndexHeaderSize {
		return nil, fmt.Errorf("data too short for timestamp index header: %d < %d", len(data), timestampIndexHeaderSize)
	}

	r := borsh.NewReader(data)
	d := &TimestampIndex{}

	v, _ := r.ReadU8()
	d.AccountType = AccountType(v)
	d.SamplesAccountPK, _ = r.ReadPubkey()
	d.NextEntryIndex, _ = r.ReadU32()

	_, _ = r.ReadBytes(64) // _unused

	count := int(d.NextEntryIndex)
	if count > MaxTimestampIndexEntries {
		return nil, fmt.Errorf("next_entry_index %d exceeds max %d", count, MaxTimestampIndexEntries)
	}

	d.Entries = make([]TimestampIndexEntry, count)
	for i := range count {
		if r.Remaining() < timestampIndexEntrySize {
			break
		}
		d.Entries[i].SampleIndex, _ = r.ReadU32()
		d.Entries[i].TimestampMicroseconds, _ = r.ReadU64()
	}

	return d, nil
}

// ReconstructTimestamp returns the wall-clock timestamp (in microseconds) for
// the sample at the given index, using the timestamp index entries and the
// sampling interval from the samples account header.
//
// Uses binary search over entries. O(log m) where m is the number of entries.
// If the timestamp index has no entries, falls back to the implicit model.
func ReconstructTimestamp(
	entries []TimestampIndexEntry,
	sampleIndex uint32,
	startTimestampMicroseconds uint64,
	samplingIntervalMicroseconds uint64,
) uint64 {
	if len(entries) == 0 {
		return startTimestampMicroseconds + uint64(sampleIndex)*samplingIntervalMicroseconds
	}

	// Binary search: find the last entry where SampleIndex <= sampleIndex.
	lo, hi := 0, len(entries)-1
	for lo < hi {
		mid := lo + (hi-lo+1)/2
		if entries[mid].SampleIndex <= sampleIndex {
			lo = mid
		} else {
			hi = mid - 1
		}
	}

	entry := entries[lo]
	return entry.TimestampMicroseconds + uint64(sampleIndex-entry.SampleIndex)*samplingIntervalMicroseconds
}

// ReconstructTimestamps returns wall-clock timestamps (in microseconds) for all
// samples, using the timestamp index to correct for gaps.
//
// Single-pass O(n + m) where n is sampleCount and m is the number of entries.
func ReconstructTimestamps(
	sampleCount uint32,
	entries []TimestampIndexEntry,
	startTimestampMicroseconds uint64,
	samplingIntervalMicroseconds uint64,
) []uint64 {
	timestamps := make([]uint64, sampleCount)
	if sampleCount == 0 {
		return timestamps
	}

	entryIdx := 0
	for i := range sampleCount {
		// Advance to the last entry that covers this sample index.
		for entryIdx+1 < len(entries) && entries[entryIdx+1].SampleIndex <= i {
			entryIdx++
		}

		if len(entries) == 0 {
			timestamps[i] = startTimestampMicroseconds + uint64(i)*samplingIntervalMicroseconds
		} else {
			e := entries[entryIdx]
			timestamps[i] = e.TimestampMicroseconds + uint64(i-e.SampleIndex)*samplingIntervalMicroseconds
		}
	}
	return timestamps
}
