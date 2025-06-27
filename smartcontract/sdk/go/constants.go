package dzsdk

// Telemetry Program IDs
const (
	// TELEMETRY_PROGRAM_ID_TESTNET is the telemetry program ID for testnet
	// TODO: placeholder
	TELEMETRY_PROGRAM_ID_TESTNET = "TeLemTest1111111111111111111111111111111111"
	// TELEMETRY_PROGRAM_ID_DEVNET is the telemetry program ID for devnet
	// TODO: placeholder
	TELEMETRY_PROGRAM_ID_DEVNET = "TeLemDev11111111111111111111111111111111111"
)

// Instruction discriminators for telemetry program
const (
	// Instruction index for initializing DZ latency samples
	INITIALIZE_DZ_LATENCY_SAMPLES_INSTRUCTION_INDEX = 0
	// Instruction index for writing DZ latency samples
	WRITE_DZ_LATENCY_SAMPLES_INSTRUCTION_INDEX = 1
)

// PDA seeds for telemetry program
const (
	// Pefix for all telemetry PDAs
	SEED_PREFIX = "telemetry"
	// Seed for DZ latency samples PDAs
	SEED_DZ_LATENCY_SAMPLES = "dz_latency"
)

// Constants for telemetry data
const (
	// Maximum number of samples that can be written in one transaction
	MAX_SAMPLES = 1000
	// Maximum size of a DZ latency samples account
	DZ_LATENCY_SAMPLES_MAX_SIZE = 10240
)
