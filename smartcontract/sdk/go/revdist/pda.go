package revdist

import (
	"encoding/binary"

	"github.com/gagliardetto/solana-go"
)

var (
	seedProgramConfig          = []byte("program_config")
	seedDistribution           = []byte("distribution")
	seedSolanaValidatorDeposit = []byte("solana_validator_deposit")
	seedContributorRewards     = []byte("contributor_rewards")
	seedJournal                = []byte("journal")
)

func DeriveConfigPDA(programID solana.PublicKey) (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress([][]byte{seedProgramConfig}, programID)
}

func DeriveDistributionPDA(programID solana.PublicKey, epoch uint64) (solana.PublicKey, uint8, error) {
	epochBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(epochBytes, epoch)
	return solana.FindProgramAddress([][]byte{seedDistribution, epochBytes}, programID)
}

func DeriveJournalPDA(programID solana.PublicKey) (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress([][]byte{seedJournal}, programID)
}

func DeriveValidatorDepositPDA(programID solana.PublicKey, nodeID solana.PublicKey) (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress([][]byte{seedSolanaValidatorDeposit, nodeID.Bytes()}, programID)
}

func DeriveContributorRewardsPDA(programID solana.PublicKey, serviceKey solana.PublicKey) (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress([][]byte{seedContributorRewards, serviceKey.Bytes()}, programID)
}
