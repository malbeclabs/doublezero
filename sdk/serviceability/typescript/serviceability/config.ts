/** Network configuration for the serviceability program. */

export const PROGRAM_IDS: Record<string, string> = {
  "mainnet-beta": "ser2VaTMAcYTaauMrTSfSrxBaUDq7BLNs2xfUugTAGv",
  testnet: "DZtnuQ839pSaDMFG5q1ad2V95G82S5EC4RrB3Ndw2Heb",
  devnet: "GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah",
  localnet: "7CTniUa88iJKUHTrCkB4TjAoG6TD7AMivhQeuqN2LPtX",
};

export const SOLANA_RPC_URLS: Record<string, string> = {
  "mainnet-beta": "https://api.mainnet-beta.solana.com",
  testnet: "https://api.testnet.solana.com",
  devnet: "https://api.devnet.solana.com",
  localnet: "http://localhost:8899",
};
