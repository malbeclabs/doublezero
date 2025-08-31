use solana_sdk::pubkey::Pubkey;
use std::fmt;
use url::Url;

use crate::constants::*;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Environment {
    MainnetBeta,
    Testnet,
    Devnet,
    Localnet,
}

impl std::str::FromStr for Environment {
    type Err = eyre::Report;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s {
            "mainnet-beta" => Ok(Environment::MainnetBeta),
            "testnet" => Ok(Environment::Testnet),
            "devnet" => Ok(Environment::Devnet),
            "localnet" => Ok(Environment::Localnet),
            _ => Err(eyre::eyre!(
                "Invalid environment {}, must be one of: mainnet-beta, testnet, devnet",
                s
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
            Environment::Localnet => write!(f, "localnet"),
        }
    }
}

impl Environment {
    pub fn moniker(&self) -> String {
        self.to_string()
    }
}

impl Environment {
    pub fn config(&self) -> NetworkConfig {
        match self {
            Environment::MainnetBeta => NetworkConfig {
                ledger_public_rpc_url: MAINNET_BETA_LEDGER_PUBLIC_RPC_URL.clone(),
                ledger_public_ws_rpc_url: MAINNET_BETA_LEDGER_PUBLIC_WS_RPC_URL.clone(),
                serviceability_program_id: *MAINNET_BETA_SERVICEABILITY_PROGRAM_ID,
                telemetry_program_id: *MAINNET_BETA_TELEMETRY_PROGRAM_ID,
                internet_latency_collector_pk: *MAINNET_BETA_INTERNET_LATENCY_COLLECTOR_PK,
            },
            Environment::Testnet => NetworkConfig {
                ledger_public_rpc_url: TESTNET_LEDGER_PUBLIC_RPC_URL.clone(),
                ledger_public_ws_rpc_url: TESTNET_LEDGER_PUBLIC_WS_RPC_URL.clone(),
                serviceability_program_id: *TESTNET_SERVICEABILITY_PROGRAM_ID,
                telemetry_program_id: *TESTNET_TELEMETRY_PROGRAM_ID,
                internet_latency_collector_pk: *TESTNET_INTERNET_LATENCY_COLLECTOR_PK,
            },
            Environment::Devnet => NetworkConfig {
                ledger_public_rpc_url: DEVNET_LEDGER_PUBLIC_RPC_URL.clone(),
                ledger_public_ws_rpc_url: DEVNET_LEDGER_PUBLIC_WS_RPC_URL.clone(),
                serviceability_program_id: *DEVNET_SERVICEABILITY_PROGRAM_ID,
                telemetry_program_id: *DEVNET_TELEMETRY_PROGRAM_ID,
                internet_latency_collector_pk: *DEVNET_INTERNET_LATENCY_COLLECTOR_PK,
            },
            Environment::Localnet => NetworkConfig {
                ledger_public_rpc_url: LOCALNET_LEDGER_PUBLIC_RPC_URL.clone(),
                ledger_public_ws_rpc_url: LOCALNET_LEDGER_PUBLIC_WS_RPC_URL.clone(),
                serviceability_program_id: *LOCALNET_SERVICEABILITY_PROGRAM_ID,
                telemetry_program_id: *LOCALNET_TELEMETRY_PROGRAM_ID,
                internet_latency_collector_pk: *LOCALNET_INTERNET_LATENCY_COLLECTOR_PK,
            },
        }
    }

    pub fn config_with_override(&self) -> NetworkConfig {
        let mut config = self.config();

        if std::env::var("DZ_LEDGER_RPC_URL").is_ok() {
            config.ledger_public_rpc_url =
                Url::parse(&std::env::var("DZ_LEDGER_RPC_URL").unwrap()).unwrap();
        }

        if std::env::var("DZ_LEDGER_WS_RPC_URL").is_ok() {
            config.ledger_public_ws_rpc_url =
                Url::parse(&std::env::var("DZ_LEDGER_WS_RPC_URL").unwrap()).unwrap();
        }

        config
    }
}

#[derive(Debug, Clone)]
pub struct NetworkConfig {
    pub ledger_public_rpc_url: Url,
    pub ledger_public_ws_rpc_url: Url,
    pub serviceability_program_id: Pubkey,
    pub telemetry_program_id: Pubkey,
    pub internet_latency_collector_pk: Pubkey,
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
    fn test_network_config_mainnet_beta() {
        let config = Environment::MainnetBeta.config();
        assert_eq!(
            config.ledger_public_rpc_url,
            *MAINNET_BETA_LEDGER_PUBLIC_RPC_URL,
        );
        assert_eq!(
            config.ledger_public_ws_rpc_url,
            *MAINNET_BETA_LEDGER_PUBLIC_WS_RPC_URL,
        );
        assert_eq!(
            config.serviceability_program_id,
            *MAINNET_BETA_SERVICEABILITY_PROGRAM_ID,
        );
        assert_eq!(
            config.telemetry_program_id,
            *MAINNET_BETA_TELEMETRY_PROGRAM_ID,
        );
        assert_eq!(
            config.internet_latency_collector_pk,
            *MAINNET_BETA_INTERNET_LATENCY_COLLECTOR_PK,
        );
    }

    #[test]
    #[serial]
    fn test_network_config_testnet() {
        let config = Environment::Testnet.config();
        assert_eq!(config.ledger_public_rpc_url, *TESTNET_LEDGER_PUBLIC_RPC_URL,);
        assert_eq!(
            config.ledger_public_ws_rpc_url,
            *TESTNET_LEDGER_PUBLIC_WS_RPC_URL,
        );
        assert_eq!(
            config.serviceability_program_id,
            *TESTNET_SERVICEABILITY_PROGRAM_ID,
        );
        assert_eq!(config.telemetry_program_id, *TESTNET_TELEMETRY_PROGRAM_ID,);
        assert_eq!(
            config.internet_latency_collector_pk,
            *TESTNET_INTERNET_LATENCY_COLLECTOR_PK,
        );
    }

    #[test]
    #[serial]
    fn test_network_config_devnet() {
        let config = Environment::Devnet.config();
        assert_eq!(config.ledger_public_rpc_url, *DEVNET_LEDGER_PUBLIC_RPC_URL,);
        assert_eq!(
            config.ledger_public_ws_rpc_url,
            *DEVNET_LEDGER_PUBLIC_WS_RPC_URL,
        );
        assert_eq!(
            config.serviceability_program_id,
            *DEVNET_SERVICEABILITY_PROGRAM_ID,
        );
        assert_eq!(config.telemetry_program_id, *DEVNET_TELEMETRY_PROGRAM_ID,);
        assert_eq!(
            config.internet_latency_collector_pk,
            *DEVNET_INTERNET_LATENCY_COLLECTOR_PK,
        );
    }

    #[test]
    #[serial]
    fn test_network_config_localnet() {
        let config = Environment::Localnet.config();
        assert_eq!(
            config.ledger_public_rpc_url,
            *LOCALNET_LEDGER_PUBLIC_RPC_URL,
        );
        assert_eq!(
            config.ledger_public_ws_rpc_url,
            *LOCALNET_LEDGER_PUBLIC_WS_RPC_URL,
        );
        assert_eq!(
            config.serviceability_program_id,
            *LOCALNET_SERVICEABILITY_PROGRAM_ID,
        );
        assert_eq!(config.telemetry_program_id, *LOCALNET_TELEMETRY_PROGRAM_ID,);
        assert_eq!(
            config.internet_latency_collector_pk,
            *LOCALNET_INTERNET_LATENCY_COLLECTOR_PK,
        );
    }

    #[test]
    #[serial]
    fn test_network_config_rpc_url_env_override() {
        std::env::set_var("DZ_LEDGER_RPC_URL", "https://other-rpc-url.com");
        std::env::set_var("DZ_LEDGER_WS_RPC_URL", "wss://other-ws-rpc-url.com");
        let config = Environment::MainnetBeta.config_with_override();
        assert_eq!(
            config.ledger_public_rpc_url.to_string(),
            "https://other-rpc-url.com/"
        );
        assert_eq!(
            config.ledger_public_ws_rpc_url.to_string(),
            "wss://other-ws-rpc-url.com/"
        );

        // reset the values in the environment when complete
        std::env::remove_var("DZ_LEDGER_RPC_URL");
        std::env::remove_var("DZ_LEDGER_WS_RPC_URL");
    }
}
