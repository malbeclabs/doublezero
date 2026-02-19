use solana_sdk::pubkey::Pubkey;

pub const ENV_MAINNET_BETA_NAME: &str = "mainnet-beta";
pub const ENV_TESTNET_NAME: &str = "testnet";
pub const ENV_DEVNET_NAME: &str = "devnet";
pub const ENV_LOCALNET_NAME: &str = "local";

pub const ENV_MAINNET_BETA_SHORT_NAME: &str = "m";
pub const ENV_TESTNET_SHORT_NAME: &str = "t";
pub const ENV_DEVNET_SHORT_NAME: &str = "d";
pub const ENV_LOCALNET_SHORT_NAME: &str = "l";

// Constants related to DoubleZero mainnet-beta configuration
pub const ENV_MAINNET_BETA_DOUBLEZERO_LEDGER_RPC_URL: &str =
    "https://doublezero-mainnet-beta.rpcpool.com/db336024-e7a8-46b1-80e5-352dd77060ab";
pub const ENV_MAINNET_BETA_DOUBLEZERO_LEDGER_WS_RPC_URL: &str =
    "wss://doublezero-mainnet-beta.rpcpool.com/db336024-e7a8-46b1-80e5-352dd77060ab";
pub const ENV_MAINNET_BETA_SERVICEABILITY_PUBKEY: Pubkey =
    Pubkey::from_str_const("ser2VaTMAcYTaauMrTSfSrxBaUDq7BLNs2xfUugTAGv");
pub const ENV_MAINNET_BETA_TELEMETRY_PUBKEY: Pubkey =
    Pubkey::from_str_const("tE1exJ5VMyoC9ByZeSmgtNzJCFF74G9JAv338sJiqkC");
pub const ENV_MAINNET_BETA_INTERNET_LATENCY_COLLECTOR_PUBKEY: Pubkey =
    Pubkey::from_str_const("8xHn4r7oQuqNZ5cLYwL5YZcDy1JjDQcpVkyoA8Dw5uXH");
// TODO: replace with deployed geolocation program ID for mainnet-beta (Currently using localnet key)
pub const ENV_MAINNET_BETA_GEOLOCATION_PUBKEY: Pubkey =
    Pubkey::from_str_const("36WA9nUCsJaAQL5h44WYoLezDpocy8Q71NZbtrUN8DyC");

// Constants related to DoubleZero testnet configuration
pub const ENV_TESTNET_DOUBLEZERO_LEDGER_RPC_URL: &str =
    "https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16";
pub const ENV_TESTNET_DOUBLEZERO_LEDGER_WS_RPC_URL: &str =
    "wss://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16/whirligig";
pub const ENV_TESTNET_SERVICEABILITY_PUBKEY: Pubkey =
    Pubkey::from_str_const("DZtnuQ839pSaDMFG5q1ad2V95G82S5EC4RrB3Ndw2Heb");
pub const ENV_TESTNET_TELEMETRY_PUBKEY: Pubkey =
    Pubkey::from_str_const("3KogTMmVxc5eUHtjZnwm136H5P8tvPwVu4ufbGPvM7p1");
pub const ENV_TESTNET_INTERNET_LATENCY_COLLECTOR_PUBKEY: Pubkey =
    Pubkey::from_str_const("HWGQSTmXWMB85NY2vFLhM1nGpXA8f4VCARRyeGNbqDF1");
// TODO: replace with deployed geolocation program ID for testnet (Currently using localnet key)
pub const ENV_TESTNET_GEOLOCATION_PUBKEY: Pubkey =
    Pubkey::from_str_const("36WA9nUCsJaAQL5h44WYoLezDpocy8Q71NZbtrUN8DyC");

// Constants related to DoubleZero devnet configuration
pub const ENV_DEVNET_DOUBLEZERO_LEDGER_RPC_URL: &str =
    "https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16";
pub const ENV_LEDGER_DOUBLEZERO_DEVNET_WS_RPC_URL: &str =
    "wss://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16/whirligig";
pub const ENV_DEVNET_SERVICEABILITY_PUBKEY: Pubkey =
    Pubkey::from_str_const("GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah");
pub const ENV_DEVNET_TELEMETRY_PUBKEY: Pubkey =
    Pubkey::from_str_const("C9xqH76NSm11pBS6maNnY163tWHT8Govww47uyEmSnoG");
pub const ENV_DEVNET_INTERNET_LATENCY_COLLECTOR_PUBKEY: Pubkey =
    Pubkey::from_str_const("3fXen9LP5JUAkaaDJtyLo1ohPiJ2LdzVqAnmhtGgAmwJ");
// TODO: replace with deployed geolocation program ID for devnet (Currently using localnet key)
pub const ENV_DEVNET_GEOLOCATION_PUBKEY: Pubkey =
    Pubkey::from_str_const("36WA9nUCsJaAQL5h44WYoLezDpocy8Q71NZbtrUN8DyC");

// Constants related to DoubleZero localnet configuration
pub const ENV_LOCAL_DOUBLEZERO_LEDGER_RPC_URL: &str = "http://localhost:8899";
pub const ENV_LOCAL_DOUBLEZERO_LEDGER_WS_RPC_URL: &str = "ws://localhost:8899";
pub const ENV_LOCAL_SERVICEABILITY_PUBKEY: Pubkey =
    Pubkey::from_str_const("7CTniUa88iJKUHTrCkB4TjAoG6TD7AMivhQeuqN2LPtX");
pub const ENV_LOCAL_TELEMETRY_PUBKEY: Pubkey =
    Pubkey::from_str_const("C9xqH76NSm11pBS6maNnY163tWHT8Govww47uyEmSnoG");
pub const ENV_LOCAL_INTERNET_LATENCY_COLLECTOR_PUBKEY: Pubkey =
    Pubkey::from_str_const("3fXen9LP5JUAkaaDJtyLo1ohPiJ2LdzVqAnmhtGgAmwJ");
pub const ENV_LOCAL_GEOLOCATION_PUBKEY: Pubkey =
    Pubkey::from_str_const("36WA9nUCsJaAQL5h44WYoLezDpocy8Q71NZbtrUN8DyC");
