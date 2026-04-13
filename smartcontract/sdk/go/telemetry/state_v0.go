package telemetry

import (
	"fmt"
	"io"

	bin "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
)

type DeviceLatencySamplesHeaderV0 struct {
	// Used to distinguish this account type during deserialization
	AccountType AccountType // 1

	// Required for recreating the PDA (seed authority)
	BumpSeed uint8 // 1

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

type DeviceLatencySamplesV0 struct {
	DeviceLatencySamplesHeaderV0
	Samples []uint32 // 4 + n*4 (RTT values in microseconds)
}

func (d *DeviceLatencySamplesV0) Serialize(w io.Writer) error {
	enc := bin.NewBorshEncoder(w)
	if err := enc.Encode(d.DeviceLatencySamplesHeaderV0); err != nil {
		return err
	}
	for _, sample := range d.Samples {
		if err := enc.Encode(sample); err != nil {
			return err
		}
	}
	return nil
}

func (d *DeviceLatencySamplesV0) Deserialize(data []byte) error {
	dec := bin.NewBorshDecoder(data)
	if err := dec.Decode(&d.DeviceLatencySamplesHeaderV0); err != nil {
		return err
	}

	if d.DeviceLatencySamplesHeaderV0.NextSampleIndex > MaxDeviceLatencySamplesPerAccount {
		return fmt.Errorf("next sample index %d exceeds max allowed samples %d", d.DeviceLatencySamplesHeaderV0.NextSampleIndex, MaxDeviceLatencySamplesPerAccount)
	}

	d.Samples = make([]uint32, d.DeviceLatencySamplesHeaderV0.NextSampleIndex)
	for i := 0; i < int(d.DeviceLatencySamplesHeaderV0.NextSampleIndex); i++ {
		if err := dec.Decode(&d.Samples[i]); err != nil {
			return err
		}
	}
	return nil
}

func (d *DeviceLatencySamplesHeaderV0) ToV1Header() DeviceLatencySamplesHeader {
	var agentVersion [16]uint8
	var agentCommit [8]uint8
	var unused [104]uint8
	copy(agentVersion[:], d.Unused[0:16])
	copy(agentCommit[:], d.Unused[16:24])
	copy(unused[:], d.Unused[24:128])

	return DeviceLatencySamplesHeader{
		AccountType:                  AccountTypeDeviceLatencySamples,
		Epoch:                        d.Epoch,
		OriginDeviceAgentPK:          d.OriginDeviceAgentPK,
		OriginDevicePK:               d.OriginDevicePK,
		TargetDevicePK:               d.TargetDevicePK,
		OriginDeviceLocationPK:       d.OriginDeviceLocationPK,
		TargetDeviceLocationPK:       d.TargetDeviceLocationPK,
		LinkPK:                       d.LinkPK,
		SamplingIntervalMicroseconds: d.SamplingIntervalMicroseconds,
		StartTimestampMicroseconds:   d.StartTimestampMicroseconds,
		NextSampleIndex:              d.NextSampleIndex,
		AgentVersion:                 agentVersion,
		AgentCommit:                  agentCommit,
		Unused:                       unused,
	}
}

func (d *DeviceLatencySamplesV0) ToV1() *DeviceLatencySamples {
	return &DeviceLatencySamples{
		DeviceLatencySamplesHeader: d.DeviceLatencySamplesHeaderV0.ToV1Header(),
		Samples:                   d.Samples,
	}
}
