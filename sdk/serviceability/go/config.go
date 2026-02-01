package serviceability

// Program IDs per environment.
var ProgramIDs = map[string]string{
	"mainnet-beta": "ser2VaTMAcYTaauMrTSfSrxBaUDq7BLNs2xfUugTAGv",
	"testnet":      "DZtnuQ839pSaDMFG5q1ad2V95G82S5EC4RrB3Ndw2Heb",
	"devnet":       "GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah",
	"localnet":     "7CTniUa88iJKUHTrCkB4TjAoG6TD7AMivhQeuqN2LPtX",
}

// Solana RPC URLs per environment.
var SolanaRPCURLs = map[string]string{
	"mainnet-beta": "https://api.mainnet-beta.solana.com",
	"testnet":      "https://api.testnet.solana.com",
	"devnet":       "https://api.devnet.solana.com",
	"localnet":     "http://localhost:8899",
}
