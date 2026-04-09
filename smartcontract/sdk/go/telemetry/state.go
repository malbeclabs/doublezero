package telemetry

import (
	"fmt"
	"io"

	bin "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
)

type AccountType uint8

const (
	AccountTypeDeviceLatencySamplesV0 AccountType = iota + 1
	AccountTypeInternetLatencySamplesV0
	AccountTypeDeviceLatencySamples
	AccountTypeInternetLatencySamples
	AccountTypeTimestampIndex
)

// Covers both V0 (350 bytes) and V1 (349 bytes) header layouts.
const DeviceLatencySamplesHeaderSize = 350

type DeviceLatencySamplesHeaderOnlyAccountType struct {
	AccountType AccountType // 1
}

func (d *DeviceLatencySamplesHeaderOnlyAccountType) Serialize(w io.Writer) error {
	enc := bin.NewBorshEncoder(w)
	if err := enc.Encode(d.AccountType); err != nil {
		return err
	}
	return nil
}

func (d *DeviceLatencySamplesHeaderOnlyAccountType) Deserialize(data []byte) error {
	dec := bin.NewBorshDecoder(data)
	if err := dec.Decode(&d.AccountType); err != nil {
		return err
	}

	return nil
}

type DeviceLatencySamplesHeader struct {
	// Used to distinguish this account type during deserialization
	AccountType AccountType // 1

	// Epoch number in which samples were collected
	Epoch uint64 // 8

	// Agent authorized to write RTT samples (must match signer)
	OriginDeviceAgentPK solana.PublicKey // 32

	// Device initiating sampling
	OriginDevicePK solana.PublicKey // 32

	// Destination device in RTT path
	TargetDevicePK solana.PublicKey // 32

	// Cached location of origin device for query/UI optimization
	OriginDeviceLocationPK solana.PublicKey // 32

	// Cached location of target device
	TargetDeviceLocationPK solana.PublicKey // 32

	// Link over which the RTT samples were taken
	LinkPK solana.PublicKey // 32

	// Sampling interval configured by the agent (in microseconds)
	SamplingIntervalMicroseconds uint64 // 8

	// Timestamp of the first written sample (µs since UNIX epoch).
	// Set on the first write, remains unchanged after.
	StartTimestampMicroseconds uint64 // 8

	// Tracks how many samples have been appended.
	NextSampleIndex uint32 // 4

	// Version string of the telemetry agent that created this account.
	AgentVersion [16]uint8 // 16

	// Short git commit hash of the telemetry agent binary.
	AgentCommit [8]uint8 // 8

	// Reserved for future use.
	Unused [104]uint8 // 104
}

type DeviceLatencySamples struct {
	DeviceLatencySamplesHeader
	Samples []uint32 // 4 + n*4 (RTT values in microseconds)
}

func (d *DeviceLatencySamples) Serialize(w io.Writer) error {
	enc := bin.NewBorshEncoder(w)
	if err := enc.Encode(d.DeviceLatencySamplesHeader); err != nil {
		return err
	}
	for _, sample := range d.Samples {
		if err := enc.Encode(sample); err != nil {
			return err
		}
	}
	return nil
}

func (d *DeviceLatencySamples) Deserialize(data []byte) error {
	dec := bin.NewBorshDecoder(data)
	if err := dec.Decode(&d.DeviceLatencySamplesHeader); err != nil {
		return err
	}

	if d.DeviceLatencySamplesHeader.NextSampleIndex > MaxDeviceLatencySamplesPerAccount {
		return fmt.Errorf("next sample index %d exceeds max allowed samples %d", d.DeviceLatencySamplesHeader.NextSampleIndex, MaxDeviceLatencySamplesPerAccount)
	}

	d.Samples = make([]uint32, d.DeviceLatencySamplesHeader.NextSampleIndex)
	for i := 0; i < int(d.DeviceLatencySamplesHeader.NextSampleIndex); i++ {
		if err := dec.Decode(&d.Samples[i]); err != nil {
			return err
		}
	}
	return nil
}

type InternetLatencySamplesHeader struct {
	// AccountType is used to distinguish this account type during deserialization.
	AccountType AccountType // 1

	// Epoch is the epoch number in which samples were collected.
	Epoch uint64 // 8

	// DataProviderName is the name of the data provider.
	DataProviderName string // 4 + len

	// OracleAgentPK authorized to write latency samples (must match signer)
	OracleAgentPK solana.PublicKey // 32

	// OriginExchangePK is the dz exchange of the origin for sample collection.
	OriginExchangePK solana.PublicKey // 32

	// TargetExchangePK is the dz exchange of the target for sample collection.
	TargetExchangePK solana.PublicKey // 32

	// SamplingIntervalMicroseconds is the interval between samples (in microseconds).
	SamplingIntervalMicroseconds uint64 // 8

	// StartTimestampMicroseconds is the timestamp of the first written sample (µs since UNIX epoch).
	// Set on the first write, remains unchanged after.
	StartTimestampMicroseconds uint64 // 8

	// NextSampleIndex tracks how many samples have been appended.
	NextSampleIndex uint32 // 4

	// Unused is reserved for future use.
	Unused [128]uint8 // 128
}

type InternetLatencySamples struct {
	InternetLatencySamplesHeader
	Samples []uint32 // 4 + n*4 (RTT values in microseconds)
}

func (d *InternetLatencySamples) Serialize(w io.Writer) error {
	enc := bin.NewBorshEncoder(w)
	if err := enc.Encode(d.InternetLatencySamplesHeader); err != nil {
		return err
	}
	for _, sample := range d.Samples {
		if err := enc.Encode(sample); err != nil {
			return err
		}
	}
	return nil
}

func (d *InternetLatencySamples) Deserialize(data []byte) error {
	dec := bin.NewBorshDecoder(data)
	if err := dec.Decode(&d.InternetLatencySamplesHeader); err != nil {
		return err
	}

	if d.InternetLatencySamplesHeader.NextSampleIndex > MaxInternetLatencySamplesPerAccount {
		return fmt.Errorf("next sample index %d exceeds max allowed samples %d", d.InternetLatencySamplesHeader.NextSampleIndex, MaxInternetLatencySamplesPerAccount)
	}

	d.Samples = make([]uint32, d.InternetLatencySamplesHeader.NextSampleIndex)
	for i := 0; i < int(d.InternetLatencySamplesHeader.NextSampleIndex); i++ {
		if err := dec.Decode(&d.Samples[i]); err != nil {
			return err
		}
	}
	return nil
}

type TimestampIndexEntry struct {
	SampleIndex           uint32 // 4
	TimestampMicroseconds uint64 // 8
}

type TimestampIndexHeader struct {
	AccountType      AccountType      // 1
	SamplesAccountPK solana.PublicKey // 32
	NextEntryIndex   uint32           // 4
	Unused           [64]uint8        // 64
}

type TimestampIndex struct {
	TimestampIndexHeader
	Entries []TimestampIndexEntry
}

func (d *TimestampIndex) Deserialize(data []byte) error {
	dec := bin.NewBorshDecoder(data)
	if err := dec.Decode(&d.TimestampIndexHeader); err != nil {
		return fmt.Errorf("timestamp index header: %w", err)
	}

	entryBytes := int(d.NextEntryIndex) * (4 + 8)
	if dec.Remaining() < entryBytes {
		return fmt.Errorf("data too short for %d timestamp index entries: %d < %d", d.NextEntryIndex, dec.Remaining(), entryBytes)
	}

	d.Entries = make([]TimestampIndexEntry, d.NextEntryIndex)
	for i := range d.Entries {
		if err := dec.Decode(&d.Entries[i]); err != nil {
			return fmt.Errorf("entry %d: %w", i, err)
		}
	}
	return nil
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
	if entry.SampleIndex > sampleIndex {
		return startTimestampMicroseconds + uint64(sampleIndex)*samplingIntervalMicroseconds
	}
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

		if len(entries) == 0 || entries[entryIdx].SampleIndex > i {
			timestamps[i] = startTimestampMicroseconds + uint64(i)*samplingIntervalMicroseconds
		} else {
			e := entries[entryIdx]
			timestamps[i] = e.TimestampMicroseconds + uint64(i-e.SampleIndex)*samplingIntervalMicroseconds
		}
	}
	return timestamps
}
