package shreds

import "github.com/gagliardetto/solana-go"

// ProgramID is the shred subscription program ID.
var ProgramID = solana.MustPublicKeyFromBase58("dzshrr3yL57SB13sJPYHYo3TV8Bo1i1FxkyrZr3bKNE")

// SolanaRPCURLs are the Solana RPC URLs per environment.
var SolanaRPCURLs = map[string]string{
	"mainnet-beta": "https://api.mainnet-beta.solana.com",
	"testnet":      "https://api.testnet.solana.com",
	"devnet":       "https://api.devnet.solana.com",
	"localnet":     "http://localhost:8899",
}
