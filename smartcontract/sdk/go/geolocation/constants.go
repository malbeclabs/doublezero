package geolocation

// GeolocationInstructionType represents the type of geolocation instruction
type GeolocationInstructionType uint8

const (
	InitProgramConfigInstructionIndex   GeolocationInstructionType = 0
	CreateGeoProbeInstructionIndex      GeolocationInstructionType = 1
	UpdateGeoProbeInstructionIndex      GeolocationInstructionType = 2
	DeleteGeoProbeInstructionIndex      GeolocationInstructionType = 3
	AddParentDeviceInstructionIndex     GeolocationInstructionType = 4
	RemoveParentDeviceInstructionIndex  GeolocationInstructionType = 5
	UpdateProgramConfigInstructionIndex GeolocationInstructionType = 6
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
