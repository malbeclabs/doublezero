package telemetry

import (
	"encoding/binary"
	"fmt"
)

type AccountType uint8

const (
	AccountTypeDeviceLatencySamplesV0   AccountType = 1
	AccountTypeInternetLatencySamplesV0 AccountType = 2
	AccountTypeDeviceLatencySamples     AccountType = 3
	AccountTypeInternetLatencySamples   AccountType = 4
)

const (
	TelemetrySeedPrefix        = "telemetry"
	DeviceLatencySamplesSeed   = "dzlatency"
	InternetLatencySamplesSeed = "inetlatency"

	MaxDeviceLatencySamplesPerAccount   = 35_000
	MaxInternetLatencySamplesPerAccount = 3_000

	deviceLatencyHeaderSize = 1 + 8 + 32*6 + 8 + 8 + 4 + 128
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

	d := &DeviceLatencySamples{}
	off := 0

	d.AccountType = AccountType(data[off])
	off++

	d.Epoch = binary.LittleEndian.Uint64(data[off:])
	off += 8

	copy(d.OriginDeviceAgentPK[:], data[off:off+32])
	off += 32
	copy(d.OriginDevicePK[:], data[off:off+32])
	off += 32
	copy(d.TargetDevicePK[:], data[off:off+32])
	off += 32
	copy(d.OriginDeviceLocationPK[:], data[off:off+32])
	off += 32
	copy(d.TargetDeviceLocationPK[:], data[off:off+32])
	off += 32
	copy(d.LinkPK[:], data[off:off+32])
	off += 32

	d.SamplingIntervalMicroseconds = binary.LittleEndian.Uint64(data[off:])
	off += 8
	d.StartTimestampMicroseconds = binary.LittleEndian.Uint64(data[off:])
	off += 8
	d.NextSampleIndex = binary.LittleEndian.Uint32(data[off:])
	off += 4

	off += 128 // _unused

	count := int(d.NextSampleIndex)
	if count > MaxDeviceLatencySamplesPerAccount {
		return nil, fmt.Errorf("next_sample_index %d exceeds max %d", count, MaxDeviceLatencySamplesPerAccount)
	}

	d.Samples = make([]uint32, count)
	for i := 0; i < count; i++ {
		if off+4 > len(data) {
			break
		}
		d.Samples[i] = binary.LittleEndian.Uint32(data[off:])
		off += 4
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

	d := &InternetLatencySamples{}
	off := 0

	d.AccountType = AccountType(data[off])
	off++

	d.Epoch = binary.LittleEndian.Uint64(data[off:])
	off += 8

	// Borsh string: 4-byte LE length + UTF-8
	nameLen := int(binary.LittleEndian.Uint32(data[off:]))
	off += 4
	if off+nameLen > len(data) {
		return nil, fmt.Errorf("data_provider_name length %d exceeds data", nameLen)
	}
	d.DataProviderName = string(data[off : off+nameLen])
	off += nameLen

	copy(d.OracleAgentPK[:], data[off:off+32])
	off += 32
	copy(d.OriginExchangePK[:], data[off:off+32])
	off += 32
	copy(d.TargetExchangePK[:], data[off:off+32])
	off += 32

	d.SamplingIntervalMicroseconds = binary.LittleEndian.Uint64(data[off:])
	off += 8
	d.StartTimestampMicroseconds = binary.LittleEndian.Uint64(data[off:])
	off += 8
	d.NextSampleIndex = binary.LittleEndian.Uint32(data[off:])
	off += 4

	off += 128 // _unused

	count := int(d.NextSampleIndex)
	if count > MaxInternetLatencySamplesPerAccount {
		return nil, fmt.Errorf("next_sample_index %d exceeds max %d", count, MaxInternetLatencySamplesPerAccount)
	}

	d.Samples = make([]uint32, count)
	for i := 0; i < count; i++ {
		if off+4 > len(data) {
			break
		}
		d.Samples[i] = binary.LittleEndian.Uint32(data[off:])
		off += 4
	}

	return d, nil
}
