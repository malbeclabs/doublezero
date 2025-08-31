use solana_sdk::pubkey::Pubkey;
use std::{str::FromStr, sync::LazyLock};
use url::Url;

// Mainnet constants.
pub static MAINNET_BETA_LEDGER_PUBLIC_RPC_URL: LazyLock<Url> = LazyLock::new(|| {
    Url::parse("https://doublezero-mainnet-beta.rpcpool.com/db336024-e7a8-46b1-80e5-352dd77060ab")
        .unwrap()
});
pub static MAINNET_BETA_LEDGER_PUBLIC_WS_RPC_URL: LazyLock<Url> = LazyLock::new(|| {
    Url::parse("wss://doublezero-mainnet-beta.rpcpool.com/db336024-e7a8-46b1-80e5-352dd77060ab")
        .unwrap()
});
pub static MAINNET_BETA_SERVICEABILITY_PROGRAM_ID: LazyLock<Pubkey> =
    LazyLock::new(|| Pubkey::from_str("ser2VaTMAcYTaauMrTSfSrxBaUDq7BLNs2xfUugTAGv").unwrap());
pub static MAINNET_BETA_TELEMETRY_PROGRAM_ID: LazyLock<Pubkey> =
    LazyLock::new(|| Pubkey::from_str("tE1exJ5VMyoC9ByZeSmgtNzJCFF74G9JAv338sJiqkC").unwrap());
pub static MAINNET_BETA_INTERNET_LATENCY_COLLECTOR_PK: LazyLock<Pubkey> =
    LazyLock::new(|| Pubkey::from_str("8xHn4r7oQuqNZ5cLYwL5YZcDy1JjDQcpVkyoA8Dw5uXH").unwrap());

// Testnet constants.
pub static TESTNET_LEDGER_PUBLIC_RPC_URL: LazyLock<Url> = LazyLock::new(|| {
    Url::parse("https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16")
        .unwrap()
});
pub static TESTNET_LEDGER_PUBLIC_WS_RPC_URL: LazyLock<Url> = LazyLock::new(|| {
    Url::parse(
        "wss://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16/whirligig",
    )
    .unwrap()
});
pub static TESTNET_SERVICEABILITY_PROGRAM_ID: LazyLock<Pubkey> =
    LazyLock::new(|| Pubkey::from_str("DZtnuQ839pSaDMFG5q1ad2V95G82S5EC4RrB3Ndw2Heb").unwrap());
pub static TESTNET_TELEMETRY_PROGRAM_ID: LazyLock<Pubkey> =
    LazyLock::new(|| Pubkey::from_str("3KogTMmVxc5eUHtjZnwm136H5P8tvPwVu4ufbGPvM7p1").unwrap());
pub static TESTNET_INTERNET_LATENCY_COLLECTOR_PK: LazyLock<Pubkey> =
    LazyLock::new(|| Pubkey::from_str("HWGQSTmXWMB85NY2vFLhM1nGpXA8f4VCARRyeGNbqDF1").unwrap());

// Devnet constants.
pub static DEVNET_LEDGER_PUBLIC_RPC_URL: LazyLock<Url> = LazyLock::new(|| {
    Url::parse("https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16")
        .unwrap()
});
pub static DEVNET_LEDGER_PUBLIC_WS_RPC_URL: LazyLock<Url> = LazyLock::new(|| {
    Url::parse(
        "wss://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16/whirligig",
    )
    .unwrap()
});
pub static DEVNET_SERVICEABILITY_PROGRAM_ID: LazyLock<Pubkey> =
    LazyLock::new(|| Pubkey::from_str("GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah").unwrap());
pub static DEVNET_TELEMETRY_PROGRAM_ID: LazyLock<Pubkey> =
    LazyLock::new(|| Pubkey::from_str("C9xqH76NSm11pBS6maNnY163tWHT8Govww47uyEmSnoG").unwrap());
pub static DEVNET_INTERNET_LATENCY_COLLECTOR_PK: LazyLock<Pubkey> =
    LazyLock::new(|| Pubkey::from_str("3fXen9LP5JUAkaaDJtyLo1ohPiJ2LdzVqAnmhtGgAmwJ").unwrap());

// Localnet constants.
pub static LOCALNET_LEDGER_PUBLIC_RPC_URL: LazyLock<Url> =
    LazyLock::new(|| Url::parse("http://localhost:8899").unwrap());
pub static LOCALNET_LEDGER_PUBLIC_WS_RPC_URL: LazyLock<Url> =
    LazyLock::new(|| Url::parse("ws://localhost:8900").unwrap());
pub static LOCALNET_SERVICEABILITY_PROGRAM_ID: LazyLock<Pubkey> =
    LazyLock::new(|| Pubkey::from_str("7CTniUa88iJKUHTrCkB4TjAoG6TD7AMivhQeuqN2LPtX").unwrap());
pub static LOCALNET_TELEMETRY_PROGRAM_ID: LazyLock<Pubkey> =
    LazyLock::new(|| Pubkey::from_str("EekrFoi4FaRvc3VvNRh6ofoNL153n1f9iU3qcws9sXoY").unwrap());
pub static LOCALNET_INTERNET_LATENCY_COLLECTOR_PK: LazyLock<Pubkey> =
    LazyLock::new(|| Pubkey::from_str("Ci7E2m9BfEzpyHC4VYJHT4f9ZVK14Y9fBAQ4vxrZjkUR").unwrap());
