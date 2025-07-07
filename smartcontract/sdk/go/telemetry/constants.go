package telemetry

// Represents the type of telemetry instruction.
type TelemetryInstructionType uint8

// Instruction indices for telemetry program.
const (
	// Represents the initialize device latency samples instruction.
	InitializeDeviceLatencySamplesInstructionIndex TelemetryInstructionType = 0

	// Represents the write device latency samples instruction.
	WriteDeviceLatencySamplesInstructionIndex TelemetryInstructionType = 1
)

// Telemetry Program IDs.
const (
	// TELEMETRY_PROGRAM_ID_TESTNET is the telemetry program ID for testnet.
	// TODO: placeholder
	TELEMETRY_PROGRAM_ID_TESTNET = "TeLemTest1111111111111111111111111111111111"

	// TELEMETRY_PROGRAM_ID_DEVNET is the telemetry program ID for devnet.
	TELEMETRY_PROGRAM_ID_DEVNET = "C9xqH76NSm11pBS6maNnY163tWHT8Govww47uyEmSnoG"
)

// Constants for telemetry data.
const (
	// Maximum number of samples that can be written in one transaction.
	MAX_DEVICE_LATENCY_SAMPLES = 35_000

	// Total size of a device latency samples account header = 349 bytes.
	DEVICE_LATENCY_SAMPLES_HEADER_SIZE = 1 + 8 + 32 + 32 + 32 + 32 + 32 + 32 + 8 + 8 + 4 + 128

	// Total size of a fully preallocated account, including the header and all samples.
	DEVICE_LATENCY_SAMPLES_ALLOCATED_SIZE = DEVICE_LATENCY_SAMPLES_HEADER_SIZE + MAX_DEVICE_LATENCY_SAMPLES*4
)

// Error codes for telemetry program.
const (
	// InstructionErrorAccountAlreadyInitialized is the error code that the telemetry program returns
	// when the given account has already been initialized.
	InstructionErrorAccountAlreadyInitialized = 1010

	// InstructionErrorAccountDoesNotExist is the error code that the telemetry program returns
	// when the given account does not exist.
	InstructionErrorAccountDoesNotExist = 1011
)
