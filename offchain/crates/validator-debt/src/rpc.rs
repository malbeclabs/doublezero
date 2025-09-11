use anyhow::{Error, Result};
use clap::Args;
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_sdk::commitment_config::CommitmentConfig;
use solana_transaction_status_client_types::{TransactionDetails, UiTransactionEncoding};

use solana_client::rpc_config::{RpcBlockConfig, RpcGetVoteAccountsConfig};
use url::Url;

use crate::solana_debt_calculator::SolanaDebtCalculator;

#[derive(Debug, Args)]
pub struct SolanaValidatorDebtConnectionOptions {
    /// URL for DoubleZero Ledger's JSON RPC. Required.
    #[arg(long)]
    pub dz_ledger_url: Option<String>,

    /// URL for Solana's JSON RPC or moniker (or their first letter):
    /// [mainnet-beta, testnet, localhost].
    #[arg(long = "url", short = 'u')]
    pub solana_url_or_moniker: Option<String>,
}

impl TryFrom<SolanaValidatorDebtConnectionOptions> for SolanaDebtCalculator {
    type Error = Error;

    fn try_from(opts: SolanaValidatorDebtConnectionOptions) -> Result<SolanaDebtCalculator> {
        let SolanaValidatorDebtConnectionOptions {
            solana_url_or_moniker,
            dz_ledger_url,
        } = opts;

        let ledger_url = dz_ledger_url.as_deref().unwrap_or("mainnet-beta");
        let ledger_rpc_url = Url::parse(normalize_to_ledger_url(ledger_url))?;

        let ledger_rpc_client =
            RpcClient::new_with_commitment(ledger_rpc_url.into(), CommitmentConfig::confirmed());

        let solana_url_or_moniker = solana_url_or_moniker.as_deref().unwrap_or("m");
        let solana_url = Url::parse(normalize_to_url_if_moniker(solana_url_or_moniker))?;

        let solana_rpc_client =
            RpcClient::new_with_commitment(solana_url.into(), CommitmentConfig::confirmed());

        let rpc_block_config = RpcBlockConfig {
            encoding: Some(UiTransactionEncoding::Base58),
            transaction_details: Some(TransactionDetails::Signatures),
            rewards: Some(true),
            commitment: None,
            max_supported_transaction_version: Some(0),
        };

        let vote_accounts_config = RpcGetVoteAccountsConfig {
            vote_pubkey: None,
            commitment: CommitmentConfig::confirmed().into(),
            keep_unstaked_delinquents: None,
            delinquent_slot_distance: None,
        };

        Ok(SolanaDebtCalculator {
            ledger_rpc_client,
            solana_rpc_client,
            vote_accounts_config,
            rpc_block_config,
        })
    }
}

// Forked from solana-clap-utils.
fn normalize_to_url_if_moniker(url_or_moniker: &str) -> &str {
    match url_or_moniker {
        "m" | "mainnet-beta" => "https://api.mainnet-beta.solana.com",
        "t" | "testnet" => "https://api.testnet.solana.com",
        "l" | "localhost" => "http://localhost:8899",
        url => url,
    }
}

fn normalize_to_ledger_url(url: &str) -> &str {
    match url {
        "m" | "mainnet-beta" => "",
        "t" | "testnet" => "",
        "l" | "localhost" => "http://localhost:8899",
        url => url,
    }
}
