use solana_sdk::pubkey::Pubkey;

// Constants related to DoubleZero mainnet-beta configuration
pub const DOUBLEZERO_LEDGER_RPC_URL: &str =
    "https://doublezero-mainnet-beta.rpcpool.com/db336024-e7a8-46b1-80e5-352dd77060ab";
pub const DOUBLEZERO_LEDGER_WS_RPC_URL: &str =
    "wss://doublezero-mainnet-beta.rpcpool.com/db336024-e7a8-46b1-80e5-352dd77060ab";
pub const SERVICEABILITY_MAINNET_BETA_PUBKEY: Pubkey =
    Pubkey::from_str_const("ser2VaTMAcYTaauMrTSfSrxBaUDq7BLNs2xfUugTAGv");
pub const TELEMETRY_MAINNET_BETA_PUBKEY: Pubkey =
    Pubkey::from_str_const("tE1exJ5VMyoC9ByZeSmgtNzJCFF74G9JAv338sJiqkC");
pub const INTERNET_LATENCY_COLLECTOR_MAINNET_BETA_PUBKEY: Pubkey =
    Pubkey::from_str_const("8xHn4r7oQuqNZ5cLYwL5YZcDy1JjDQcpVkyoA8Dw5uXH");

// Constants related to DoubleZero testnet configuration
pub const DOUBLEZERO_TESTNET_LEDGER_RPC_URL: &str =
    "https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16";
pub const DOUBLEZERO_TESTNET_LEDGER_WS_RPC_URL: &str =
    "wss://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16/whirligig";
pub const SERVICEABILITY_TESTNET_PUBKEY: Pubkey =
    Pubkey::from_str_const("DZtnuQ839pSaDMFG5q1ad2V95G82S5EC4RrB3Ndw2Heb");
pub const TELEMETRY_TESTNET_PUBKEY: Pubkey =
    Pubkey::from_str_const("3KogTMmVxc5eUHtjZnwm136H5P8tvPwVu4ufbGPvM7p1");
pub const INTERNET_LATENCY_COLLECTOR_TESTNET_PUBKEY: Pubkey =
    Pubkey::from_str_const("HWGQSTmXWMB85NY2vFLhM1nGpXA8f4VCARRyeGNbqDF1");

// Constants related to DoubleZero devnet configuration
pub const DOUBLEZERO_DEVNET_LEDGER_RPC_URL: &str =
    "https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16";
pub const DOUBLEZERO_DEVNET_LEDGER_WS_RPC_URL: &str =
    "wss://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16/whirligig";
pub const SERVICEABILITY_DEVNET_PUBKEY: Pubkey =
    Pubkey::from_str_const("GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah");
pub const TELEMETRY_DEVNET_PUBKEY: Pubkey =
    Pubkey::from_str_const("C9xqH76NSm11pBS6maNnY163tWHT8Govww47uyEmSnoG");
pub const INTERNET_LATENCY_COLLECTOR_DEVNET_PUBKEY: Pubkey =
    Pubkey::from_str_const("3fXen9LP5JUAkaaDJtyLo1ohPiJ2LdzVqAnmhtGgAmwJ");

// Constants related to DoubleZero localnet configuration
pub const DOUBLEZERO_LOCALNET_LEDGER_RPC_URL: &str = "http://localhost:8899";
pub const DOUBLEZERO_LOCALNET_LEDGER_WS_RPC_URL: &str = "ws://localhost:8899";
pub const SERVICEABILITY_LOCALNET_PUBKEY: Pubkey =
    Pubkey::from_str_const("7CTniUa88iJKUHTrCkB4TjAoG6TD7AMivhQeuqN2LPtX");
pub const TELEMETRY_LOCALNET_PUBKEY: Pubkey =
    Pubkey::from_str_const("C9xqH76NSm11pBS6maNnY163tWHT8Govww47uyEmSnoG");
pub const INTERNET_LATENCY_COLLECTOR_LOCALNET_PUBKEY: Pubkey =
    Pubkey::from_str_const("3fXen9LP5JUAkaaDJtyLo1ohPiJ2LdzVqAnmhtGgAmwJ");
