package serviceability

import (
	"encoding/binary"

	"github.com/gagliardetto/solana-go"
)

// PDA seeds matching Rust implementation in seeds.rs
const (
	SeedPrefix                  = "doublezero"
	SeedGlobalState             = "globalstate"
	SeedGlobalConfig            = "config"
	SeedProgramConfig           = "programconfig"
	SeedLinkIds                 = "linkids"
	SeedSegmentRoutingIds       = "segmentroutingids"
	SeedUserTunnelBlock         = "usertunnelblock"
	SeedDeviceTunnelBlock       = "devicetunnelblock"
	SeedMulticastGroupBlock     = "multicastgroupblock"
	SeedMulticastPublisherBlock = "multicastpublisherblock"
	SeedTenant                  = "tenant"
	SeedPermission              = "permission"
	SeedUser                    = "user"
	SeedAccessPass              = "accesspass"
	SeedTunnelIds               = "tunnelids"
	SeedDzPrefixBlock           = "dzprefixblock"
)

// DeriveGlobalStatePDA derives the PDA for the GlobalState account.
func DeriveGlobalStatePDA(programID solana.PublicKey) (solana.PublicKey, uint8, error) {
	seeds := [][]byte{
		[]byte(SeedPrefix),
		[]byte(SeedGlobalState),
	}
	return solana.FindProgramAddress(seeds, programID)
}

// DeriveGlobalConfigPDA derives the PDA for the GlobalConfig account.
func DeriveGlobalConfigPDA(programID solana.PublicKey) (solana.PublicKey, uint8, error) {
	seeds := [][]byte{
		[]byte(SeedPrefix),
		[]byte(SeedGlobalConfig),
	}
	return solana.FindProgramAddress(seeds, programID)
}

// DeriveProgramConfigPDA derives the PDA for the ProgramConfig account.
func DeriveProgramConfigPDA(programID solana.PublicKey) (solana.PublicKey, uint8, error) {
	seeds := [][]byte{
		[]byte(SeedPrefix),
		[]byte(SeedProgramConfig),
	}
	return solana.FindProgramAddress(seeds, programID)
}

// GetGlobalStatePDA is a convenience alias for DeriveGlobalStatePDA.
func GetGlobalStatePDA(programID solana.PublicKey) (solana.PublicKey, uint8, error) {
	return DeriveGlobalStatePDA(programID)
}

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

// GetPermissionPDA derives the PDA for a Permission account given the user_payer pubkey.
func GetPermissionPDA(programID solana.PublicKey, userPayer solana.PublicKey) (solana.PublicKey, uint8, error) {
	seeds := [][]byte{
		[]byte(SeedPrefix),
		[]byte(SeedPermission),
		userPayer[:],
	}
	return solana.FindProgramAddress(seeds, programID)
}

// GetUserPDA derives the PDA for a User account, keyed by (client_ip, user_type).
// Mirrors smartcontract/programs/doublezero-serviceability/src/pda.rs:get_user_pda.
func GetUserPDA(programID solana.PublicKey, clientIP [4]byte, userType UserUserType) (solana.PublicKey, uint8, error) {
	seeds := [][]byte{
		[]byte(SeedPrefix),
		[]byte(SeedUser),
		clientIP[:],
		{byte(userType)},
	}
	return solana.FindProgramAddress(seeds, programID)
}

// GetAccessPassPDA derives the PDA for an AccessPass account, keyed by (client_ip, user_payer).
// Mirrors smartcontract/programs/doublezero-serviceability/src/pda.rs:get_accesspass_pda.
func GetAccessPassPDA(programID solana.PublicKey, clientIP [4]byte, userPayer solana.PublicKey) (solana.PublicKey, uint8, error) {
	seeds := [][]byte{
		[]byte(SeedPrefix),
		[]byte(SeedAccessPass),
		clientIP[:],
		userPayer[:],
	}
	return solana.FindProgramAddress(seeds, programID)
}

// GetTunnelIdsPDA derives the PDA for a per-device TunnelIds resource extension at the given index.
// Rust uses usize (8 bytes on 64-bit) little-endian for the index; we always encode 8 bytes.
func GetTunnelIdsPDA(programID solana.PublicKey, devicePK solana.PublicKey, index uint64) (solana.PublicKey, uint8, error) {
	var idxBuf [8]byte
	binary.LittleEndian.PutUint64(idxBuf[:], index)
	seeds := [][]byte{
		[]byte(SeedPrefix),
		[]byte(SeedTunnelIds),
		devicePK[:],
		idxBuf[:],
	}
	return solana.FindProgramAddress(seeds, programID)
}

// GetDzPrefixBlockPDA derives the PDA for a per-device DzPrefixBlock resource extension at the given index.
// Rust uses usize (8 bytes on 64-bit) little-endian for the index; we always encode 8 bytes.
func GetDzPrefixBlockPDA(programID solana.PublicKey, devicePK solana.PublicKey, index uint64) (solana.PublicKey, uint8, error) {
	var idxBuf [8]byte
	binary.LittleEndian.PutUint64(idxBuf[:], index)
	seeds := [][]byte{
		[]byte(SeedPrefix),
		[]byte(SeedDzPrefixBlock),
		devicePK[:],
		idxBuf[:],
	}
	return solana.FindProgramAddress(seeds, programID)
}
