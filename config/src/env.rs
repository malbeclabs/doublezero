use eyre::Ok;
use serde::{Deserialize, Serialize};
use solana_sdk::pubkey::Pubkey;
use std::fmt;

use crate::constants::*;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, Default)]
pub enum Environment {
    MainnetBeta,
    Testnet,
    #[default]
    Devnet,
    Local,
}

impl std::str::FromStr for Environment {
    type Err = eyre::Report;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s {
            ENV_MAINNET_BETA_SHORT_NAME | ENV_MAINNET_BETA_NAME => Ok(Environment::MainnetBeta),
            ENV_TESTNET_SHORT_NAME | ENV_TESTNET_NAME => Ok(Environment::Testnet),
            ENV_DEVNET_SHORT_NAME | ENV_DEVNET_NAME => Ok(Environment::Devnet),
            ENV_LOCALNET_SHORT_NAME | ENV_LOCALNET_NAME => Ok(Environment::Local),
            _ => Err(eyre::eyre!(
                "Invalid environment {s}, must be one of: {ENV_MAINNET_BETA_NAME}, {ENV_TESTNET_NAME}, {ENV_DEVNET_NAME}, {ENV_LOCALNET_NAME}",
            )),
        }
    }
}

impl fmt::Display for Environment {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Environment::MainnetBeta => write!(f, "mainnet-beta"),
            Environment::Testnet => write!(f, "testnet"),
            Environment::Devnet => write!(f, "devnet"),
            Environment::Local => write!(f, "local"),
        }
    }
}

impl Environment {
    pub fn from_program_id(program_id: &str) -> eyre::Result<Environment> {
        if program_id.eq(&ENV_MAINNET_BETA_SERVICEABILITY_PUBKEY.to_string()) {
            return Ok(Environment::MainnetBeta);
        } else if program_id.eq(&ENV_TESTNET_SERVICEABILITY_PUBKEY.to_string()) {
            return Ok(Environment::Testnet);
        } else if program_id.eq(&ENV_DEVNET_SERVICEABILITY_PUBKEY.to_string()) {
            return Ok(Environment::Devnet);
        } else if program_id.eq(&ENV_LOCAL_SERVICEABILITY_PUBKEY.to_string()) {
            return Ok(Environment::Local);
        }
        Err(eyre::eyre!(
            "Could not match environment from Program ID: {program_id}"
        ))
    }

    pub fn config(&self) -> eyre::Result<NetworkConfig> {
        let mut config = match self {
            Environment::MainnetBeta => NetworkConfig {
                ledger_public_rpc_url: ENV_MAINNET_BETA_DOUBLEZERO_LEDGER_RPC_URL.to_string(),
                ledger_public_ws_rpc_url: ENV_MAINNET_BETA_DOUBLEZERO_LEDGER_WS_RPC_URL.to_string(),
                serviceability_program_id: ENV_MAINNET_BETA_SERVICEABILITY_PUBKEY,
                telemetry_program_id: ENV_MAINNET_BETA_TELEMETRY_PUBKEY,
                internet_latency_collector_pk: ENV_MAINNET_BETA_INTERNET_LATENCY_COLLECTOR_PUBKEY,
                geolocation_program_id: ENV_MAINNET_BETA_GEOLOCATION_PUBKEY,
            },
            Environment::Testnet => NetworkConfig {
                ledger_public_rpc_url: ENV_TESTNET_DOUBLEZERO_LEDGER_RPC_URL.to_string(),
                ledger_public_ws_rpc_url: ENV_TESTNET_DOUBLEZERO_LEDGER_WS_RPC_URL.to_string(),
                serviceability_program_id: ENV_TESTNET_SERVICEABILITY_PUBKEY,
                telemetry_program_id: ENV_TESTNET_TELEMETRY_PUBKEY,
                internet_latency_collector_pk: ENV_TESTNET_INTERNET_LATENCY_COLLECTOR_PUBKEY,
                geolocation_program_id: ENV_TESTNET_GEOLOCATION_PUBKEY,
            },
            Environment::Devnet => NetworkConfig {
                ledger_public_rpc_url: ENV_DEVNET_DOUBLEZERO_LEDGER_RPC_URL.to_string(),
                ledger_public_ws_rpc_url: ENV_LEDGER_DOUBLEZERO_DEVNET_WS_RPC_URL.to_string(),
                serviceability_program_id: ENV_DEVNET_SERVICEABILITY_PUBKEY,
                telemetry_program_id: ENV_DEVNET_TELEMETRY_PUBKEY,
                internet_latency_collector_pk: ENV_DEVNET_INTERNET_LATENCY_COLLECTOR_PUBKEY,
                geolocation_program_id: ENV_DEVNET_GEOLOCATION_PUBKEY,
            },
            Environment::Local => NetworkConfig {
                ledger_public_rpc_url: ENV_LOCAL_DOUBLEZERO_LEDGER_RPC_URL.to_string(),
                ledger_public_ws_rpc_url: ENV_LOCAL_DOUBLEZERO_LEDGER_WS_RPC_URL.to_string(),
                serviceability_program_id: ENV_LOCAL_SERVICEABILITY_PUBKEY,
                telemetry_program_id: ENV_LOCAL_TELEMETRY_PUBKEY,
                internet_latency_collector_pk: ENV_LOCAL_INTERNET_LATENCY_COLLECTOR_PUBKEY,
                geolocation_program_id: ENV_LOCAL_GEOLOCATION_PUBKEY,
            },
        };

        if std::env::var("DZ_LEDGER_RPC_URL").is_ok() {
            config.ledger_public_rpc_url = std::env::var("DZ_LEDGER_RPC_URL").unwrap();
        }
        if std::env::var("DZ_LEDGER_WS_RPC_URL").is_ok() {
            config.ledger_public_ws_rpc_url = std::env::var("DZ_LEDGER_WS_RPC_URL").unwrap();
        }

        Ok(config)
    }
}

#[derive(Debug, Clone)]
pub struct NetworkConfig {
    pub ledger_public_rpc_url: String,
    pub ledger_public_ws_rpc_url: String,
    pub serviceability_program_id: Pubkey,
    pub telemetry_program_id: Pubkey,
    pub internet_latency_collector_pk: Pubkey,
    pub geolocation_program_id: Pubkey,
}

#[cfg(test)]
mod tests {
    use serial_test::serial;

    use super::*;

    #[test]
    #[serial]
    fn test_environment_from_str_valid() {
        assert_eq!(
            "testnet".parse::<Environment>().unwrap(),
            Environment::Testnet
        );
        assert_eq!(
            "devnet".parse::<Environment>().unwrap(),
            Environment::Devnet
        );
    }

    #[test]
    #[serial]
    fn test_environment_from_str_invalid() {
        let err = "invalid".parse::<Environment>();
        assert!(err.is_err());
    }

    #[test]
    #[serial]
    fn test_network_config_mainnet() {
        let config = Environment::MainnetBeta.config().unwrap();
        assert_eq!(
            config.ledger_public_rpc_url,
            "https://doublezero-mainnet-beta.rpcpool.com/db336024-e7a8-46b1-80e5-352dd77060ab",
            "Invalid RPC URL"
        );
        assert_eq!(
            config.ledger_public_ws_rpc_url,
            "wss://doublezero-mainnet-beta.rpcpool.com/db336024-e7a8-46b1-80e5-352dd77060ab",
            "Invalid WebSocket URL"
        );
        assert_eq!(
            config.serviceability_program_id.to_string(),
            "ser2VaTMAcYTaauMrTSfSrxBaUDq7BLNs2xfUugTAGv",
            "Invalid Serviceability Program ID"
        );
        assert_eq!(
            config.telemetry_program_id.to_string(),
            "tE1exJ5VMyoC9ByZeSmgtNzJCFF74G9JAv338sJiqkC",
            "Invalid Telemetry Program ID"
        );
        assert_eq!(
            config.internet_latency_collector_pk.to_string(),
            "8xHn4r7oQuqNZ5cLYwL5YZcDy1JjDQcpVkyoA8Dw5uXH",
            "Invalid Internet Latency Collector Program ID"
        );
    }

    #[test]
    #[serial]
    fn test_network_config_testnet() {
        let config = Environment::Testnet.config().unwrap();
        assert_eq!(
            config.ledger_public_rpc_url,
            "https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16",
            "Invalid RPC URL"
        );
        assert_eq!(
            config.ledger_public_ws_rpc_url,
            "wss://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16/whirligig",
            "Invalid WebSocket URL"
        );
        assert_eq!(
            config.serviceability_program_id.to_string(),
            "DZtnuQ839pSaDMFG5q1ad2V95G82S5EC4RrB3Ndw2Heb",
            "Invalid Serviceability Program ID"
        );
        assert_eq!(
            config.telemetry_program_id.to_string(),
            "3KogTMmVxc5eUHtjZnwm136H5P8tvPwVu4ufbGPvM7p1",
            "Invalid Telemetry Program ID"
        );
        assert_eq!(
            config.internet_latency_collector_pk.to_string(),
            "HWGQSTmXWMB85NY2vFLhM1nGpXA8f4VCARRyeGNbqDF1",
            "Invalid Internet Latency Collector Program ID"
        );
    }

    #[test]
    #[serial]
    fn test_network_config_devnet() {
        let config = Environment::Devnet.config().unwrap();
        assert_eq!(
            config.ledger_public_rpc_url,
            "https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16",
            "Invalid RPC URL"
        );
        assert_eq!(
            config.ledger_public_ws_rpc_url,
            "wss://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16/whirligig",
            "Invalid WebSocket URL"
        );
        assert_eq!(
            config.serviceability_program_id.to_string(),
            "GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah",
            "Invalid Serviceability Program ID"
        );
        assert_eq!(
            config.telemetry_program_id.to_string(),
            "C9xqH76NSm11pBS6maNnY163tWHT8Govww47uyEmSnoG",
            "Invalid Telemetry Program ID"
        );
        assert_eq!(
            config.internet_latency_collector_pk.to_string(),
            "3fXen9LP5JUAkaaDJtyLo1ohPiJ2LdzVqAnmhtGgAmwJ",
            "Invalid Internet Latency Collector Program ID"
        );
    }

    #[test]
    #[serial]
    fn test_network_config_rpc_url_env_override() {
        std::env::set_var("DZ_LEDGER_RPC_URL", "https://other-rpc-url.com");
        std::env::set_var("DZ_LEDGER_WS_RPC_URL", "wss://other-ws-rpc-url.com");
        let config = Environment::MainnetBeta.config().unwrap();
        assert_eq!(config.ledger_public_rpc_url, "https://other-rpc-url.com");
        assert_eq!(
            config.ledger_public_ws_rpc_url,
            "wss://other-ws-rpc-url.com"
        );

        // reset the values in the environment when complete
        std::env::remove_var("DZ_LEDGER_RPC_URL");
        std::env::remove_var("DZ_LEDGER_WS_RPC_URL");
    }

    #[test]
    #[serial]
    fn test_environment_match_environment() {
        let env = Environment::from_program_id(&ENV_MAINNET_BETA_SERVICEABILITY_PUBKEY.to_string())
            .unwrap();
        assert_eq!(env, Environment::MainnetBeta);

        let env =
            Environment::from_program_id(&ENV_TESTNET_SERVICEABILITY_PUBKEY.to_string()).unwrap();
        assert_eq!(env, Environment::Testnet);

        let env =
            Environment::from_program_id(&ENV_DEVNET_SERVICEABILITY_PUBKEY.to_string()).unwrap();
        assert_eq!(env, Environment::Devnet);

        let err = Environment::from_program_id(&Pubkey::default().to_string());
        assert!(err.is_err());
    }
}
