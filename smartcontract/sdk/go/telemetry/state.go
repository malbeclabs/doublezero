package telemetry

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/near/borsh-go"
)

type AccountType uint8

const (
	AccountTypeDeviceLatencySamples AccountType = iota + 1
)

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

	// Reserved for future use.
	Unused [128]uint8 // 128
}

type DeviceLatencySamples struct {
	// Header of the account.
	DeviceLatencySamplesHeader

	// RTT samples in microseconds, one per entry.
	Samples []uint32
}

func DeserializeDeviceLatencySamples(data []byte) (*DeviceLatencySamples, error) {
	if len(data) < DEVICE_LATENCY_SAMPLES_HEADER_SIZE {
		return nil, fmt.Errorf("account data too short for header: %d < %d", len(data), DEVICE_LATENCY_SAMPLES_HEADER_SIZE)
	}

	var hdr DeviceLatencySamplesHeader
	if err := borsh.Deserialize(&hdr, data[:DEVICE_LATENCY_SAMPLES_HEADER_SIZE]); err != nil {
		return nil, fmt.Errorf("failed to deserialize header: %w", err)
	}

	expectedSamples := int(hdr.NextSampleIndex)
	sampleBytes := data[DEVICE_LATENCY_SAMPLES_HEADER_SIZE:]
	if len(sampleBytes) < expectedSamples*4 {
		return nil, fmt.Errorf("account data too short for sample count: %d < %d", len(sampleBytes), expectedSamples*4)
	}

	samples := make([]uint32, expectedSamples)
	for i := 0; i < expectedSamples; i++ {
		samples[i] = binary.LittleEndian.Uint32(sampleBytes[i*4 : (i+1)*4])
	}

	return &DeviceLatencySamples{DeviceLatencySamplesHeader: hdr, Samples: samples}, nil
}

func (d *DeviceLatencySamples) Serialize() ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, DEVICE_LATENCY_SAMPLES_HEADER_SIZE+len(d.Samples)*4))
	headerBuf, err := borsh.Serialize(d.DeviceLatencySamplesHeader)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize header: %w", err)
	}
	if _, err := buf.Write(headerBuf); err != nil {
		return nil, fmt.Errorf("failed to write header: %w", err)
	}
	for _, sample := range d.Samples {
		if err := binary.Write(buf, binary.LittleEndian, sample); err != nil {
			return nil, fmt.Errorf("failed to write sample: %w", err)
		}
	}
	return buf.Bytes(), nil
}
