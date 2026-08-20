package shreds

import "github.com/gagliardetto/solana-go"

// ProgramID is the shred subscription program ID.
var ProgramID = solana.MustPublicKeyFromBase58("dzshrr3yL57SB13sJPYHYo3TV8Bo1i1FxkyrZr3bKNE")

// FeedProgramID is the feed subscription program ID. That is a separate program
// from the shred subscription one above, and it is deployed on Solana
// mainnet-beta only: on a cluster without it, getProgramAccounts returns an
// empty list rather than an error.
var FeedProgramID = solana.MustPublicKeyFromBase58("J9gupbyffs4XAoKn5NrJ4hrbdqW5ZfvMDaaas3FtH8yC")

// SolanaRPCURLs are the Solana RPC URLs per environment.
var SolanaRPCURLs = map[string]string{
	"mainnet-beta": "https://api.mainnet-beta.solana.com",
	"testnet":      "https://api.testnet.solana.com",
	"devnet":       "https://api.devnet.solana.com",
	"localnet":     "http://localhost:8899",
}
