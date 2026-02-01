/** Network configuration for the telemetry program. */

export const PROGRAM_IDS: Record<string, string> = {
  "mainnet-beta": "tE1exJ5VMyoC9ByZeSmgtNzJCFF74G9JAv338sJiqkC",
  testnet: "3KogTMmVxc5eUHtjZnwm136H5P8tvPwVu4ufbGPvM7p1",
  devnet: "C9xqH76NSm11pBS6maNnY163tWHT8Govww47uyEmSnoG",
  localnet: "C9xqH76NSm11pBS6maNnY163tWHT8Govww47uyEmSnoG",
};

export const LEDGER_RPC_URLS: Record<string, string> = {
  "mainnet-beta":
    "https://doublezero-mainnet-beta.rpcpool.com/db336024-e7a8-46b1-80e5-352dd77060ab",
  testnet:
    "https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16",
  devnet:
    "https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16",
  localnet: "http://localhost:8899",
};
