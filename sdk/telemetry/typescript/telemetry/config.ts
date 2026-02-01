/** Network configuration for the telemetry program. */

export const PROGRAM_IDS: Record<string, string> = {
  "mainnet-beta": "tE1exJ5VMyoC9ByZeSmgtNzJCFF74G9JAv338sJiqkC",
  testnet: "3KogTMmVxc5eUHtjZnwm136H5P8tvPwVu4ufbGPvM7p1",
  devnet: "C9xqH76NSm11pBS6maNnY163tWHT8Govww47uyEmSnoG",
  localnet: "C9xqH76NSm11pBS6maNnY163tWHT8Govww47uyEmSnoG",
};

export const SOLANA_RPC_URLS: Record<string, string> = {
  "mainnet-beta": "https://api.mainnet-beta.solana.com",
  testnet: "https://api.testnet.solana.com",
  devnet: "https://api.devnet.solana.com",
  localnet: "http://localhost:8899",
};
