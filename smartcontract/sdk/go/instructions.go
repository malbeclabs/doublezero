package dzsdk

import (
	"github.com/gagliardetto/solana-go"
	"github.com/near/borsh-go"
)

// Represents the type of telemetry instruction
type TelemetryInstructionType uint8

const (
	// Represents the initialize device latency samples instruction
	InitializeDeviceLatencySamplesInstruction TelemetryInstructionType = 0
	// Represents the write device latency samples instruction
	WriteDeviceLatencySamplesInstruction TelemetryInstructionType = 1
)

// Represents the arguments for initializing device latency samples
type InitializeDeviceLatencySamplesArgs struct {
	OriginDevicePK               solana.PublicKey
	TargetDevicePK               solana.PublicKey
	LinkPK                       solana.PublicKey
	Epoch                        uint64
	SamplingIntervalMicroseconds uint64
}

// Represents the arguments for writing device latency samples
type WriteDeviceLatencySamplesArgs struct {
	StartTimestampMicroseconds uint64
	Samples                    []uint32
}

// Serializes the initialize instruction
func SerializeInitializeDeviceLatencySamples(args *InitializeDeviceLatencySamplesArgs) ([]byte, error) {
	// Create a struct that matches the Rust side exactly
	type instructionData struct {
		Discriminator                uint8
		OriginDevicePK               [32]byte
		TargetDevicePK               [32]byte
		LinkPK                       [32]byte
		Epoch                        uint64
		SamplingIntervalMicroseconds uint64
	}

	data := instructionData{
		Discriminator:                uint8(InitializeDeviceLatencySamplesInstruction),
		OriginDevicePK:               args.OriginDevicePK,
		TargetDevicePK:               args.TargetDevicePK,
		LinkPK:                       args.LinkPK,
		Epoch:                        args.Epoch,
		SamplingIntervalMicroseconds: args.SamplingIntervalMicroseconds,
	}

	return borsh.Serialize(data)
}

// Serializes the write instruction
func SerializeWriteDeviceLatencySamples(args *WriteDeviceLatencySamplesArgs) ([]byte, error) {
	// Create a struct that matches the expected format
	type instructionData struct {
		Discriminator              uint8
		StartTimestampMicroseconds uint64
		Samples                    []uint32
	}

	data := instructionData{
		Discriminator:              uint8(WriteDeviceLatencySamplesInstruction),
		StartTimestampMicroseconds: args.StartTimestampMicroseconds,
		Samples:                    args.Samples,
	}

	return borsh.Serialize(data)
}
