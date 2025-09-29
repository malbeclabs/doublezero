use anyhow::{Error, Result, bail, ensure};
use clap::Args;
use doublezero_solana_client_tools::log_warn;
use leaky_bucket::RateLimiter;
use solana_client::{
    client_error::{ClientError, ClientErrorKind},
    nonblocking::rpc_client::RpcClient,
    rpc_custom_error::{
        JSON_RPC_SERVER_ERROR_LONG_TERM_STORAGE_SLOT_SKIPPED, JSON_RPC_SERVER_ERROR_SLOT_SKIPPED,
    },
    rpc_request::RpcError,
};
use solana_sdk::commitment_config::CommitmentConfig;
use solana_transaction_status_client_types::{TransactionDetails, UiTransactionEncoding};

use solana_client::rpc_config::{RpcBlockConfig, RpcGetVoteAccountsConfig};
use url::Url;

use crate::solana_debt_calculator::SolanaDebtCalculator;

#[derive(Debug, Args)]
pub struct SolanaValidatorDebtConnectionOptions {
    /// URL for DoubleZero Ledger's JSON RPC. Required.
    #[arg(long)]
    pub dz_ledger_url: String,

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

        let ledger_rpc_client = Url::parse(&dz_ledger_url)
            .map(|url| RpcClient::new_with_commitment(url.into(), CommitmentConfig::confirmed()))?;

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

pub enum JoinedSolanaEpochs {
    Range(std::ops::RangeInclusive<u64>),
    Duplicate(u64),
}

impl JoinedSolanaEpochs {
    /// Estimates block time for a skipped slot by searching forward for a
    /// non-skipped slot.
    async fn estimate_block_time_for_skipped_slot(
        solana_client: &RpcClient,
        rate_limiter: &RateLimiter,
        slot: u64,
        current_epoch: u64,
    ) -> Result<i64> {
        const SLOTS_TO_SKIP: u32 = 10;
        const ESTIMATED_SKIP_TIME: i64 = 4;
        const MAX_SLOTS_TO_SEARCH: u32 = 432_000;

        log_warn!(
            "Block time for slot {} in epoch {} not found. Estimating block time",
            slot,
            current_epoch,
        );

        // Start at SLOTS_TO_SKIP since we already know slot 0 failed.
        let mut slots_count = SLOTS_TO_SKIP;

        // Traverse forward from the current slot until we find a block time
        // that is not skipped.
        while slots_count < MAX_SLOTS_TO_SEARCH {
            rate_limiter.acquire_one().await;

            let search_slot = slot + u64::from(slots_count);

            match solana_client.get_block_time(search_slot).await {
                Ok(block_time) => {
                    // Estimate the original slot's block time by subtracting
                    // estimated time.
                    return Ok(block_time
                        - ESTIMATED_SKIP_TIME * i64::from(slots_count) / i64::from(SLOTS_TO_SKIP));
                }
                _ => {
                    log_warn!(
                        "Block time for slot {} in epoch {} not found. Continuing search...",
                        search_slot,
                        current_epoch,
                    );
                }
            }

            slots_count += SLOTS_TO_SKIP;
        }

        bail!(
            "Cannot estimate block time for slot {} in epoch {} after searching {} slots",
            slot,
            current_epoch,
            MAX_SLOTS_TO_SEARCH
        )
    }

    /// Gets block time for a slot, with fallback to estimation if the slot was
    /// skipped.
    async fn get_block_time_with_estimation(
        solana_client: &RpcClient,
        rate_limiter: &RateLimiter,
        slot: u64,
        current_epoch: u64,
    ) -> Result<i64> {
        rate_limiter.acquire_one().await;

        match solana_client.get_block_time(slot).await {
            Ok(block_time) => Ok(block_time),
            Err(e) => match e {
                ClientError {
                    request: _,
                    kind:
                        ClientErrorKind::RpcError(RpcError::RpcResponseError {
                            code:
                                JSON_RPC_SERVER_ERROR_SLOT_SKIPPED
                                | JSON_RPC_SERVER_ERROR_LONG_TERM_STORAGE_SLOT_SKIPPED,
                            message: _,
                            data: _,
                        }),
                } => {
                    Self::estimate_block_time_for_skipped_slot(
                        solana_client,
                        rate_limiter,
                        slot,
                        current_epoch,
                    )
                    .await
                }
                e => bail!(e),
            },
        }
    }

    async fn find_solana_epoch_before_timestamp(
        solana_client: &RpcClient,
        rate_limiter: &RateLimiter,
        initial_solana_epoch: u64,
        initial_last_slot_of_epoch: u64,
        slots_per_epoch: u64,
        target_timestamp: i64,
    ) -> Result<u64> {
        let mut current_epoch = initial_solana_epoch;
        let mut current_last_slot = initial_last_slot_of_epoch;

        // This loop will always terminate.
        loop {
            let last_slot_block_time = Self::get_block_time_with_estimation(
                solana_client,
                rate_limiter,
                current_last_slot,
                current_epoch,
            )
            .await?;

            if last_slot_block_time < target_timestamp {
                return Ok(current_epoch);
            }

            current_epoch -= 1;
            current_last_slot -= slots_per_epoch;
        }
    }

    pub async fn try_new(
        solana_client: &RpcClient,
        dz_ledger_client: &RpcClient,
        target_dz_epoch: u64,
        rate_limiter: &RateLimiter,
    ) -> Result<Self> {
        let current_dz_epoch_info = dz_ledger_client.get_epoch_info().await?;
        ensure!(
            target_dz_epoch < current_dz_epoch_info.epoch,
            "DZ epoch {target_dz_epoch} is not less than the current DZ epoch {}",
            current_dz_epoch_info.epoch
        );

        let dz_epoch_diff = current_dz_epoch_info.epoch - target_dz_epoch;

        let last_slot_of_current_dz_epoch = current_dz_epoch_info.absolute_slot
            - current_dz_epoch_info.slot_index
            + current_dz_epoch_info.slots_in_epoch
            - 1;

        let last_slot_of_target_dz_epoch =
            last_slot_of_current_dz_epoch - (current_dz_epoch_info.slots_in_epoch * dz_epoch_diff);

        let last_dz_block_time = dz_ledger_client
            .get_block_time(last_slot_of_target_dz_epoch)
            .await?;

        let current_solana_epoch_info = solana_client.get_epoch_info().await?;

        let initial_solana_epoch = current_solana_epoch_info.epoch - 1;
        let initial_last_slot_of_solana_epoch =
            current_solana_epoch_info.absolute_slot - current_solana_epoch_info.slot_index - 1;

        // Find the last Solana epoch that ends before the target DZ epoch ends.
        let last_solana_epoch = Self::find_solana_epoch_before_timestamp(
            solana_client,
            rate_limiter,
            initial_solana_epoch,
            initial_last_slot_of_solana_epoch,
            current_solana_epoch_info.slots_in_epoch,
            last_dz_block_time,
        )
        .await?;

        let last_slot_of_previous_dz_epoch =
            last_slot_of_target_dz_epoch - current_dz_epoch_info.slots_in_epoch;

        let previous_dz_block_time = dz_ledger_client
            .get_block_time(last_slot_of_previous_dz_epoch)
            .await?;

        // Calculate the last slot for the last Solana epoch we found.
        let last_slot_of_last_solana_epoch = initial_last_slot_of_solana_epoch
            - (initial_solana_epoch - last_solana_epoch) * current_solana_epoch_info.slots_in_epoch;

        // Find the Solana epoch that ends before the previous DZ epoch ends.
        let solana_epoch_before_previous = Self::find_solana_epoch_before_timestamp(
            solana_client,
            rate_limiter,
            last_solana_epoch,
            last_slot_of_last_solana_epoch,
            current_solana_epoch_info.slots_in_epoch,
            previous_dz_block_time,
        )
        .await?;

        // This epoch could be the same as the last solana epoch, which means
        // the last DZ epoch that determined Solana epochs already accounted for
        // the last Solana epoch.
        if solana_epoch_before_previous == last_solana_epoch {
            Ok(Self::Duplicate(last_solana_epoch))
        } else {
            let first_solana_epoch = solana_epoch_before_previous + 1;

            Ok(Self::Range(first_solana_epoch..=last_solana_epoch))
        }
    }
}
