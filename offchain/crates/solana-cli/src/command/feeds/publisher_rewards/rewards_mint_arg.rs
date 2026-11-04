use std::str::FromStr;

use anyhow::{Context, Result};
use doublezero_solana_client_tools::rpc::SolanaConnection;
use doublezero_solana_sdk::{
    Pubkey, environment_2z_token_mint_key, environment_usdc_token_mint_key,
};

// A `--rewards-token-mint` value: a base58 pubkey, or `2z`, `usdc` or `wsol` in any case, which
// resolve against the cluster the command connects to.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum RewardsMintArg {
    Pubkey(Pubkey),
    TwoZ,
    Usdc,
    Wsol,
}

impl FromStr for RewardsMintArg {
    type Err = anyhow::Error;

    fn from_str(s: &str) -> Result<Self> {
        match s.to_ascii_lowercase().as_str() {
            "2z" => Ok(Self::TwoZ),
            "usdc" => Ok(Self::Usdc),
            "wsol" => Ok(Self::Wsol),
            _ => Pubkey::from_str(s).map(Self::Pubkey).with_context(|| {
                format!("expected a base58 pubkey or one of '2z', 'usdc', 'wsol' (got '{s}')")
            }),
        }
    }
}

impl RewardsMintArg {
    pub async fn try_resolve(self, connection: &SolanaConnection) -> Result<Pubkey> {
        match self {
            Self::Pubkey(mint_key) => Ok(mint_key),
            Self::TwoZ => {
                let network_env = connection
                    .try_network_environment()
                    .await
                    .context("detecting the network environment for the '2z' alias")?;
                Ok(environment_2z_token_mint_key(network_env))
            }
            Self::Usdc => {
                let network_env = connection
                    .try_network_environment()
                    .await
                    .context("detecting the network environment for the 'usdc' alias")?;
                Ok(environment_usdc_token_mint_key(network_env))
            }
            Self::Wsol => Ok(spl_token_interface::native_mint::ID),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parses_aliases_in_any_case_and_pubkeys() {
        for (s, expected) in [
            ("2z", RewardsMintArg::TwoZ),
            ("2Z", RewardsMintArg::TwoZ),
            ("usdc", RewardsMintArg::Usdc),
            ("USDC", RewardsMintArg::Usdc),
            ("wsol", RewardsMintArg::Wsol),
            ("WSol", RewardsMintArg::Wsol),
        ] {
            assert_eq!(RewardsMintArg::from_str(s).unwrap(), expected);
        }

        let mint_key = Pubkey::new_unique();
        assert_eq!(
            RewardsMintArg::from_str(&mint_key.to_string()).unwrap(),
            RewardsMintArg::Pubkey(mint_key)
        );
    }

    #[test]
    fn test_refuses_an_unknown_alias() {
        assert_eq!(
            RewardsMintArg::from_str("eth").unwrap_err().to_string(),
            "expected a base58 pubkey or one of '2z', 'usdc', 'wsol' (got 'eth')"
        );
    }
}
