use solana_sdk::pubkey::Pubkey;
use std::fmt;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Environment {
    MainnetBeta,
    Testnet,
    Devnet,
}

impl std::str::FromStr for Environment {
    type Err = eyre::Report;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s {
            "mainnet-beta" => Ok(Environment::MainnetBeta),
            "testnet" => Ok(Environment::Testnet),
            "devnet" => Ok(Environment::Devnet),
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
        }
    }
}

impl Environment {
    pub fn config(&self) -> eyre::Result<NetworkConfig> {
        let mut config = match self {
            Environment::MainnetBeta => NetworkConfig {
                ledger_public_rpc_url: "https://doublezero-mainnet-beta.rpcpool.com/db336024-e7a8-46b1-80e5-352dd77060ab".to_string(),
                ledger_public_ws_rpc_url: "wss://doublezero-mainnet-beta.rpcpool.com/db336024-e7a8-46b1-80e5-352dd77060ab".to_string(),
                serviceability_program_id: "ser2VaTMAcYTaauMrTSfSrxBaUDq7BLNs2xfUugTAGv".parse()?,
                telemetry_program_id: "tE1exJ5VMyoC9ByZeSmgtNzJCFF74G9JAv338sJiqkC".parse()?,
                internet_latency_collector_pk: "8xHn4r7oQuqNZ5cLYwL5YZcDy1JjDQcpVkyoA8Dw5uXH".parse()?,
            },
            Environment::Testnet => NetworkConfig {
                ledger_public_rpc_url: "https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16".to_string(),
                ledger_public_ws_rpc_url: "wss://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16/whirligig".to_string(),
                serviceability_program_id: "DZtnuQ839pSaDMFG5q1ad2V95G82S5EC4RrB3Ndw2Heb".parse()?,
                telemetry_program_id: "3KogTMmVxc5eUHtjZnwm136H5P8tvPwVu4ufbGPvM7p1".parse()?,
                internet_latency_collector_pk: "HWGQSTmXWMB85NY2vFLhM1nGpXA8f4VCARRyeGNbqDF1".parse()?,
            },
            Environment::Devnet => NetworkConfig {
                ledger_public_rpc_url: "https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16".to_string(),
                ledger_public_ws_rpc_url: "wss://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16/whirligig".to_string(),
                serviceability_program_id: "GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah".parse()?,
                telemetry_program_id: "C9xqH76NSm11pBS6maNnY163tWHT8Govww47uyEmSnoG".parse()?,
                internet_latency_collector_pk: "3fXen9LP5JUAkaaDJtyLo1ohPiJ2LdzVqAnmhtGgAmwJ".parse()?,
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
            "https://doublezero-mainnet-beta.rpcpool.com/db336024-e7a8-46b1-80e5-352dd77060ab"
        );
        assert_eq!(
            config.ledger_public_ws_rpc_url,
            "wss://doublezero-mainnet-beta.rpcpool.com/db336024-e7a8-46b1-80e5-352dd77060ab"
        );
        assert_eq!(
            config.serviceability_program_id.to_string(),
            "ser2VaTMAcYTaauMrTSfSrxBaUDq7BLNs2xfUugTAGv"
        );
        assert_eq!(
            config.telemetry_program_id.to_string(),
            "tE1exJ5VMyoC9ByZeSmgtNzJCFF74G9JAv338sJiqkC"
        );
        assert_eq!(
            config.internet_latency_collector_pk.to_string(),
            "8xHn4r7oQuqNZ5cLYwL5YZcDy1JjDQcpVkyoA8Dw5uXH"
        );
    }

    #[test]
    #[serial]
    fn test_network_config_testnet() {
        let config = Environment::Testnet.config().unwrap();
        assert_eq!(
            config.ledger_public_rpc_url,
            "https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16"
        );
        assert_eq!(
            config.ledger_public_ws_rpc_url,
            "wss://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16/whirligig"
        );
        assert_eq!(
            config.serviceability_program_id.to_string(),
            "DZtnuQ839pSaDMFG5q1ad2V95G82S5EC4RrB3Ndw2Heb"
        );
        assert_eq!(
            config.telemetry_program_id.to_string(),
            "3KogTMmVxc5eUHtjZnwm136H5P8tvPwVu4ufbGPvM7p1"
        );
        assert_eq!(
            config.internet_latency_collector_pk.to_string(),
            "HWGQSTmXWMB85NY2vFLhM1nGpXA8f4VCARRyeGNbqDF1"
        );
    }

    #[test]
    #[serial]
    fn test_network_config_devnet() {
        let config = Environment::Devnet.config().unwrap();
        assert_eq!(
            config.ledger_public_rpc_url,
            "https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16"
        );
        assert_eq!(
            config.ledger_public_ws_rpc_url,
            "wss://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16/whirligig"
        );
        assert_eq!(
            config.serviceability_program_id.to_string(),
            "GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah"
        );
        assert_eq!(
            config.telemetry_program_id.to_string(),
            "C9xqH76NSm11pBS6maNnY163tWHT8Govww47uyEmSnoG"
        );
        assert_eq!(
            config.internet_latency_collector_pk.to_string(),
            "3fXen9LP5JUAkaaDJtyLo1ohPiJ2LdzVqAnmhtGgAmwJ"
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
}
