package geolocation

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
)

// bpfLoaderUpgradeableProgramID is the well-known program ID for the BPF Loader Upgradeable program.
var bpfLoaderUpgradeableProgramID = solana.MustPublicKeyFromBase58("BPFLoaderUpgradeab1e11111111111111111111111")

// DeriveProgramConfigPDA derives the PDA for the GeolocationProgramConfig account.
// Seeds: ["doublezero", "programconfig"]
func DeriveProgramConfigPDA(programID solana.PublicKey) (solana.PublicKey, uint8, error) {
	seeds := [][]byte{
		[]byte(SeedPrefix),
		[]byte(ProgramConfigSeed),
	}
	return solana.FindProgramAddress(seeds, programID)
}

// DeriveGeoProbePDA derives the PDA for a GeoProbe account.
// Seeds: ["doublezero", "probe", code.as_bytes()]
func DeriveGeoProbePDA(programID solana.PublicKey, code string) (solana.PublicKey, uint8, error) {
	if code == "" {
		return solana.PublicKey{}, 0, fmt.Errorf("code is required")
	}
	if len(code) > MaxCodeLength {
		return solana.PublicKey{}, 0, fmt.Errorf("code length %d exceeds max %d", len(code), MaxCodeLength)
	}
	seeds := [][]byte{
		[]byte(SeedPrefix),
		[]byte(GeoProbeAccountSeed),
		[]byte(code),
	}
	return solana.FindProgramAddress(seeds, programID)
}

// DeriveProgramDataPDA derives the program data PDA for a BPF Upgradeable program.
// Seeds: [programID] with BPF Loader Upgradeable as the program.
func DeriveProgramDataPDA(programID solana.PublicKey) (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress(
		[][]byte{programID[:]},
		bpfLoaderUpgradeableProgramID,
	)
}
