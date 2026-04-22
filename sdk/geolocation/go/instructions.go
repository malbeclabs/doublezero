package geolocation

// Instruction discriminator indices for the GeolocationInstruction Borsh enum.
// Only user-facing operations are included; Foundation commands are excluded.
const (
	CreateGeolocationUserInstructionIndex = 7
	UpdateGeolocationUserInstructionIndex = 8
	DeleteGeolocationUserInstructionIndex = 9
	AddTargetInstructionIndex             = 10
	RemoveTargetInstructionIndex          = 11
	SetResultDestinationInstructionIndex  = 13
)
