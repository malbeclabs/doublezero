use serde::{Deserialize, Serialize};
use std::{fmt, str::FromStr};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, Default)]
#[serde(rename_all = "kebab-case")]
pub enum Network {
    Devnet,
    #[default]
    Testnet,
    #[serde(rename = "mainnet-beta")]
    MainnetBeta,
    Mainnet,
}

impl fmt::Display for Network {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Network::Devnet => write!(f, "devnet"),
            Network::Testnet => write!(f, "testnet"),
            Network::MainnetBeta => write!(f, "mainnet-beta"),
            Network::Mainnet => write!(f, "mainnet"),
        }
    }
}

impl FromStr for Network {
    type Err = String;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s.to_lowercase().as_str() {
            "devnet" => Ok(Network::Devnet),
            "testnet" => Ok(Network::Testnet),
            "mainnet-beta" => Ok(Network::MainnetBeta),
            "mainnet" => Ok(Network::Mainnet),
            _ => Err(format!(
                "Invalid network: {s}. Valid options are: devnet, testnet, mainnet-beta, mainnet"
            )),
        }
    }
}

impl Network {
    /// Get the default RPC endpoint for this network
    pub fn default_rpc_endpoint(&self) -> &str {
        match self {
            Network::Devnet => "https://api.devnet.solana.com",
            Network::Testnet => "https://api.testnet.solana.com",
            Network::MainnetBeta => "https://api.mainnet-beta.solana.com",
            Network::Mainnet => "https://api.mainnet.solana.com",
        }
    }

    /// Check if this is a production network
    pub fn is_production(&self) -> bool {
        matches!(self, Network::MainnetBeta | Network::Mainnet)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_network_from_str() {
        assert_eq!(Network::from_str("devnet").unwrap(), Network::Devnet);
        assert_eq!(Network::from_str("testnet").unwrap(), Network::Testnet);
        assert_eq!(
            Network::from_str("mainnet-beta").unwrap(),
            Network::MainnetBeta
        );
        assert_eq!(Network::from_str("mainnet").unwrap(), Network::Mainnet);
        assert!(Network::from_str("invalid").is_err());
    }

    #[test]
    fn test_network_display() {
        assert_eq!(Network::Devnet.to_string(), "devnet");
        assert_eq!(Network::MainnetBeta.to_string(), "mainnet-beta");
    }

    #[test]
    fn test_is_production() {
        assert!(!Network::Devnet.is_production());
        assert!(!Network::Testnet.is_production());
        assert!(Network::MainnetBeta.is_production());
        assert!(Network::Mainnet.is_production());
    }
}
