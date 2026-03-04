package serviceability

import "github.com/gagliardetto/solana-go"

// PDA seeds matching Rust implementation in seeds.rs
const (
	SeedPrefix                  = "doublezero"
	SeedLinkIds                 = "linkids"
	SeedSegmentRoutingIds       = "segmentroutingids"
	SeedUserTunnelBlock         = "usertunnelblock"
	SeedDeviceTunnelBlock       = "devicetunnelblock"
	SeedMulticastGroupBlock     = "multicastgroupblock"
	SeedMulticastPublisherBlock = "multicastpublisherblock"
	SeedTenant                  = "tenant"
)

// GetLinkIdsPDA derives the PDA for the global LinkIds resource extension
func GetLinkIdsPDA(programID solana.PublicKey) (solana.PublicKey, uint8, error) {
	seeds := [][]byte{
		[]byte(SeedPrefix),
		[]byte(SeedLinkIds),
	}
	return solana.FindProgramAddress(seeds, programID)
}

// GetSegmentRoutingIdsPDA derives the PDA for the global SegmentRoutingIds resource extension
func GetSegmentRoutingIdsPDA(programID solana.PublicKey) (solana.PublicKey, uint8, error) {
	seeds := [][]byte{
		[]byte(SeedPrefix),
		[]byte(SeedSegmentRoutingIds),
	}
	return solana.FindProgramAddress(seeds, programID)
}

// GetUserTunnelBlockPDA derives the PDA for the global UserTunnelBlock resource extension
func GetUserTunnelBlockPDA(programID solana.PublicKey) (solana.PublicKey, uint8, error) {
	seeds := [][]byte{
		[]byte(SeedPrefix),
		[]byte(SeedUserTunnelBlock),
	}
	return solana.FindProgramAddress(seeds, programID)
}

// GetDeviceTunnelBlockPDA derives the PDA for the global DeviceTunnelBlock resource extension
func GetDeviceTunnelBlockPDA(programID solana.PublicKey) (solana.PublicKey, uint8, error) {
	seeds := [][]byte{
		[]byte(SeedPrefix),
		[]byte(SeedDeviceTunnelBlock),
	}
	return solana.FindProgramAddress(seeds, programID)
}

// GetMulticastGroupBlockPDA derives the PDA for the global MulticastGroupBlock resource extension
func GetMulticastGroupBlockPDA(programID solana.PublicKey) (solana.PublicKey, uint8, error) {
	seeds := [][]byte{
		[]byte(SeedPrefix),
		[]byte(SeedMulticastGroupBlock),
	}
	return solana.FindProgramAddress(seeds, programID)
}

// GetMulticastPublisherBlockPDA derives the PDA for the global MulticastPublisherBlock resource extension
func GetMulticastPublisherBlockPDA(programID solana.PublicKey) (solana.PublicKey, uint8, error) {
	seeds := [][]byte{
		[]byte(SeedPrefix),
		[]byte(SeedMulticastPublisherBlock),
	}
	return solana.FindProgramAddress(seeds, programID)
}

// GetTenantPDA derives the PDA for a tenant account based on its code
func GetTenantPDA(programID solana.PublicKey, code string) (solana.PublicKey, uint8, error) {
	seeds := [][]byte{
		[]byte(SeedPrefix),
		[]byte(SeedTenant),
		[]byte(code),
	}
	return solana.FindProgramAddress(seeds, programID)
}
