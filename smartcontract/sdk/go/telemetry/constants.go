package telemetry

// Represents the type of telemetry instruction
type TelemetryInstructionType uint8

const (
	// Represents the initialize device latency samples instruction
	InitializeDeviceLatencySamplesInstructionIndex TelemetryInstructionType = 0
	// Represents the write device latency samples instruction
	WriteDeviceLatencySamplesInstructionIndex TelemetryInstructionType = 1

	// InstructionErrorAccountDoesNotExist is the error code that the telemetry program returns
	// when the given PDA does not exist.
	InstructionErrorAccountDoesNotExist = 1011

	// SolanaMaxPermittedDataIncrease is the maximum number of bytes a program may add to an
	// account during a single realloc.
	// This is the samples batch size limit in bytes.
	SolanaMaxPermittedDataIncrease = 10_240

	// MaxSamplesPerBatch is the maximum number of samples that can be written in a single batch.
	MaxSamplesPerBatch = SolanaMaxPermittedDataIncrease / 4

	// MaxSamples is the maximum number of samples that can be written to a single account.
	MaxSamples = 35_000
)

// Telemetry Program IDs
const (
	// TELEMETRY_PROGRAM_ID_TESTNET is the telemetry program ID for testnet
	TELEMETRY_PROGRAM_ID_TESTNET = "3KogTMmVxc5eUHtjZnwm136H5P8tvPwVu4ufbGPvM7p1"
	// TELEMETRY_PROGRAM_ID_DEVNET is the telemetry program ID for devnet
	TELEMETRY_PROGRAM_ID_DEVNET = "C9xqH76NSm11pBS6maNnY163tWHT8Govww47uyEmSnoG"
)

// Instruction discriminators for telemetry program
const (
	// Instruction index for initializing device latency samples
	INITIALIZE_DEVICE_LATENCY_SAMPLES_INSTRUCTION_INDEX = 0
	// Instruction index for writing device latency samples
	WRITE_DEVICE_LATENCY_SAMPLES_INSTRUCTION_INDEX = 1
)

// PDA seeds for telemetry program
const (
	// Pefix for all telemetry PDAs
	SEED_PREFIX = "telemetry"
	// Seed for device latency samples PDAs
	SEED_DEVICE_LATENCY_SAMPLES = "dzlatency"
)

// Constants for telemetry data
const (
	// Maximum number of samples that can be written in one transaction
	MAX_SAMPLES = 1000
	// Maximum size of a device latency samples account
	DEVICE_LATENCY_SAMPLES_MAX_SIZE = 10240
)
