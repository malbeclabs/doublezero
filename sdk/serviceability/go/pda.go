package serviceability

import (
	"github.com/gagliardetto/solana-go"
)

var (
	seedPrefix        = []byte("doublezero")
	seedGlobalState   = []byte("globalstate")
	seedGlobalConfig  = []byte("config")
	seedProgramConfig = []byte("programconfig")
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
