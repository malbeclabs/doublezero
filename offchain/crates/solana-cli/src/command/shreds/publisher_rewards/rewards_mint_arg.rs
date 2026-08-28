use std::str::FromStr;

use anyhow::{Context, Result, anyhow};
use doublezero_solana_client_tools::rpc::SolanaConnection;
use doublezero_solana_sdk::{
    Pubkey, environment_2z_token_mint_key, environment_usdc_token_mint_key,
};

/// A `--rewards-token-mint` argument: either an explicit pubkey or a
/// well-known alias resolved against the connected network environment.
///
/// Aliases accepted (case-insensitive): `2z`, `usdc`, `wsol`.
#[derive(Debug, Clone)]
pub enum RewardsMintArg {
    Pubkey(Pubkey),
    Alias(MintAlias),
}

#[derive(Debug, Clone, Copy)]
pub enum MintAlias {
    TwoZ,
    Usdc,
    Wsol,
}

impl FromStr for RewardsMintArg {
    type Err = anyhow::Error;

    fn from_str(s: &str) -> Result<Self> {
        match s.to_ascii_lowercase().as_str() {
            "2z" => Ok(Self::Alias(MintAlias::TwoZ)),
            "usdc" => Ok(Self::Alias(MintAlias::Usdc)),
            "wsol" => Ok(Self::Alias(MintAlias::Wsol)),
            _ => Pubkey::from_str(s).map(Self::Pubkey).map_err(|_| {
                anyhow!("expected a base58 pubkey or one of '2z', 'usdc', 'wsol' (got '{s}')")
            }),
        }
    }
}

impl RewardsMintArg {
    /// Resolve to an on-chain mint pubkey. Looks up the network environment
    /// via `connection` only when the alias actually requires it.
    pub async fn resolve(&self, connection: &SolanaConnection) -> Result<Pubkey> {
        match self {
            Self::Pubkey(p) => Ok(*p),
            Self::Alias(MintAlias::TwoZ) => {
                let env = connection
                    .try_network_environment()
                    .await
                    .context("failed to determine network environment for '2z' alias")?;
                Ok(environment_2z_token_mint_key(env))
            }
            Self::Alias(MintAlias::Usdc) => {
                let env = connection
                    .try_network_environment()
                    .await
                    .context("failed to determine network environment for 'usdc' alias")?;
                Ok(environment_usdc_token_mint_key(env))
            }
            Self::Alias(MintAlias::Wsol) => Ok(spl_token_interface::native_mint::ID),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_aliases_case_insensitive() {
        for s in ["2z", "2Z"] {
            assert!(matches!(
                RewardsMintArg::from_str(s).unwrap(),
                RewardsMintArg::Alias(MintAlias::TwoZ)
            ));
        }
        for s in ["usdc", "USDC", "Usdc"] {
            assert!(matches!(
                RewardsMintArg::from_str(s).unwrap(),
                RewardsMintArg::Alias(MintAlias::Usdc)
            ));
        }
        for s in ["wsol", "WSOL"] {
            assert!(matches!(
                RewardsMintArg::from_str(s).unwrap(),
                RewardsMintArg::Alias(MintAlias::Wsol)
            ));
        }
    }

    #[test]
    fn parses_explicit_pubkey() {
        let pk = Pubkey::new_unique();
        let arg = RewardsMintArg::from_str(&pk.to_string()).unwrap();
        assert!(matches!(arg, RewardsMintArg::Pubkey(p) if p == pk));
    }

    #[test]
    fn rejects_unknown_alias() {
        let err = RewardsMintArg::from_str("eth").expect_err("unknown alias must error");
        assert!(
            err.to_string().contains("'2z', 'usdc', 'wsol'"),
            "got: {err}"
        );
    }
}
