package dzsdk

import (
	"github.com/gagliardetto/solana-go"
	"github.com/near/borsh-go"
)

// Represents the type of telemetry instruction
type TelemetryInstructionType uint8

const (
	// Represents the initialize DZ latency samples instruction
	InitializeDzLatencySamplesInstruction TelemetryInstructionType = 0
	// Represents the write DZ latency samples instruction
	WriteDzLatencySamplesInstruction TelemetryInstructionType = 1
)

// Represents the arguments for initializing DZ latency samples
type InitializeDzLatencySamplesArgs struct {
	DeviceAPk                    solana.PublicKey
	DeviceZPk                    solana.PublicKey
	LinkPk                       solana.PublicKey
	Epoch                        uint64
	SamplingIntervalMicroseconds uint64
}

// Represents the arguments for writing DZ latency samples
type WriteDzLatencySamplesArgs struct {
	StartTimestampMicroseconds uint64
	Samples                    []uint32
}

// Serializes the initialize instruction
func SerializeInitializeDzLatencySamples(args *InitializeDzLatencySamplesArgs) ([]byte, error) {
	// Create a struct that matches the Rust side exactly
	type instructionData struct {
		Discriminator                uint8
		DeviceAPk                    [32]byte
		DeviceZPk                    [32]byte
		LinkPk                       [32]byte
		Epoch                        uint64
		SamplingIntervalMicroseconds uint64
	}

	data := instructionData{
		Discriminator:                uint8(InitializeDzLatencySamplesInstruction),
		DeviceAPk:                    args.DeviceAPk,
		DeviceZPk:                    args.DeviceZPk,
		LinkPk:                       args.LinkPk,
		Epoch:                        args.Epoch,
		SamplingIntervalMicroseconds: args.SamplingIntervalMicroseconds,
	}

	return borsh.Serialize(data)
}

// Serializes the write instruction
func SerializeWriteDzLatencySamples(args *WriteDzLatencySamplesArgs) ([]byte, error) {
	// Create a struct that matches the expected format
	type instructionData struct {
		Discriminator              uint8
		StartTimestampMicroseconds uint64
		Samples                    []uint32
	}

	data := instructionData{
		Discriminator:              uint8(WriteDzLatencySamplesInstruction),
		StartTimestampMicroseconds: args.StartTimestampMicroseconds,
		Samples:                    args.Samples,
	}

	return borsh.Serialize(data)
}
