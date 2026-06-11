package geolocation

// Instruction discriminator indices for the GeolocationInstruction Borsh enum.
// Only target management operations are included; other user-facing and Foundation
// commands are added by follow-on changes.
const (
	AddTargetInstructionIndex    = 10
	RemoveTargetInstructionIndex = 11
)
