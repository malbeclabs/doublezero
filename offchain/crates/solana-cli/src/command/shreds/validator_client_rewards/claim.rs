use anyhow::{Context, Result, bail, ensure};
use clap::Args;
use doublezero_solana_client_tools::{
    payer::{SolanaPayerOptions, TransactionOutcome, Wallet},
    rpc::try_fetch_multiple_accounts,
};
use doublezero_solana_sdk::{
    shred_subscription::{
        ID,
        instruction::{
            ClaimHoldingId, ShredSubscriptionInstructionData,
            account::ClaimValidatorClientRewardsAccounts,
        },
        state::{
            find_claim_holding_address, find_program_config_address,
            find_validator_client_rewards_address, parse_program_config_shred_oracle_key,
            parse_validator_client_rewards,
        },
    },
    try_build_instruction,
};
use solana_sdk::{
    account::Account, commitment_config::CommitmentConfig,
    compute_budget::ComputeBudgetInstruction, instruction::AccountMeta, program_pack::Pack,
    pubkey::Pubkey,
};
use spl_associated_token_account_interface::address::get_associated_token_address;

/*
   doublezero-solana shreds validator-client-rewards claim \
       --client-id <ID> --rewards-token-mint <PUBKEY> \
       [--subscription-epoch <EPOCH> ...] \
       [--destination-token-account <PUBKEY>]

   When no --subscription-epoch is given, every outstanding holding for the
   client and mint is discovered and claimed across as many transactions as
   needed (up to MAX_CLAIM_EPOCHS_PER_TX holdings per tx).
*/

#[derive(Debug, Args)]
pub struct ClaimCommand {
    /// Validator client ID.
    #[arg(long)]
    pub client_id: u16,
    /// Token mint that holdings are denominated in.
    #[arg(long)]
    pub rewards_token_mint: Pubkey,
    /// Subscription epochs to claim. When omitted, every outstanding holding
    /// for this client and mint is discovered and claimed.
    #[arg(long = "subscription-epoch", num_args = 1..)]
    pub subscription_epochs: Vec<u64>,
    /// Destination token account. Defaults to ATA(manager, rewards_token_mint).
    #[arg(long)]
    pub destination_token_account: Option<Pubkey>,
    #[command(flatten)]
    pub solana_payer_options: SolanaPayerOptions,
}

pub(crate) fn resolve_destination(
    manager: &Pubkey,
    mint: &Pubkey,
    override_destination: Option<Pubkey>,
) -> Pubkey {
    override_destination.unwrap_or_else(|| get_associated_token_address(manager, mint))
}

pub(crate) fn validate_manager(
    wallet: &Pubkey,
    validator_client_rewards_manager: &Pubkey,
) -> Result<()> {
    ensure!(
        wallet == validator_client_rewards_manager,
        "manager mismatch: wallet is {wallet}, validator client rewards manager is {validator_client_rewards_manager}"
    );
    Ok(())
}

// Upper bound on epochs per claim tx. Each `ClaimHoldingId` adds 9 bytes of
// instruction data and the holding account adds 32 bytes to the account list,
// so beyond ~20 the tx blows past the 1232-byte packet limit. 16 is a
// conservative cap that leaves room for the destination/rent/program-config
// accounts and the CheckCliVersion ix.
pub(crate) const MAX_CLAIM_EPOCHS_PER_TX: usize = 16;

// How far back (in subscription epochs) auto-discovery probes from the current
// epoch. Holdings older than the on-chain abandonment window are swept, so this
// covers every holding that can still exist.
const MAX_DISCOVERY_LOOKBACK: u64 = 90;

struct HoldingToClaim {
    epoch: u64,
    bump_seed: u8,
    holding_pda: Pubkey,
    pre_balance: u64,
}

/// Decode a fetched account as a claim holding for `mint`, returning its
/// balance.
fn holding_balance(account: Option<&Account>, mint: &Pubkey) -> Option<u64> {
    let account = account?;
    if account.owner != spl_token_interface::ID {
        return None;
    }
    let token = spl_token_interface::state::Account::unpack(&account.data).ok()?;
    (token.mint == *mint).then_some(token.amount)
}

impl ClaimCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let wallet = Wallet::try_new(self.solana_payer_options, None)?;
        let wallet_key = wallet.pubkey();

        let validator_client_rewards_key = find_validator_client_rewards_address(self.client_id).0;
        let program_config_key = find_program_config_address().0;

        // Single fetch: validator client rewards, program config.
        let accounts = try_fetch_multiple_accounts(
            &wallet.connection,
            &[validator_client_rewards_key, program_config_key],
        )
        .await
        .context("fetching validator client rewards + program config")?;

        let validator_client_rewards_account =
            accounts.first().and_then(|a| a.as_ref()).with_context(|| {
                format!(
                    "validator client rewards not initialized for client-id {} (PDA {validator_client_rewards_key})",
                    self.client_id
                )
            })?;
        let validator_client_rewards_info = parse_validator_client_rewards(
            &validator_client_rewards_account.data,
        )
        .with_context(|| {
            format!("failed to parse ValidatorClientRewards at {validator_client_rewards_key}")
        })?;
        validate_manager(&wallet_key, &validator_client_rewards_info.manager_key)?;

        let config_account = accounts
            .get(1)
            .and_then(|a| a.as_ref())
            .with_context(|| format!("ProgramConfig {program_config_key} not found onchain"))?;
        let rent_beneficiary = parse_program_config_shred_oracle_key(&config_account.data)
            .context("failed to parse shred_oracle_key from ProgramConfig")?;

        // Resolve the set of holdings to claim: explicit epochs (validated), or
        // every outstanding holding discovered on chain.
        let holdings = if self.subscription_epochs.is_empty() {
            let target = validator_client_rewards_info.claim_holding_count as usize;
            if target == 0 {
                println!(
                    "No outstanding claim holdings for client_id {} (claim_holding_count is 0).",
                    self.client_id
                );
                return Ok(());
            }
            // The shred-subscription program stamps `current_subscription_epoch`
            // from the Clock of the cluster it runs on, which is exactly the
            // cluster `wallet.connection` talks to, so the live epoch there is
            // the discovery ceiling.
            let current_epoch = wallet
                .connection
                .get_epoch_info()
                .await
                .context("fetching current epoch")?
                .epoch;
            let discovered = discover_holdings(
                &wallet,
                &validator_client_rewards_key,
                &self.rewards_token_mint,
                current_epoch,
            )
            .await?;
            if discovered.is_empty() {
                println!(
                    "No claim holdings for client_id {} found for mint {} within the last {MAX_DISCOVERY_LOOKBACK} epochs.",
                    self.client_id, self.rewards_token_mint,
                );
                return Ok(());
            }
            if discovered.len() < target {
                eprintln!(
                    "warning: found {} holding(s) for mint {} but claim_holding_count is {target}; \
                     the remainder may be denominated in another mint or older than the \
                     {MAX_DISCOVERY_LOOKBACK}-epoch discovery window.",
                    discovered.len(),
                    self.rewards_token_mint,
                );
            }
            println!(
                "Discovered {} outstanding holding(s) for client_id {} (mint {}).",
                discovered.len(),
                self.client_id,
                self.rewards_token_mint,
            );
            discovered
        } else {
            validate_explicit_holdings(
                &wallet,
                &validator_client_rewards_key,
                &self.rewards_token_mint,
                &self.subscription_epochs,
            )
            .await?
        };

        if holdings.is_empty() {
            println!(
                "Nothing to claim for client_id {} (mint {}); no valid holdings.",
                self.client_id, self.rewards_token_mint,
            );
            return Ok(());
        }

        // Resolve destination token account and validate it.
        let destination = resolve_destination(
            &wallet_key,
            &self.rewards_token_mint,
            self.destination_token_account,
        );
        let destination_account = wallet
            .connection
            .get_account_with_commitment(&destination, CommitmentConfig::confirmed())
            .await
            .with_context(|| format!("fetching destination token account {destination}"))?
            .value
            .with_context(|| {
                format!(
                    "destination token account {destination} does not exist. \
                     Run: `spl-token create-account --owner {wallet_key} {} --fee-payer {wallet_key}`",
                    self.rewards_token_mint
                )
            })?;
        if destination_account.owner != spl_token_interface::ID {
            bail!(
                "destination {destination} is not an SPL token account (owner = {})",
                destination_account.owner
            );
        }
        let destination_token =
            spl_token_interface::state::Account::unpack(&destination_account.data)
                .with_context(|| format!("unpacking destination token account {destination}"))?;
        if destination_token.mint != self.rewards_token_mint {
            bail!(
                "destination {destination} mint mismatch: expected {}, found {}",
                self.rewards_token_mint,
                destination_token.mint
            );
        }

        let total_holdings = holdings.len();
        let total_pre_balance = holdings.iter().fold(0u64, |total, holding| {
            total.saturating_add(holding.pre_balance)
        });
        let batches = holdings.chunks(MAX_CLAIM_EPOCHS_PER_TX).collect::<Vec<_>>();
        let batch_count = batches.len();

        println!(
            "Shred subscription - Claim Validator Client Rewards \
             (client_id={}, mint={}, holdings={total_holdings}, transactions={batch_count})",
            self.client_id, self.rewards_token_mint,
        );
        println!("  manager       : {wallet_key}");
        println!("  destination   : {destination}");
        println!("  rent recovers : {rent_beneficiary}");

        // Submit one transaction per batch of up to MAX_CLAIM_EPOCHS_PER_TX
        // holdings. Batches are independent, so a later failure does not undo an
        // earlier executed batch.
        let mut executed_holdings = 0;
        let mut last_executed = false;
        for (batch_index, batch) in batches.into_iter().enumerate() {
            let epochs = batch
                .iter()
                .map(|holding| holding.epoch)
                .collect::<Vec<_>>();
            let claim_holding_ids = batch
                .iter()
                .map(|holding| ClaimHoldingId {
                    subscription_epoch: holding.epoch,
                    bump_seed: holding.bump_seed,
                })
                .collect::<Vec<_>>();

            let claim_accounts = ClaimValidatorClientRewardsAccounts::new(
                self.client_id,
                &wallet_key,
                &destination,
                &rent_beneficiary,
                &self.rewards_token_mint,
                &epochs,
            );
            let metas: Vec<AccountMeta> = claim_accounts.into();
            let ix = try_build_instruction(
                &ID,
                metas,
                &ShredSubscriptionInstructionData::ClaimValidatorClientRewards(claim_holding_ids),
            )?;

            let mut instructions = vec![super::super::build_check_cli_version_instruction()?, ix];
            // ~30k CU per holding (token transfer + close + state decrement),
            // plus the check-cli-version ix.
            let compute_unit_limit = 30_000u32.saturating_mul(epochs.len() as u32 + 1);
            instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(
                compute_unit_limit,
            ));
            if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
                instructions.push(compute_unit_price_ix.clone());
            }

            if batch_count > 1 {
                println!(
                    "\nTransaction {}/{batch_count}: {} holding(s), epochs {epochs:?}",
                    batch_index + 1,
                    batch.len(),
                );
            }

            let transaction = wallet.new_transaction(&instructions).await?;
            let tx_outcome = wallet.send_or_simulate_transaction(&transaction).await?;

            if let TransactionOutcome::Executed(tx_sig) = tx_outcome {
                executed_holdings += batch.len();
                last_executed = true;
                println!("Claimed: {tx_sig}");
                // The on-chain handler transfers the full balance of each
                // holding, but these balances were read pre-tx — a top-up
                // between the read and the claim makes the actual drained amount
                // higher. Diff the destination balance before/after for the
                // authoritative number.
                for holding in batch {
                    println!(
                        "  epoch {}: {} from {} (pre-claim)",
                        holding.epoch, holding.pre_balance, holding.holding_pda,
                    );
                }
                wallet.print_verbose_output(&[tx_sig]).await?;
            }
        }

        if last_executed {
            println!(
                "\nPre-claim total: {total_pre_balance} ({executed_holdings}/{total_holdings} holding(s) claimed across {batch_count} transaction(s))."
            );

            // Re-fetch the validator client rewards account to report the
            // post-tx claim_holding_count.
            let post_count = match wallet
                .connection
                .get_account_with_commitment(
                    &validator_client_rewards_key,
                    CommitmentConfig::confirmed(),
                )
                .await
            {
                Ok(response) => response.value.and_then(|account| {
                    parse_validator_client_rewards(&account.data)
                        .map(|info| info.claim_holding_count)
                }),
                Err(err) => {
                    eprintln!(
                        "warning: post-claim validator client rewards re-fetch failed: {err}"
                    );
                    None
                }
            };
            match post_count {
                Some(count) => println!("Remaining claim holding count: {count}"),
                None => println!("Remaining claim holding count: (unavailable)"),
            }
        }

        Ok(())
    }
}

/// Discover every outstanding claim holding for `validator_client_rewards_key`/`mint`
/// by probing every holding PDA in
/// `[ceiling_epoch - MAX_DISCOVERY_LOOKBACK, ceiling_epoch]`. Returns the
/// holdings that exist, sorted by epoch.
async fn discover_holdings(
    wallet: &Wallet,
    validator_client_rewards_key: &Pubkey,
    mint: &Pubkey,
    ceiling_epoch: u64,
) -> Result<Vec<HoldingToClaim>> {
    let floor = ceiling_epoch.saturating_sub(MAX_DISCOVERY_LOOKBACK);
    let derived = (floor..=ceiling_epoch)
        .map(|epoch| {
            let (pda, bump) = find_claim_holding_address(validator_client_rewards_key, epoch, mint);
            (epoch, pda, bump)
        })
        .collect::<Vec<_>>();
    let keys = derived.iter().map(|(_, pda, _)| *pda).collect::<Vec<_>>();
    let probed = try_fetch_multiple_accounts(&wallet.connection, &keys)
        .await
        .context("probing claim holdings")?;
    let mut found = Vec::new();
    for ((epoch, pda, bump), account) in derived.into_iter().zip(probed) {
        if let Some(pre_balance) = holding_balance(account.as_ref(), mint) {
            found.push(HoldingToClaim {
                epoch,
                bump_seed: bump,
                holding_pda: pda,
                pre_balance,
            });
        }
    }
    found.sort_unstable_by_key(|holding| holding.epoch);
    Ok(found)
}

/// Resolve an explicit set of subscription epochs into claimable holdings.
async fn validate_explicit_holdings(
    wallet: &Wallet,
    validator_client_rewards_key: &Pubkey,
    mint: &Pubkey,
    epochs: &[u64],
) -> Result<Vec<HoldingToClaim>> {
    let mut epochs = epochs.to_vec();
    epochs.sort_unstable();
    epochs.dedup();

    let derived = epochs
        .iter()
        .map(|&epoch| {
            let (pda, bump) = find_claim_holding_address(validator_client_rewards_key, epoch, mint);
            (epoch, pda, bump)
        })
        .collect::<Vec<_>>();

    let keys = derived.iter().map(|(_, pda, _)| *pda).collect::<Vec<_>>();
    let accounts = try_fetch_multiple_accounts(&wallet.connection, &keys)
        .await
        .context("fetching claim holdings")?;

    let mut holdings = Vec::new();
    for ((epoch, pda, bump), maybe_account) in derived.iter().zip(accounts) {
        match maybe_account.as_ref() {
            None => eprintln!(
                "warning: epoch {epoch} holding {pda} is not initialized; skipping. \
                 Run `shreds validator-client-rewards init-holding ...` to create it."
            ),
            Some(account) if account.owner != spl_token_interface::ID => eprintln!(
                "warning: epoch {epoch} holding {pda} is not an SPL token account (owner {}); skipping.",
                account.owner
            ),
            Some(account) => match spl_token_interface::state::Account::unpack(&account.data) {
                Ok(token) if token.mint != *mint => eprintln!(
                    "warning: epoch {epoch} holding {pda} is for mint {} (expected {mint}); skipping.",
                    token.mint
                ),
                Ok(token) => {
                    if token.amount == 0 {
                        eprintln!(
                            "warning: epoch {epoch} holding has 0 balance; will still close and recover rent."
                        );
                    }
                    holdings.push(HoldingToClaim {
                        epoch: *epoch,
                        bump_seed: *bump,
                        holding_pda: *pda,
                        pre_balance: token.amount,
                    });
                }
                Err(err) => eprintln!(
                    "warning: epoch {epoch} holding {pda} failed to unpack ({err}); skipping."
                ),
            },
        }
    }
    Ok(holdings)
}

#[cfg(test)]
mod tests {
    use clap::Parser;

    use super::*;

    #[derive(Parser)]
    struct Cli {
        #[command(flatten)]
        cmd: ClaimCommand,
    }

    #[test]
    fn test_parses_required_args_with_implicit_destination() {
        let mint = Pubkey::new_unique();
        let cli = Cli::try_parse_from([
            "test",
            "--client-id",
            "7",
            "--rewards-token-mint",
            &mint.to_string(),
            "--subscription-epoch",
            "100",
        ])
        .unwrap();
        assert_eq!(cli.cmd.client_id, 7);
        assert_eq!(cli.cmd.rewards_token_mint, mint);
        assert_eq!(cli.cmd.subscription_epochs, vec![100]);
        assert!(cli.cmd.destination_token_account.is_none());
    }

    #[test]
    fn test_parses_explicit_destination() {
        let mint = Pubkey::new_unique();
        let destination = Pubkey::new_unique();
        let cli = Cli::try_parse_from([
            "test",
            "--client-id",
            "7",
            "--rewards-token-mint",
            &mint.to_string(),
            "--subscription-epoch",
            "100",
            "--destination-token-account",
            &destination.to_string(),
        ])
        .unwrap();
        assert_eq!(cli.cmd.destination_token_account, Some(destination));
    }

    #[test]
    fn test_resolve_destination_uses_override_when_provided() {
        let manager = Pubkey::new_unique();
        let mint = Pubkey::new_unique();
        let override_destination = Pubkey::new_unique();
        assert_eq!(
            resolve_destination(&manager, &mint, Some(override_destination)),
            override_destination
        );
    }

    #[test]
    fn test_resolve_destination_defaults_to_ata() {
        let manager = Pubkey::new_unique();
        let mint = Pubkey::new_unique();
        let expected = get_associated_token_address(&manager, &mint);
        assert_eq!(resolve_destination(&manager, &mint, None), expected);
    }

    #[test]
    fn test_validate_manager_matches() {
        let wallet = Pubkey::new_unique();
        assert!(validate_manager(&wallet, &wallet).is_ok());
    }

    #[test]
    fn test_validate_manager_mismatch() {
        let wallet = Pubkey::new_unique();
        let manager = Pubkey::new_unique();
        let err = validate_manager(&wallet, &manager).unwrap_err();
        let message = format!("{err}");
        assert!(message.contains("manager mismatch"));
        assert!(message.contains(&wallet.to_string()));
        assert!(message.contains(&manager.to_string()));
    }

    #[test]
    fn test_allows_missing_subscription_epoch() {
        // Omitting --subscription-epoch is valid: the command discovers and
        // claims every outstanding holding for the client and mint.
        let mint = Pubkey::new_unique();
        let cli = Cli::try_parse_from([
            "test",
            "--client-id",
            "7",
            "--rewards-token-mint",
            &mint.to_string(),
        ])
        .unwrap();
        assert!(cli.cmd.subscription_epochs.is_empty());
    }
}
