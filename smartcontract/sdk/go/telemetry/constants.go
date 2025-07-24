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

	// MaxSamplesPerBatch is the maximum number of samples that can be written in a single batch.
	//
	// Messages transmitted to Solana validators must not exceed the IPv6 MTU size to ensure fast
	// and reliable network transmission of cluster info over UDP. Solana's networking stack uses a
	// conservative MTU size of 1280 bytes which, after accounting for headers, leaves 1232 bytes
	// for packet data like serialized transactions.
	// https://docs.anza.xyz/proposals/versioned-transactions#problem
	MaxSamplesPerBatch = 245 // 980 bytes

	// MaxDeviceLatencySamplesPerAccount is the maximum number of samples that can be written to a single device latency samples account.
	// This provides space for just over 12 samples per minute, or 1 sample every 5 seconds.
	MaxDeviceLatencySamplesPerAccount = 35_000

	// MaxInternetLatencySamplesPerAccount is the maximum number of samples that can be written to an internet latency samples account.
	// This provides space for just over 1 sample per minute.
	MaxInternetLatencySamplesPerAccount = 3000
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
