package serviceability

import (
	"github.com/gagliardetto/solana-go"
)

var (
	seedPrefix                  = []byte("doublezero")
	seedGlobalState             = []byte("globalstate")
	seedGlobalConfig            = []byte("config")
	seedProgramConfig           = []byte("programconfig")
	seedLinkIds                 = []byte("linkids")
	seedSegmentRoutingIds       = []byte("segmentroutingids")
	seedUserTunnelBlock         = []byte("usertunnelblock")
	seedDeviceTunnelBlock       = []byte("devicetunnelblock")
	seedMulticastGroupBlock     = []byte("multicastgroupblock")
	seedMulticastPublisherBlock = []byte("multicastpublisherblock")
	seedPermission              = []byte("permission")
)

func DeriveGlobalStatePDA(programID solana.PublicKey) (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress([][]byte{seedPrefix, seedGlobalState}, programID)
}

func DeriveGlobalConfigPDA(programID solana.PublicKey) (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress([][]byte{seedPrefix, seedGlobalConfig}, programID)
}

func DeriveProgramConfigPDA(programID solana.PublicKey) (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress([][]byte{seedPrefix, seedProgramConfig}, programID)
}

// GetLinkIdsPDA derives the PDA for the global LinkIds resource extension
func GetLinkIdsPDA(programID solana.PublicKey) (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress([][]byte{seedPrefix, seedLinkIds}, programID)
}

// GetSegmentRoutingIdsPDA derives the PDA for the global SegmentRoutingIds resource extension
func GetSegmentRoutingIdsPDA(programID solana.PublicKey) (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress([][]byte{seedPrefix, seedSegmentRoutingIds}, programID)
}

// GetUserTunnelBlockPDA derives the PDA for the global UserTunnelBlock resource extension
func GetUserTunnelBlockPDA(programID solana.PublicKey) (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress([][]byte{seedPrefix, seedUserTunnelBlock}, programID)
}

// GetDeviceTunnelBlockPDA derives the PDA for the global DeviceTunnelBlock resource extension
func GetDeviceTunnelBlockPDA(programID solana.PublicKey) (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress([][]byte{seedPrefix, seedDeviceTunnelBlock}, programID)
}

// GetMulticastGroupBlockPDA derives the PDA for the global MulticastGroupBlock resource extension
func GetMulticastGroupBlockPDA(programID solana.PublicKey) (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress([][]byte{seedPrefix, seedMulticastGroupBlock}, programID)
}

// GetMulticastPublisherBlockPDA derives the PDA for the global MulticastPublisherBlock resource extension
func GetMulticastPublisherBlockPDA(programID solana.PublicKey) (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress([][]byte{seedPrefix, seedMulticastPublisherBlock}, programID)
}

// GetPermissionPDA derives the PDA for a Permission account given the user_payer pubkey.
func GetPermissionPDA(programID solana.PublicKey, userPayer solana.PublicKey) (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress([][]byte{seedPrefix, seedPermission, userPayer[:]}, programID)
}
