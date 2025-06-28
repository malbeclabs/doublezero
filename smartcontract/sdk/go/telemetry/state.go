package telemetry

import "github.com/gagliardetto/solana-go"

type AccountType uint8

const (
	AccountTypeDeviceLatencySamples AccountType = iota + 1
)

type DeviceLatencySamples struct {
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

	// RTT samples in microseconds, one per entry (with length prefix).
	Samples []uint32 // 4 + n*4 (RTT values in microseconds)
}
