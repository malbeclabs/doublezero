use std::{
    collections::{BTreeMap, HashMap, HashSet},
    io::Write,
    mem::offset_of,
};

use anyhow::{Context, Result};
use borsh::BorshDeserialize;
use chrono::{DateTime, Datelike, Months, NaiveDate};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_solana_client_tools::{
    account::zero_copy::ZeroCopyAccountOwnedData, rpc::SolanaConnectionOptions,
};
use doublezero_solana_sdk::{
    DISCRIMINATOR_LEN, Discriminator, PrecomputedDiscriminator, environment_2z_token_mint_key,
    environment_usdc_token_mint_key,
    feed_subscription::{
        ID,
        instruction::FeedSubscriptionInstructionData,
        state::{
            FEED_CHAIN_SOLANA, FeedDistribution, FeedDistributionJournal, FeedPublisherRewards,
            find_feed_distribution_journal_address, find_feed_publisher_rewards_address,
        },
    },
};
use futures::stream::{self, StreamExt, TryStreamExt};
use solana_account_decoder_client_types::UiAccountEncoding;
use solana_client::{
    rpc_client::GetConfirmedSignaturesForAddress2Config,
    rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig, RpcTransactionConfig},
    rpc_filter::{Memcmp, RpcFilterType},
};
use solana_commitment_config::CommitmentConfig;
use solana_sdk::{clock::Clock, pubkey::Pubkey, signature::Signature};
use solana_transaction_status_client_types::UiTransactionEncoding;
use tabled::{
    Table, Tabled,
    settings::{Alignment, Style, object::Columns},
};

/*
   doublezero-solana feeds publisher-rewards status --node-id <PUBKEY> [--num-months <N>]

   Every route and payout the oracle sends for a validator names the validator's feed publisher
   rewards address, even before the validator configures it. So that address's transaction
   history shows which months owe the validator rewards, and what each payout sent.
*/

// The most signatures one `getSignaturesForAddress` call returns.
const SIGNATURE_PAGE_LIMIT: usize = 1_000;

const TRANSACTION_FETCH_CONCURRENCY: usize = 8;

// A payout lists the validator's token account last.
const DISTRIBUTE_DESTINATION_ACCOUNT_INDEX: usize = 7;

#[derive(Debug, Args)]
pub struct StatusCommand {
    /// Validator identity to report on.
    #[arg(long)]
    node_id: Pubkey,

    /// How many calendar months before the current one to report.
    #[arg(long, default_value_t = 3)]
    num_months: u32,

    #[command(flatten)]
    connection_options: SolanaConnectionOptions,
}

#[derive(Default)]
struct MonthRewards {
    collected_feed_distribution_keys: Vec<Pubkey>,
    routed_feed_distribution_keys: HashSet<Pubkey>,
    paid_feed_distribution_keys: HashSet<Pubkey>,
    // The raw amount and the mint's decimals.
    paid_by_mint: BTreeMap<Pubkey, (u128, u8)>,
}

#[derive(Tabled)]
struct StatusRow {
    #[tabled(rename = "MONTH")]
    month: String,
    #[tabled(rename = "STATUS")]
    status: &'static str,
    #[tabled(rename = "PAID")]
    paid: String,
}

impl StatusCommand {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        let connection = crate::command::solana_connection(ctx, &self.connection_options);
        let commitment = connection.commitment();

        let feed_publisher_rewards_key = find_feed_publisher_rewards_address(&self.node_id).0;
        let feed_publisher_rewards_account = connection
            .get_account_with_commitment(&feed_publisher_rewards_key, commitment)
            .await
            .with_context(|| {
                format!("reading the feed publisher rewards at {feed_publisher_rewards_key}")
            })?
            .value;
        let feed_publisher_rewards = feed_publisher_rewards_account
            .map(|account| {
                ZeroCopyAccountOwnedData::<FeedPublisherRewards>::from_account(&account)
                    .with_context(|| {
                        format!(
                            "the feed publisher rewards at {feed_publisher_rewards_key} is malformed"
                        )
                    })
            })
            .transpose()?;

        let network_env = connection
            .try_network_environment()
            .await
            .context("detecting the network environment")?;
        let mint_symbols = HashMap::from([
            (environment_2z_token_mint_key(network_env), "2Z"),
            (environment_usdc_token_mint_key(network_env), "USDC"),
            (spl_token_interface::native_mint::ID, "WSOL"),
        ]);
        let mint_symbol = |mint_key: &Pubkey| {
            mint_symbols
                .get(mint_key)
                .map_or_else(|| mint_key.to_string(), ToString::to_string)
        };

        writeln!(out, "Node ID:        {}", self.node_id)?;
        match &feed_publisher_rewards {
            Some(feed_publisher_rewards) => {
                writeln!(
                    out,
                    "Rewards owner:  {}",
                    feed_publisher_rewards.rewards_token_owner_key
                )?;
                writeln!(
                    out,
                    "Rewards mint:   {}",
                    mint_symbol(&feed_publisher_rewards.rewards_token_mint_key)
                )?;
            }
            None => {
                writeln!(out, "Rewards owner:  not configured")?;
                writeln!(
                    out,
                    "\nThe oracle cannot pay this validator until it sets a rewards wallet. Run \
                     `doublezero-solana feeds publisher-rewards configure` to set one."
                )?;
            }
        }

        let unix_timestamp = connection
            .try_fetch_sysvar::<Clock>()
            .await
            .context("reading the cluster clock")?
            .unix_timestamp;
        let now = DateTime::from_timestamp(unix_timestamp, 0)
            .with_context(|| format!("the cluster clock reads {unix_timestamp}"))?;
        let current_month_start = NaiveDate::from_ymd_opt(now.year(), now.month(), 1)
            .context("computing the start of the current month")?;
        let month_starts = (1..=self.num_months)
            .map(|months_back| {
                current_month_start
                    .checked_sub_months(Months::new(months_back))
                    .context("computing a reported month")
            })
            .collect::<Result<Vec<_>>>()?;
        let Some(oldest_month_start) = month_starts.last() else {
            return Ok(());
        };
        let window_start = oldest_month_start
            .and_time(Default::default())
            .and_utc()
            .timestamp();

        let mut months = month_starts
            .iter()
            .map(|month_start| {
                let year = u16::try_from(month_start.year()).context("year out of range")?;
                let month = u8::try_from(month_start.month()).context("month out of range")?;
                Ok(((year, month), MonthRewards::default()))
            })
            .collect::<Result<BTreeMap<_, _>>>()?;

        let feed_distributions = connection
            .get_program_accounts_with_config(
                &ID,
                RpcProgramAccountsConfig {
                    filters: Some(vec![
                        RpcFilterType::Memcmp(Memcmp::new_raw_bytes(
                            0,
                            FeedDistribution::discriminator_slice().to_vec(),
                        )),
                        RpcFilterType::Memcmp(Memcmp::new_raw_bytes(
                            DISCRIMINATOR_LEN + offset_of!(FeedDistribution, feed_chain),
                            FEED_CHAIN_SOLANA.to_le_bytes().to_vec(),
                        )),
                    ]),
                    account_config: RpcAccountInfoConfig {
                        encoding: Some(UiAccountEncoding::Base64),
                        ..Default::default()
                    },
                    ..Default::default()
                },
            )
            .await
            .context("listing the Solana feed months")?;

        let mut month_by_feed_distribution_key = HashMap::new();
        for (feed_distribution_key, account) in feed_distributions {
            let Some(feed_distribution) =
                ZeroCopyAccountOwnedData::<FeedDistribution>::from_account(&account)
            else {
                continue;
            };
            let year_month = (feed_distribution.year, feed_distribution.month);
            let Some(month_rewards) = months.get_mut(&year_month) else {
                continue;
            };
            month_by_feed_distribution_key.insert(feed_distribution_key, year_month);
            if feed_distribution.collected_usdc_amount != 0 {
                month_rewards
                    .collected_feed_distribution_keys
                    .push(feed_distribution_key);
            }
        }

        let collected_feed_distribution_keys = months
            .values()
            .flat_map(|month_rewards| month_rewards.collected_feed_distribution_keys.clone())
            .collect::<Vec<_>>();
        let journal_keys = collected_feed_distribution_keys
            .iter()
            .map(|feed_distribution_key| {
                find_feed_distribution_journal_address(feed_distribution_key).0
            })
            .collect::<Vec<_>>();
        let journals = connection
            .try_fetch_multiple_zero_copy_data::<FeedDistributionJournal>(&journal_keys)
            .await
            .context("reading the feed distribution journals")?;
        let is_routed_by_feed_distribution_key = collected_feed_distribution_keys
            .into_iter()
            .zip(journals)
            .map(|(feed_distribution_key, journal)| {
                let is_routed =
                    journal.is_some_and(|journal| journal.is_every_publisher_leaf_routed());
                (feed_distribution_key, is_routed)
            })
            .collect::<HashMap<_, _>>();

        let mut signatures = Vec::new();
        let mut before = None;
        loop {
            let page = connection
                .get_signatures_for_address_with_config(
                    &feed_publisher_rewards_key,
                    GetConfirmedSignaturesForAddress2Config {
                        before,
                        limit: Some(SIGNATURE_PAGE_LIMIT),
                        commitment: Some(CommitmentConfig::confirmed()),
                        ..Default::default()
                    },
                )
                .await
                .with_context(|| {
                    format!("reading the transaction history of {feed_publisher_rewards_key}")
                })?;
            let Some(last) = page.last() else {
                break;
            };
            let is_last_page = page.len() < SIGNATURE_PAGE_LIMIT
                || last
                    .block_time
                    .is_some_and(|block_time| block_time < window_start);
            before = Some(
                last.signature
                    .parse::<Signature>()
                    .context("parsing a history signature")?,
            );
            for signature_info in page {
                let is_in_window = signature_info
                    .block_time
                    .is_none_or(|block_time| block_time >= window_start);
                if signature_info.err.is_none() && is_in_window {
                    signatures.push(
                        signature_info
                            .signature
                            .parse::<Signature>()
                            .context("parsing a history signature")?,
                    );
                }
            }
            if is_last_page {
                break;
            }
        }

        let transaction_config = RpcTransactionConfig {
            encoding: Some(UiTransactionEncoding::Base64),
            commitment: Some(CommitmentConfig::confirmed()),
            max_supported_transaction_version: Some(0),
        };
        let transactions = stream::iter(signatures)
            .map(|signature| {
                let connection = &connection;
                async move {
                    connection
                        .get_transaction_with_config(&signature, transaction_config)
                        .await
                        .with_context(|| format!("reading transaction {signature}"))
                }
            })
            .buffered(TRANSACTION_FETCH_CONCURRENCY)
            .try_collect::<Vec<_>>()
            .await?;

        for response in transactions {
            let Some(transaction) = response.transaction.transaction.decode() else {
                continue;
            };
            let account_keys = transaction.message.static_account_keys();
            let token_balances = response.transaction.meta.map(|meta| {
                (
                    Option::<Vec<_>>::from(meta.pre_token_balances).unwrap_or_default(),
                    Option::<Vec<_>>::from(meta.post_token_balances).unwrap_or_default(),
                )
            });

            for instruction in transaction.message.instructions() {
                if account_keys.get(usize::from(instruction.program_id_index)) != Some(&ID) {
                    continue;
                }
                let mut data = instruction.data.as_slice();
                let Ok(discriminator) = Discriminator::<DISCRIMINATOR_LEN>::deserialize(&mut data)
                else {
                    continue;
                };
                if Pubkey::deserialize(&mut data).ok() != Some(self.node_id) {
                    continue;
                }
                let account_key_at = |position: usize| {
                    instruction
                        .accounts
                        .get(position)
                        .and_then(|index| account_keys.get(usize::from(*index)))
                };
                let Some(feed_distribution_key) = account_key_at(1) else {
                    continue;
                };
                let Some(month_rewards) = month_by_feed_distribution_key
                    .get(feed_distribution_key)
                    .and_then(|year_month| months.get_mut(year_month))
                else {
                    continue;
                };

                match discriminator {
                    FeedSubscriptionInstructionData::ROUTE_PUBLISHER_REWARDS => {
                        month_rewards
                            .routed_feed_distribution_keys
                            .insert(*feed_distribution_key);
                    }
                    FeedSubscriptionInstructionData::DISTRIBUTE_SOLANA_PUBLISHER_REWARDS => {
                        month_rewards
                            .paid_feed_distribution_keys
                            .insert(*feed_distribution_key);
                        let Some((pre_balances, post_balances)) = &token_balances else {
                            continue;
                        };
                        let Some(destination_index) = instruction
                            .accounts
                            .get(DISTRIBUTE_DESTINATION_ACCOUNT_INDEX)
                        else {
                            continue;
                        };
                        let Some(post_balance) = post_balances
                            .iter()
                            .find(|balance| balance.account_index == *destination_index)
                        else {
                            continue;
                        };
                        let post_amount = post_balance
                            .ui_token_amount
                            .amount
                            .parse::<u128>()
                            .context("parsing a payout balance")?;
                        let pre_amount = pre_balances
                            .iter()
                            .find(|balance| balance.account_index == *destination_index)
                            .map(|balance| balance.ui_token_amount.amount.parse::<u128>())
                            .transpose()
                            .context("parsing a payout balance")?
                            .unwrap_or_default();
                        let mint_key = post_balance
                            .mint
                            .parse::<Pubkey>()
                            .context("parsing a payout mint")?;
                        let paid = month_rewards
                            .paid_by_mint
                            .entry(mint_key)
                            .or_insert((0, post_balance.ui_token_amount.decimals));
                        paid.0 += post_amount.saturating_sub(pre_amount);
                    }
                    _ => {}
                }
            }
        }

        let rows = months
            .iter()
            .rev()
            .map(|((year, month), month_rewards)| {
                let has_unpaid = month_rewards
                    .routed_feed_distribution_keys
                    .difference(&month_rewards.paid_feed_distribution_keys)
                    .next()
                    .is_some();
                let is_month_routed = month_rewards
                    .collected_feed_distribution_keys
                    .iter()
                    .all(|key| is_routed_by_feed_distribution_key.get(key) == Some(&true));
                let status = if has_unpaid {
                    "unpaid"
                } else if month_rewards.collected_feed_distribution_keys.is_empty() {
                    "no rewards"
                } else if !is_month_routed {
                    "not ready"
                } else if month_rewards.paid_feed_distribution_keys.is_empty() {
                    "no rewards"
                } else {
                    "paid"
                };
                let paid = if month_rewards.paid_by_mint.is_empty() {
                    "-".to_string()
                } else {
                    month_rewards
                        .paid_by_mint
                        .iter()
                        .map(|(mint_key, (amount, decimals))| {
                            format!(
                                "{} {}",
                                format_token_amount(*amount, *decimals),
                                mint_symbol(mint_key)
                            )
                        })
                        .collect::<Vec<_>>()
                        .join(", ")
                };
                StatusRow {
                    month: format!("{year}-{month:02}"),
                    status,
                    paid,
                }
            })
            .collect::<Vec<_>>();

        let mut table = Table::new(rows);
        table.with(Style::markdown());
        table.modify(Columns::one(2), Alignment::right());
        writeln!(out, "\n{table}")?;
        writeln!(
            out,
            "\nStatus:\n  \
             paid        the oracle paid this month's rewards to the rewards wallet\n  \
             unpaid      this month has rewards that the oracle has not paid yet\n  \
             not ready   the oracle has not finished this month yet\n  \
             no rewards  this month has no rewards for this validator"
        )?;

        Ok(())
    }
}

// Truncates to three decimal places and drops trailing zeros.
fn format_token_amount(amount: u128, decimals: u8) -> String {
    let scale = 10_u128.pow(u32::from(decimals));
    let whole = amount / scale;
    let thousandths = amount % scale * 1_000 / scale;
    if thousandths == 0 {
        return whole.to_string();
    }
    let fraction = format!("{thousandths:03}");
    format!("{whole}.{}", fraction.trim_end_matches('0'))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_format_token_amount_truncates_to_three_decimals() {
        assert_eq!(format_token_amount(357_780_938_287, 8), "3577.809");
        assert_eq!(format_token_amount(1_500_000, 6), "1.5");
        assert_eq!(format_token_amount(2_000_000_000, 9), "2");
        assert_eq!(format_token_amount(400, 6), "0");
        assert_eq!(format_token_amount(42, 0), "42");
    }
}
