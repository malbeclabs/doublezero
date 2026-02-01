package telemetry

import "github.com/gagliardetto/solana-go"

var ProgramIDs = map[string]solana.PublicKey{
	"mainnet-beta": solana.MustPublicKeyFromBase58("tE1exJ5VMyoC9ByZeSmgtNzJCFF74G9JAv338sJiqkC"),
	"testnet":      solana.MustPublicKeyFromBase58("3KogTMmVxc5eUHtjZnwm136H5P8tvPwVu4ufbGPvM7p1"),
	"devnet":       solana.MustPublicKeyFromBase58("C9xqH76NSm11pBS6maNnY163tWHT8Govww47uyEmSnoG"),
	"localnet":     solana.MustPublicKeyFromBase58("C9xqH76NSm11pBS6maNnY163tWHT8Govww47uyEmSnoG"),
}

var SolanaRPCURLs = map[string]string{
	"mainnet-beta": "https://api.mainnet-beta.solana.com",
	"testnet":      "https://api.testnet.solana.com",
	"devnet":       "https://api.devnet.solana.com",
	"localnet":     "http://localhost:8899",
}
