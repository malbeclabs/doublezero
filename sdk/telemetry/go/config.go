package telemetry

import "github.com/gagliardetto/solana-go"

var ProgramIDs = map[string]solana.PublicKey{
	"mainnet-beta": solana.MustPublicKeyFromBase58("tE1exJ5VMyoC9ByZeSmgtNzJCFF74G9JAv338sJiqkC"),
	"testnet":      solana.MustPublicKeyFromBase58("3KogTMmVxc5eUHtjZnwm136H5P8tvPwVu4ufbGPvM7p1"),
	"devnet":       solana.MustPublicKeyFromBase58("C9xqH76NSm11pBS6maNnY163tWHT8Govww47uyEmSnoG"),
	"localnet":     solana.MustPublicKeyFromBase58("C9xqH76NSm11pBS6maNnY163tWHT8Govww47uyEmSnoG"),
}

// LedgerRPCURLs are the DZ Ledger RPC URLs per environment.
var LedgerRPCURLs = map[string]string{
	"mainnet-beta": "https://doublezero-mainnet-beta.rpcpool.com/db336024-e7a8-46b1-80e5-352dd77060ab",
	"testnet":      "https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16",
	"devnet":       "https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16",
	"localnet":     "http://localhost:8899",
}
