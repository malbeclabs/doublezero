use solana_sdk::pubkey::Pubkey;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Environment {
    Mainnet,
    Testnet,
    Devnet,
}

impl std::str::FromStr for Environment {
    type Err = eyre::Report;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s {
            "mainnet" => Ok(Environment::Mainnet),
            "testnet" => Ok(Environment::Testnet),
            "devnet" => Ok(Environment::Devnet),
            _ => Err(eyre::eyre!("Invalid environment: {}", s)),
        }
    }
}

impl Environment {
    pub fn config(&self) -> eyre::Result<NetworkConfig> {
        let config = match self {
            Environment::Mainnet => NetworkConfig {
                ledger_rpc_url: "TODO".to_string(),
                ledger_ws_url: "TODO".to_string(),
                serviceability_program_id: "TODO".parse()?,
                telemetry_program_id: "TODO".parse()?,
                internet_latency_collector_pk: "TODO".parse()?,
            },
            Environment::Testnet => NetworkConfig {
                ledger_rpc_url: "https://doublezerolocalnet.rpcpool.com/f50e62d0-06e7-410e-867e-6873e358ed30".to_string(),
                ledger_ws_url: "wss://doublezerolocalnet.rpcpool.com/f50e62d0-06e7-410e-867e-6873e358ed30/whirligig".to_string(),
                serviceability_program_id: "DZtnuQ839pSaDMFG5q1ad2V95G82S5EC4RrB3Ndw2Heb".parse()?,
                telemetry_program_id: "3KogTMmVxc5eUHtjZnwm136H5P8tvPwVu4ufbGPvM7p1".parse()?,
                internet_latency_collector_pk: "HWGQSTmXWMB85NY2vFLhM1nGpXA8f4VCARRyeGNbqDF1".parse()?,
            },
            Environment::Devnet => NetworkConfig {
                ledger_rpc_url: "https://doublezerolocalnet.rpcpool.com/f50e62d0-06e7-410e-867e-6873e358ed30".to_string(),
                ledger_ws_url: "wss://doublezerolocalnet.rpcpool.com/f50e62d0-06e7-410e-867e-6873e358ed30/whirligig".to_string(),
                serviceability_program_id: "GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah".parse()?,
                telemetry_program_id: "C9xqH76NSm11pBS6maNnY163tWHT8Govww47uyEmSnoG".parse()?,
                internet_latency_collector_pk: "3fXen9LP5JUAkaaDJtyLo1ohPiJ2LdzVqAnmhtGgAmwJ".parse()?,
            },
        };

        Ok(config)
    }
}

#[derive(Debug, Clone)]
pub struct NetworkConfig {
    pub ledger_rpc_url: String,
    pub ledger_ws_url: String,
    pub serviceability_program_id: Pubkey,
    pub telemetry_program_id: Pubkey,
    pub internet_latency_collector_pk: Pubkey,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
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
    fn test_environment_from_str_invalid() {
        let err = "mainnet".parse::<Environment>();
        assert!(err.is_err());
    }

    #[test]
    fn test_network_config_mainnet() {
        let config = Environment::Mainnet.config().unwrap();
        assert_eq!(config.ledger_rpc_url, "TODO");
        assert_eq!(config.ledger_ws_url, "TODO");
        assert_eq!(config.serviceability_program_id.to_string(), "TODO");
        assert_eq!(config.telemetry_program_id.to_string(), "TODO");
        assert_eq!(config.internet_latency_collector_pk.to_string(), "TODO");
    }

    #[test]
    fn test_network_config_testnet() {
        let config = Environment::Testnet.config().unwrap();
        assert_eq!(
            config.ledger_rpc_url,
            "https://doublezerolocalnet.rpcpool.com/f50e62d0-06e7-410e-867e-6873e358ed30"
        );
        assert_eq!(
            config.ledger_ws_url,
            "wss://doublezerolocalnet.rpcpool.com/f50e62d0-06e7-410e-867e-6873e358ed30/whirligig"
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
    fn test_network_config_devnet() {
        let config = Environment::Devnet.config().unwrap();
        assert_eq!(
            config.ledger_rpc_url,
            "https://doublezerolocalnet.rpcpool.com/f50e62d0-06e7-410e-867e-6873e358ed30"
        );
        assert_eq!(
            config.ledger_ws_url,
            "wss://doublezerolocalnet.rpcpool.com/f50e62d0-06e7-410e-867e-6873e358ed30/whirligig"
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
}
