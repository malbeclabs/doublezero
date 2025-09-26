use std::sync::Arc;

use anyhow::{bail, Result};
use clap::Parser;
use doublezero_solana_client_tools::{
    log_error, log_info, log_warn,
    rpc::{SolanaConnection, SolanaConnectionOptions},
};
use leaky_bucket::RateLimiter;
use solana_client::{
    client_error::{ClientError, ClientErrorKind},
    rpc_config::RpcBlockConfig,
    rpc_custom_error::{
        JSON_RPC_SERVER_ERROR_LONG_TERM_STORAGE_SLOT_SKIPPED, JSON_RPC_SERVER_ERROR_SLOT_SKIPPED,
    },
    rpc_request::RpcError,
};
use solana_commitment_config::CommitmentConfig;
use solana_reward_info::RewardType;
use solana_transaction_status_client_types::TransactionDetails;

#[tokio::main]
async fn main() -> Result<()> {
    GetBlocksExampleApp::parse().into_execute().await
}

#[derive(Debug, Parser)]
#[command(term_width = 0)]
#[command(version = option_env!("BUILD_VERSION").unwrap_or(env!("CARGO_PKG_VERSION")))]
#[command(about = "Get blocks example", long_about = None)]
struct GetBlocksExampleApp {
    #[arg(long)]
    first_slot: Option<u64>,

    #[arg(long)]
    last_slot: Option<u64>,

    #[arg(long)]
    rate_limit: Option<usize>,

    #[arg(long)]
    debug: bool,

    #[command(flatten)]
    solana_connection_options: SolanaConnectionOptions,
}

#[derive(Debug, Default)]
struct BlockInfo {
    i: usize,
    slot: u64,
    rewards: u64,
}

impl GetBlocksExampleApp {
    async fn into_execute(self) -> Result<()> {
        #[cfg(feature = "tracing")]
        {
            use tracing_subscriber::FmtSubscriber;

            let subscriber = FmtSubscriber::builder()
                .with_max_level(tracing::Level::DEBUG)
                .finish();

            tracing::subscriber::set_global_default(subscriber).unwrap();
        }

        let Self {
            first_slot,
            last_slot,
            rate_limit,
            debug,
            solana_connection_options,
        } = self;

        let connection = SolanaConnection::try_from(solana_connection_options)?;
        let rpc_client = connection.rpc_client;

        let last_slot = match last_slot {
            Some(last_slot) => last_slot,
            None => {
                let epoch_info = rpc_client.get_epoch_info().await?;
                epoch_info.absolute_slot
            }
        };

        let first_slot = first_slot.unwrap_or(last_slot - 10);
        if first_slot > last_slot {
            bail!("First slot must be less than or equal to last slot");
        }

        let rate_limit = rate_limit.unwrap_or(5);
        let rate_limiter = Arc::new(
            RateLimiter::builder()
                .max(rate_limit)
                .initial(rate_limit)
                .refill(rate_limit)
                .interval(std::time::Duration::from_secs(1))
                .build(),
        );

        let rpc_block_config = RpcBlockConfig {
            transaction_details: Some(TransactionDetails::None),
            commitment: Some(CommitmentConfig::confirmed()),
            ..Default::default()
        };

        let rpc_client = Arc::new(rpc_client);

        let mut tasks = vec![];
        for (i, slot) in (first_slot..=last_slot).enumerate() {
            let rate_limiter = Arc::clone(&rate_limiter);
            rate_limiter.acquire_one().await;

            let rpc_client = Arc::clone(&rpc_client);

            let task = tokio::spawn(async move {
                log_info!("Fetching i={i}, slot={slot}");

                let mut block = None;

                while block.is_none() {
                    match rpc_client
                        .get_block_with_config(slot, rpc_block_config)
                        .await
                    {
                        Ok(confirmed_block) => {
                            block.replace(confirmed_block);
                        }
                        Err(ClientError {
                            request: _,
                            kind:
                                ClientErrorKind::RpcError(RpcError::RpcResponseError {
                                    code:
                                        JSON_RPC_SERVER_ERROR_SLOT_SKIPPED
                                        | JSON_RPC_SERVER_ERROR_LONG_TERM_STORAGE_SLOT_SKIPPED,
                                    message: _,
                                    data: _,
                                }),
                        }) => {
                            return Some(BlockInfo {
                                i,
                                slot,
                                rewards: 0,
                            });
                        }
                        Err(ClientError {
                            request: _,
                            kind: ClientErrorKind::Reqwest(_),
                        }) => {
                            if debug {
                                log_warn!("Reqwest error at slot={slot}: Retry after 1 second");
                            }
                            tokio::time::sleep(std::time::Duration::from_secs(1)).await;
                            rate_limiter.acquire_one().await;
                        }
                        Err(e) => {
                            log_error!("Failed to get block {slot}: {e:?}");
                            return None;
                        }
                    };
                }

                let rewards = block
                    .unwrap()
                    .rewards
                    .unwrap()
                    .iter()
                    .filter_map(|reward| {
                        if reward.reward_type.unwrap() == RewardType::Fee {
                            u64::try_from(reward.lamports).ok()
                        } else {
                            None
                        }
                    })
                    .sum::<u64>();

                Some(BlockInfo { i, slot, rewards })
            });

            tasks.push(task);
        }

        let mut block_infos = Vec::new();
        for task in tasks {
            let block_info = match task.await? {
                Some(block_info) => block_info,
                None => continue,
            };
            block_infos.push(block_info);
        }

        block_infos.sort_by_key(|info| info.i);

        log_info!("Block infos:");
        for info in block_infos {
            log_info!("i={}, slot={}, rewards={}", info.i, info.slot, info.rewards);
        }

        Ok(())
    }
}
