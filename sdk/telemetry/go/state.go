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
