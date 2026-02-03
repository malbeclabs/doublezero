package revdist

import "github.com/gagliardetto/solana-go"

// ProgramID is the revenue distribution program ID (same across all environments).
var ProgramID = solana.MustPublicKeyFromBase58("dzrevZC94tBLwuHw1dyynZxaXTWyp7yocsinyEVPtt4")

// SolanaRPCURLs are the Solana RPC URLs per environment.
var SolanaRPCURLs = map[string]string{
	"mainnet-beta": "https://api.mainnet-beta.solana.com",
	"testnet":      "https://api.testnet.solana.com",
	"devnet":       "https://api.devnet.solana.com",
	"localnet":     "http://localhost:8899",
}

// LedgerRPCURLs are the DZ Ledger RPC URLs per environment.
var LedgerRPCURLs = map[string]string{
	"mainnet-beta": "https://doublezero-mainnet-beta.rpcpool.com/db336024-e7a8-46b1-80e5-352dd77060ab",
	"testnet":      "https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16",
	"devnet":       "https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16",
	"localnet":     "http://localhost:8899",
}
