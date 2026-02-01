package serviceability

import (
	"github.com/gagliardetto/solana-go"
)

var (
	seedGlobalState   = []byte("global_state")
	seedGlobalConfig  = []byte("global_config")
	seedProgramConfig = []byte("program_config")
)

func DeriveGlobalStatePDA(programID solana.PublicKey) (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress([][]byte{seedGlobalState}, programID)
}

func DeriveGlobalConfigPDA(programID solana.PublicKey) (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress([][]byte{seedGlobalConfig}, programID)
}

func DeriveProgramConfigPDA(programID solana.PublicKey) (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress([][]byte{seedProgramConfig}, programID)
}
