package geolocation

// GeolocationInstructionType represents the type of geolocation instruction
type GeolocationInstructionType uint8

const (
	InitProgramConfigInstructionIndex   GeolocationInstructionType = 0
	UpdateProgramConfigInstructionIndex GeolocationInstructionType = 1
	CreateGeoProbeInstructionIndex      GeolocationInstructionType = 2
	UpdateGeoProbeInstructionIndex      GeolocationInstructionType = 3
	DeleteGeoProbeInstructionIndex      GeolocationInstructionType = 4
	AddParentDeviceInstructionIndex     GeolocationInstructionType = 5
	RemoveParentDeviceInstructionIndex  GeolocationInstructionType = 6
)

// PDA seeds for geolocation program
const (
	SeedPrefix          = "doublezero"
	ProgramConfigSeed   = "programconfig"
	GeoProbeAccountSeed = "probe"
)

// Limits
const (
	MaxParentDevices = 5
	MaxCodeLength    = 32
)
