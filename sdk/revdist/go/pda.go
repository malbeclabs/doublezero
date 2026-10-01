package revdist

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/gagliardetto/solana-go"
	"github.com/mr-tron/base58"
)

var (
	seedProgramConfig          = []byte("program_config")
	seedDistribution           = []byte("distribution")
	seedSolanaValidatorDeposit = []byte("solana_validator_deposit")
	seedContributorRewards     = []byte("contributor_rewards")
	seedJournal                = []byte("journal")
	seedSolanaValidatorDebt    = []byte("solana_validator_debt")
	seedDZContributorRewards   = []byte("dz_contributor_rewards")
	seedShapleyOutput          = []byte("shapley_output")
)

// RecordProgramID is the on-chain program ID for the doublezero-record program.
var RecordProgramID = solana.MustPublicKeyFromBase58("dzrecxigtaZQ3gPmt2X5mDkYigaruFR1rHCqztFTvx7")

// recordHeaderSize is the size of the RecordData header (version u8 + authority pubkey).
const recordHeaderSize = 33

// createRecordSeedString hashes the seeds with SHA256, encodes as base58,
// and truncates to 32 characters — matching the Rust create_record_seed_string.
func createRecordSeedString(seeds [][]byte) string {
	h := sha256.New()
	for _, s := range seeds {
		h.Write(s)
	}
	encoded := base58.Encode(h.Sum(nil))
	if len(encoded) > 32 {
		encoded = encoded[:32]
	}
	return encoded
}

// DeriveRecordKey derives a ledger record address using create-with-seed,
// matching the Rust create_record_key function.
func DeriveRecordKey(payerKey solana.PublicKey, seeds [][]byte) (solana.PublicKey, error) {
	seedStr := createRecordSeedString(seeds)
	return solana.CreateWithSeed(payerKey, seedStr, RecordProgramID)
}

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
