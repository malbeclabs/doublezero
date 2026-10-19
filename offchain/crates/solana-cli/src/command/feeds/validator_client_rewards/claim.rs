use std::{io::Write, num::NonZeroUsize};

use anyhow::{Context, Result, bail, ensure};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_solana_client_tools::{
    account::zero_copy::ZeroCopyAccountOwnedData,
    payer::{TransactionOutcome, Wallet},
    rpc::try_fetch_multiple_accounts,
    squads::{OptionalSquadsArgs, try_write_vault_transaction, vault_transaction_packing_budget},
    transaction::MAX_TRANSACTION_SIZE,
};
use doublezero_solana_sdk::{
    PrecomputedDiscriminator, environment_2z_token_mint_key,
    feed_subscription::{
        ID,
        instruction::{
            FeedSubscriptionInstructionData, account::ClaimFeedValidatorClientRewardsAccounts,
        },
        state::{
            FeedDistribution, ProgramConfig, ValidatorClientRewards, find_claim_holding_address,
            find_feed_distribution_journal_address, find_program_config_address,
            find_validator_client_rewards_address,
        },
    },
    try_build_instruction,
};
use solana_account_decoder_client_types::UiAccountEncoding;
use solana_client::{
    rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig},
    rpc_filter::{Memcmp, RpcFilterType},
};
use solana_commitment_config::CommitmentConfig;
use solana_compute_budget_interface::ComputeBudgetInstruction;
use solana_sdk::{
    account::Account, instruction::Instruction, message::Message, program_pack::Pack,
    pubkey::Pubkey,
};
use spl_associated_token_account_interface::{
    address::get_associated_token_address, instruction::create_associated_token_account_idempotent,
};

use super::Actor;

/*
   doublezero-solana feeds validator-client-rewards claim \
       --client-id <ID> \
       [--destination-token-account <PUBKEY>] \
       [--max-transactions <N>] \
       [--multisig <PUBKEY> [--vault-index <U8>]]

   Every open holding the client has is found by listing the feed program's
   months, then claimed one instruction per holding, with each transaction
   taking as many holdings as fit its size limit.

   With --multisig, the Squads vault stands in for the wallet as manager, and
   the transactions are printed as base58 payloads for import into Squads
   rather than signed and sent.
*/

// Measured against the program with headroom: a claim whose journal and holding both derive at a
// bump of 255 used 20,426.
const CLAIM_COMPUTE_UNITS: u32 = 25_000;

#[derive(Debug, Args)]
pub struct ClaimCommand {
    /// Validator client ID.
    #[arg(long)]
    client_id: u16,
    /// Destination 2Z token account. Defaults to ATA(manager, 2Z), where the
    /// manager is the wallet, or the vault with --multisig.
    #[arg(long)]
    destination_token_account: Option<Pubkey>,
    /// Emit at most N transactions. Holdings are claimed in month order, so
    /// the first N are deterministic. Defaults to all of them.
    #[arg(long, value_name = "N")]
    max_transactions: Option<NonZeroUsize>,
    #[command(flatten)]
    squads: OptionalSquadsArgs,
    #[command(flatten)]
    write_opts: crate::command::WriteVerbOptions,
}

#[derive(Debug)]
struct HoldingToClaim {
    feed_key: Pubkey,
    year: u16,
    month: u8,
    holding_key: Pubkey,
    pre_balance: u64,
    compute_units: u32,
    claim_ix: Instruction,
}

impl HoldingToClaim {
    fn year_month(&self) -> String {
        format!("{}-{:02}", self.year, self.month)
    }
}

impl ClaimCommand {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        // The vault path builds no wallet, so --multisig runs on a machine with
        // no keypair. Every signing and sending option is inert there. Both
        // paths read through this connection, which resolves the URL the same
        // way the wallet does.
        let connection =
            crate::command::solana_connection(ctx, &self.write_opts.connection_options);
        let actor = match self.squads.try_find_vault_address(&connection).await? {
            Some(vault_key) => Actor::Vault(vault_key),
            None => Actor::Wallet(Box::new(crate::command::build_wallet(
                ctx,
                self.write_opts,
            )?)),
        };
        let actor_key = actor.key();

        let validator_client_rewards_key = find_validator_client_rewards_address(self.client_id).0;
        let program_config_key = find_program_config_address().0;
        let accounts = try_fetch_multiple_accounts(
            &connection,
            &[validator_client_rewards_key, program_config_key],
        )
        .await
        .context("fetching validator client rewards and the program config")?;

        let validator_client_rewards_account =
            accounts.first().and_then(Option::as_ref).with_context(|| {
                format!(
                    "validator client rewards not initialized for client-id {} (PDA {validator_client_rewards_key})",
                    self.client_id
                )
            })?;
        let validator_client_rewards =
            ZeroCopyAccountOwnedData::<ValidatorClientRewards>::from_account(
                validator_client_rewards_account,
            )
            .with_context(|| {
                format!("failed to decode ValidatorClientRewards at {validator_client_rewards_key}")
            })?;
        // Refusing here is what stops a payload the multisig can never execute
        // from consuming an approval round.
        super::validate_manager(
            actor.label(),
            &actor_key,
            &validator_client_rewards.manager_key,
        )?;

        let program_config_account =
            accounts.get(1).and_then(Option::as_ref).with_context(|| {
                format!("feed subscription program config {program_config_key} not found onchain")
            })?;
        let program_config =
            ZeroCopyAccountOwnedData::<ProgramConfig>::from_account(program_config_account)
                .with_context(|| {
                    format!("failed to decode ProgramConfig at {program_config_key}")
                })?;
        ensure!(
            !program_config.is_paused(),
            "the feed subscription program is paused, so it refuses this"
        );
        let oracle_key = program_config.oracle_key;

        let claim_holding_count = validator_client_rewards.claim_holding_count as usize;
        if claim_holding_count == 0 {
            writeln!(
                out,
                "No outstanding claim holdings for client_id {} (claim_holding_count is 0).",
                self.client_id
            )?;
            return Ok(());
        }

        // The program only opens holdings in the 2Z mint of the cluster it runs on.
        let network_env = connection
            .try_network_environment()
            .await
            .context("detecting the network environment for the 2Z mint")?;
        let doublezero_mint_key = environment_2z_token_mint_key(network_env);

        // Resolve the destination token account. An existing one has to be a
        // token account for 2Z. A missing one is an error on the direct path,
        // and on the vault path it is created by every payload, so long as it
        // is the vault's own associated token account.
        let destination_key = self
            .destination_token_account
            .unwrap_or_else(|| get_associated_token_address(&actor_key, &doublezero_mint_key));
        let destination_account = connection
            .get_account_with_commitment(&destination_key, CommitmentConfig::confirmed())
            .await
            .with_context(|| format!("fetching destination token account {destination_key}"))?
            .value;
        let (destination_authority_key, create_destination_ix) = match (destination_account, &actor)
        {
            (Some(destination_account), _) => {
                let vault_key = match &actor {
                    Actor::Wallet(_) => None,
                    Actor::Vault(vault_key) => Some(vault_key),
                };
                let destination_authority_key = validate_existing_destination(
                    &destination_key,
                    &destination_account,
                    &doublezero_mint_key,
                    vault_key,
                )?;
                (destination_authority_key, None)
            }
            (None, Actor::Wallet(_)) => bail!(
                "destination token account {destination_key} does not exist. Create it first. \
                 The wallet's own associated token account is created by \
                 `spl-token create-account --owner {actor_key} {doublezero_mint_key} --fee-payer <KEYPAIR>`"
            ),
            (None, Actor::Vault(vault_key)) => {
                // Compared against the derived address rather than against whether the
                // flag was given, so naming the vault's own associated token account
                // explicitly reaches the same place as leaving the flag off.
                ensure!(
                    destination_key
                        == get_associated_token_address(vault_key, &doublezero_mint_key),
                    "destination token account {destination_key} does not exist, and this \
                     command only creates the vault's own associated token account. Create \
                     it first, or drop --destination-token-account to claim into the vault's \
                     associated token account"
                );
                (
                    *vault_key,
                    Some(create_associated_token_account_idempotent(
                        vault_key,
                        vault_key,
                        &doublezero_mint_key,
                        &spl_token_interface::ID,
                    )),
                )
            }
        };

        // A holding's address is derived from its month, so probing the holding
        // address of every month the program has finds each one that is open.
        let feed_distributions = connection
            .get_program_accounts_with_config(
                &ID,
                RpcProgramAccountsConfig {
                    filters: Some(vec![RpcFilterType::Memcmp(Memcmp::new_raw_bytes(
                        0,
                        FeedDistribution::discriminator_slice().to_vec(),
                    ))]),
                    account_config: RpcAccountInfoConfig {
                        encoding: Some(UiAccountEncoding::Base64),
                        ..Default::default()
                    },
                    ..Default::default()
                },
            )
            .await
            .context("listing the feed program's months")?
            .into_iter()
            .filter_map(|(feed_distribution_key, account)| {
                ZeroCopyAccountOwnedData::<FeedDistribution>::from_account(&account)
                    .map(|feed_distribution| (feed_distribution_key, feed_distribution))
            })
            .collect::<Vec<_>>();
        let holding_addresses = feed_distributions
            .iter()
            .map(|(feed_distribution_key, _)| {
                find_claim_holding_address(
                    &validator_client_rewards_key,
                    feed_distribution_key,
                    &doublezero_mint_key,
                )
            })
            .collect::<Vec<_>>();
        let holding_keys = holding_addresses
            .iter()
            .map(|(holding_key, _)| *holding_key)
            .collect::<Vec<_>>();
        let holding_accounts = try_fetch_multiple_accounts(&connection, &holding_keys)
            .await
            .context("fetching claim holdings")?;

        let mut holdings = Vec::new();
        for (((feed_distribution_key, feed_distribution), (holding_key, holding_bump)), account) in
            feed_distributions
                .iter()
                .zip(holding_addresses)
                .zip(holding_accounts)
        {
            let Some(pre_balance) = holding_balance(account.as_ref(), &doublezero_mint_key) else {
                continue;
            };
            let journal_bump = find_feed_distribution_journal_address(feed_distribution_key).1;
            holdings.push(HoldingToClaim {
                feed_key: feed_distribution.feed_key,
                year: feed_distribution.year,
                month: feed_distribution.month,
                holding_key,
                pre_balance,
                // The program derives the month's journal and the holding again.
                compute_units: CLAIM_COMPUTE_UNITS
                    + Wallet::compute_units_for_bump_seed(journal_bump)
                    + Wallet::compute_units_for_bump_seed(holding_bump),
                claim_ix: try_build_instruction(
                    &ID,
                    ClaimFeedValidatorClientRewardsAccounts::new(
                        &actor_key,
                        self.client_id,
                        feed_distribution_key,
                        &destination_key,
                        &oracle_key,
                        &doublezero_mint_key,
                    ),
                    &FeedSubscriptionInstructionData::ClaimFeedValidatorClientRewards,
                )?,
            });
        }
        holdings.sort_unstable_by_key(|holding| (holding.year, holding.month, holding.feed_key));

        ensure!(
            !holdings.is_empty(),
            "client_id {} has {claim_holding_count} open claim holding(s), but none were found \
             searching {network_env:?} for the 2Z mint {doublezero_mint_key}. An endpoint with \
             an unrecognized genesis hash is read as Localnet",
            self.client_id
        );
        if holdings.len() < claim_holding_count {
            tracing::warn!(
                "found {} holding(s) in the 2Z mint {doublezero_mint_key}, but \
                 claim_holding_count is {claim_holding_count}",
                holdings.len()
            );
        }

        // Grow each transaction one holding at a time while a trial still fits.
        // The direct path measures the v0 transaction the wallet sends, counting
        // the fee payer and both compute budget instructions whether or not a
        // price is configured, so packing does not shift with the flags. The
        // vault path measures the legacy message the base58 payload carries
        // against what Squads leaves of the transaction that wraps it.
        let batches = match &actor {
            Actor::Wallet(wallet) => pack_holdings(&holdings, |batch| {
                let instructions = direct_instructions(
                    batch,
                    Some(&ComputeBudgetInstruction::set_compute_unit_price(0)),
                );
                Ok(wallet.try_transaction_size(&instructions)? <= MAX_TRANSACTION_SIZE)
            })?,
            Actor::Vault(vault_key) => pack_holdings(&holdings, |batch| {
                let instructions = vault_instructions(create_destination_ix.as_ref(), batch);
                let payload = Message::new(&instructions, Some(vault_key)).serialize();
                // The packing budget, not the encoder's hard budget, so a memo typed
                // into the import dialog still fits.
                Ok(payload.len() <= vault_transaction_packing_budget(instructions.len()))
            })?,
        };

        let emitted_count = self
            .max_transactions
            .map_or(batches.len(), |max| max.get().min(batches.len()));
        let (emitted, withheld) = batches.split_at(emitted_count);

        // Counted over `emitted`, since with --max-transactions the rest are not
        // claimed by this invocation.
        let emitted_holdings = emitted.iter().map(|batch| batch.len()).sum::<usize>();
        let emitted_pre_balance = emitted
            .iter()
            .flat_map(|batch| batch.iter())
            .map(|holding| holding.pre_balance)
            .fold(0, u64::saturating_add);

        writeln!(
            out,
            "Feed subscription - Claim Validator Client Rewards \
             (client_id={}, holdings={emitted_holdings}, transactions={emitted_count})",
            self.client_id,
        )?;

        match &actor {
            Actor::Wallet(wallet) => {
                writeln!(out, "  manager               : {actor_key}")?;
                writeln!(out, "  destination           : {destination_key}")?;
                writeln!(out, "  destination authority : {destination_authority_key}")?;
                writeln!(out, "  rent recovers         : {oracle_key}")?;

                // Batches are independent, so a later failure does not undo an
                // earlier executed batch.
                let mut executed_holdings = 0;
                for (batch_index, batch) in emitted.iter().enumerate() {
                    // Written before the send, so a dry run or a failed send
                    // still shows what the transaction claims.
                    writeln!(
                        out,
                        "\nTransaction {}/{emitted_count}: {} holding(s), months {}",
                        batch_index + 1,
                        batch.len(),
                        month_list(batch),
                    )?;
                    // The program moves each holding's whole balance, which a
                    // payout landing after this read would raise.
                    for holding in batch.iter() {
                        writeln!(
                            out,
                            "  {} feed {}: {} from {} (pre-claim)",
                            holding.year_month(),
                            holding.feed_key,
                            holding.pre_balance,
                            holding.holding_key,
                        )?;
                    }

                    let instructions =
                        direct_instructions(batch, wallet.compute_unit_price_ix.as_ref());
                    let transaction = wallet.new_transaction(&instructions).await?;
                    let tx_outcome = wallet.send_or_simulate_transaction(&transaction).await?;

                    if let TransactionOutcome::Executed(tx_sig) = tx_outcome {
                        executed_holdings += batch.len();
                        writeln!(out, "Claimed: {tx_sig}")?;
                        wallet.write_verbose_output(out, &[tx_sig]).await?;
                    }
                }

                if executed_holdings > 0 {
                    writeln!(
                        out,
                        "\nPre-claim total: {emitted_pre_balance} ({executed_holdings}/{emitted_holdings} holding(s) claimed across {emitted_count} transaction(s))."
                    )?;
                    match connection
                        .try_fetch_zero_copy_data_with_commitment::<ValidatorClientRewards>(
                            &validator_client_rewards_key,
                            CommitmentConfig::confirmed(),
                        )
                        .await
                    {
                        Ok(refetched) => writeln!(
                            out,
                            "Remaining claim holding count: {}",
                            refetched.claim_holding_count
                        )?,
                        Err(err) => {
                            tracing::warn!(
                                "post-claim validator client rewards re-fetch failed: {err}"
                            );
                            writeln!(out, "Remaining claim holding count: (unavailable)")?;
                        }
                    }
                }
            }
            Actor::Vault(vault_key) => {
                writeln!(out, "  manager (vault)       : {vault_key}")?;
                if let Some(multisig_key) = self.squads.multisig {
                    writeln!(
                        out,
                        "  multisig              : {multisig_key} (vault index {})",
                        self.squads.vault_index
                    )?;
                }
                match create_destination_ix {
                    Some(_) => writeln!(
                        out,
                        "  destination           : {destination_key} (missing; every payload \
                         creates it, and the vault pays the rent, so the vault has to hold SOL)"
                    )?,
                    None => writeln!(out, "  destination           : {destination_key}")?,
                }
                writeln!(out, "  destination authority : {destination_authority_key}")?;
                writeln!(out, "  rent recovers         : {oracle_key}")?;
                writeln!(out)?;
                writeln!(
                    out,
                    "Nothing was signed or sent. --keypair, --fee-payer, --dry-run, \
                     --with-compute-unit-price and --verbose have no effect with --multisig."
                )?;
                writeln!(
                    out,
                    "A payload carries no compute budget, since Squads sets the budget on its \
                     own execute transaction. Check that budget against the compute units each \
                     payload reports below."
                )?;

                let create_destination_units = create_destination_ix.as_ref().map_or(0, |_| {
                    Wallet::ata_address_and_create_compute_units(&actor_key, &doublezero_mint_key).1
                });
                for (index, batch) in emitted.iter().enumerate() {
                    let batch_pre_balance = batch
                        .iter()
                        .map(|holding| holding.pre_balance)
                        .fold(0, u64::saturating_add);

                    writeln!(out)?;
                    writeln!(out, "{}", "-".repeat(72))?;
                    writeln!(out, "Vault transaction {} of {emitted_count}", index + 1)?;
                    writeln!(out, "  holdings        : {}", batch.len())?;
                    for holding in batch.iter() {
                        writeln!(
                            out,
                            "    {} feed {}: {}",
                            holding.year_month(),
                            holding.feed_key,
                            holding.pre_balance
                        )?;
                    }
                    writeln!(out, "  pre-claim total : {batch_pre_balance}")?;
                    writeln!(
                        out,
                        "  compute units   : {} needed at execute",
                        batch_compute_units(batch) + create_destination_units
                    )?;
                    try_write_vault_transaction(
                        out,
                        &connection,
                        vault_key,
                        &vault_instructions(create_destination_ix.as_ref(), batch),
                    )?;
                }

                writeln!(out)?;
                writeln!(out, "{}", "-".repeat(72))?;
                writeln!(out, "Recap of the {emitted_count} payload(s) above:")?;
                for (index, batch) in emitted.iter().enumerate() {
                    writeln!(
                        out,
                        "  {}: {} holding(s), months {}",
                        index + 1,
                        batch.len(),
                        month_list(batch)
                    )?;
                }
                writeln!(
                    out,
                    "The payloads are independent: import them in any order, and a partial \
                     import is safe. Re-running this command finds only the holdings that are \
                     still open."
                )?;
            }
        }

        if !withheld.is_empty() {
            writeln!(
                out,
                "\nWithheld {} transaction(s) covering {} holding(s) because of \
                 --max-transactions. Re-run this command once the transactions above have \
                 executed.",
                withheld.len(),
                withheld.iter().map(|batch| batch.len()).sum::<usize>(),
            )?;
        }

        Ok(())
    }
}

/// Validate a destination token account that already exists, returning its authority.
/// The vault path passes `vault_key`, which requires the vault to be that authority: its
/// approvers see a base58 blob, so a destination the multisig does not control is a
/// destination nothing in the payload reveals. The direct path passes `None`, since an
/// operator signing with their own key is directing their own rewards.
fn validate_existing_destination(
    destination_key: &Pubkey,
    destination_account: &Account,
    mint_key: &Pubkey,
    vault_key: Option<&Pubkey>,
) -> Result<Pubkey> {
    ensure!(
        destination_account.owner == spl_token_interface::ID,
        "destination {destination_key} is not an SPL token account (owner = {})",
        destination_account.owner
    );
    let destination_token = spl_token_interface::state::Account::unpack(&destination_account.data)
        .with_context(|| format!("unpacking destination token account {destination_key}"))?;
    ensure!(
        destination_token.mint == *mint_key,
        "destination {destination_key} mint mismatch: expected {mint_key}, found {}",
        destination_token.mint
    );
    if let Some(vault_key) = vault_key {
        ensure!(
            destination_token.owner == *vault_key,
            "destination {destination_key} authority is {}, not the vault {vault_key}. \
             Approvers see only the base58 payload, so a claim into an account the multisig \
             does not control is invisible to them. Drop --destination-token-account to claim \
             into the vault's associated token account",
            destination_token.owner
        );
    }
    Ok(destination_token.owner)
}

/// Decode a fetched account as an open claim holding for `mint_key`,
/// returning its balance.
fn holding_balance(account: Option<&Account>, mint_key: &Pubkey) -> Option<u64> {
    let account = account?;
    if account.owner != spl_token_interface::ID {
        return None;
    }
    let token = spl_token_interface::state::Account::unpack(&account.data).ok()?;
    (token.mint == *mint_key).then_some(token.amount)
}

/// The instruction list the wallet signs: one claim per holding and the compute
/// budget. The trial passes a placeholder price so it is always counted.
fn direct_instructions(
    batch: &[HoldingToClaim],
    compute_unit_price_ix: Option<&Instruction>,
) -> Vec<Instruction> {
    let mut instructions = batch
        .iter()
        .map(|holding| holding.claim_ix.clone())
        .collect::<Vec<_>>();
    instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(
        batch_compute_units(batch),
    ));
    instructions.extend(compute_unit_price_ix.cloned());
    instructions
}

/// The payload a vault imports: the destination create when the account is
/// missing, and one claim per holding.
///
/// No compute budget instructions, since Squads sets the budget on its own
/// execute transaction and a budget instruction reached through a CPI is a
/// no-op that only burns compute units.
fn vault_instructions(
    create_destination_ix: Option<&Instruction>,
    batch: &[HoldingToClaim],
) -> Vec<Instruction> {
    create_destination_ix
        .into_iter()
        .cloned()
        .chain(batch.iter().map(|holding| holding.claim_ix.clone()))
        .collect()
}

fn batch_compute_units(batch: &[HoldingToClaim]) -> u32 {
    batch.iter().map(|holding| holding.compute_units).sum()
}

/// Split month-ordered holdings into consecutive batches, growing each one
/// holding at a time while `fits` accepts it.
fn pack_holdings(
    holdings: &[HoldingToClaim],
    mut fits: impl FnMut(&[HoldingToClaim]) -> Result<bool>,
) -> Result<Vec<&[HoldingToClaim]>> {
    let mut batches = Vec::new();
    let mut start = 0;
    while start < holdings.len() {
        ensure!(
            fits(&holdings[start..=start])?,
            "the holding for {} feed {} does not fit a transaction on its own",
            holdings[start].year_month(),
            holdings[start].feed_key
        );
        let mut end = start + 1;
        while end < holdings.len() && fits(&holdings[start..=end])? {
            end += 1;
        }
        batches.push(&holdings[start..end]);
        start = end;
    }
    Ok(batches)
}

/// The months a batch covers, each named once.
fn month_list(batch: &[HoldingToClaim]) -> String {
    let mut months = batch
        .iter()
        .map(HoldingToClaim::year_month)
        .collect::<Vec<_>>();
    months.dedup();
    months.join(", ")
}

#[cfg(test)]
mod tests {
    use clap::{Parser, error::ErrorKind};
    use doublezero_solana_client_tools::{
        rpc::{NetworkEnvironment, SolanaConnection},
        squads::{
            SQUADS_IMPORT_MEMO_RESERVE_BYTES, try_encode_vault_transaction,
            vault_transaction_payload_budget,
        },
    };
    use solana_sdk::signature::Keypair;

    use super::*;

    #[derive(Parser)]
    struct Cli {
        #[command(flatten)]
        cmd: ClaimCommand,
    }

    fn parse(extra: &[&str]) -> Result<ClaimCommand, clap::Error> {
        let mut args = vec!["test", "--client-id", "7"];
        args.extend_from_slice(extra);
        Cli::try_parse_from(args).map(|cli| cli.cmd)
    }

    // One holding per month given, each a different feed's, with the claim the command builds.
    fn holdings(
        manager_key: &Pubkey,
        destination_key: &Pubkey,
        months: &[u8],
    ) -> Vec<HoldingToClaim> {
        let oracle_key = Pubkey::new_unique();
        let mint_key = Pubkey::new_unique();
        months
            .iter()
            .map(|&month| {
                let claim_ix = try_build_instruction(
                    &ID,
                    ClaimFeedValidatorClientRewardsAccounts::new(
                        manager_key,
                        7,
                        &Pubkey::new_unique(),
                        destination_key,
                        &oracle_key,
                        &mint_key,
                    ),
                    &FeedSubscriptionInstructionData::ClaimFeedValidatorClientRewards,
                )
                .unwrap();
                HoldingToClaim {
                    feed_key: Pubkey::new_unique(),
                    year: 2_026,
                    month,
                    holding_key: claim_ix.accounts[5].pubkey,
                    pre_balance: 10,
                    compute_units: CLAIM_COMPUTE_UNITS,
                    claim_ix,
                }
            })
            .collect()
    }

    fn wallet(fee_payer: Option<Keypair>) -> Wallet {
        Wallet {
            connection: SolanaConnection::new(NetworkEnvironment::DEFAULT_LOCALNET_URL.into()),
            signer: Keypair::new(),
            compute_unit_price_ix: None,
            verbose: false,
            fee_payer,
            dry_run: false,
        }
    }

    #[test]
    fn test_parses_required_args_with_implicit_destination() {
        let cmd = parse(&[]).unwrap();
        assert_eq!(cmd.client_id, 7);
        assert!(cmd.destination_token_account.is_none());
        assert!(cmd.squads.multisig.is_none());
        assert!(cmd.max_transactions.is_none());
    }

    #[test]
    fn test_parses_explicit_destination() {
        let destination_key = Pubkey::new_unique();
        let cmd = parse(&["--destination-token-account", &destination_key.to_string()]).unwrap();
        assert_eq!(cmd.destination_token_account, Some(destination_key));
    }

    #[test]
    fn test_parses_multisig_and_vault_index() {
        let multisig_key = Pubkey::new_unique();
        let cmd = parse(&[
            "--multisig",
            &multisig_key.to_string(),
            "--vault-index",
            "2",
        ])
        .unwrap();
        assert_eq!(cmd.squads.multisig, Some(multisig_key));
        assert_eq!(cmd.squads.vault_index, 2);
    }

    #[test]
    fn test_vault_index_requires_multisig() {
        let error = parse(&["--vault-index", "2"]).unwrap_err();
        assert_eq!(error.kind(), ErrorKind::MissingRequiredArgument);
    }

    #[test]
    fn test_max_transactions_refuses_zero() {
        assert_eq!(
            parse(&["--max-transactions", "0"]).unwrap_err().kind(),
            ErrorKind::ValueValidation
        );
        let cmd = parse(&["--max-transactions", "1"]).unwrap();
        assert_eq!(cmd.max_transactions, NonZeroUsize::new(1));
    }

    #[test]
    fn test_pack_holdings_grows_each_batch_until_it_stops_fitting() {
        let holdings = holdings(&Pubkey::new_unique(), &Pubkey::new_unique(), &[3; 7]);
        let batches = pack_holdings(&holdings, |batch| Ok(batch.len() <= 3)).unwrap();

        let sizes = batches.iter().map(|batch| batch.len()).collect::<Vec<_>>();
        assert_eq!(sizes, [3, 3, 1]);
        assert_eq!(batches[1][0].feed_key, holdings[3].feed_key);
        assert_eq!(batches[2][0].feed_key, holdings[6].feed_key);
    }

    #[test]
    fn test_pack_holdings_refuses_a_holding_that_fits_nowhere() {
        let holdings = holdings(&Pubkey::new_unique(), &Pubkey::new_unique(), &[3, 4]);
        let error = pack_holdings(&holdings, |batch| {
            Ok(!batch.iter().any(|holding| holding.month == 4))
        })
        .unwrap_err()
        .to_string();
        assert_eq!(
            error,
            format!(
                "the holding for 2026-04 feed {} does not fit a transaction on its own",
                holdings[1].feed_key
            )
        );
    }

    #[test]
    fn test_pack_holdings_of_nothing_is_no_batches() {
        assert!(pack_holdings(&[], |_| Ok(true)).unwrap().is_empty());
    }

    fn direct_batches(wallet: &Wallet, holdings: &[HoldingToClaim]) -> usize {
        let price_ix = ComputeBudgetInstruction::set_compute_unit_price(1_000);
        let batches = pack_holdings(holdings, |batch| {
            Ok(
                wallet.try_transaction_size(&direct_instructions(batch, Some(&price_ix)))?
                    <= MAX_TRANSACTION_SIZE,
            )
        })
        .unwrap();

        // Every batch fits as sent, with and without a configured price, and
        // the first would not take one more holding.
        for batch in &batches {
            for compute_unit_price_ix in [None, Some(&price_ix)] {
                let size = wallet
                    .try_transaction_size(&direct_instructions(batch, compute_unit_price_ix))
                    .unwrap();
                assert!(size <= MAX_TRANSACTION_SIZE, "{size}");
            }
        }
        let overfull = &holdings[..batches[0].len() + 1];
        assert!(
            wallet
                .try_transaction_size(&direct_instructions(overfull, Some(&price_ix)))
                .unwrap()
                > MAX_TRANSACTION_SIZE
        );

        batches[0].len()
    }

    #[test]
    fn test_direct_transaction_takes_7_holdings_with_one_signer() {
        let wallet = wallet(None);
        let holdings = holdings(&wallet.pubkey(), &Pubkey::new_unique(), &[3; 30]);
        assert_eq!(direct_batches(&wallet, &holdings), 7);
    }

    #[test]
    fn test_direct_transaction_takes_6_holdings_with_a_distinct_fee_payer() {
        let wallet = wallet(Some(Keypair::new()));
        let holdings = holdings(&wallet.pubkey(), &Pubkey::new_unique(), &[3; 30]);
        assert_eq!(direct_batches(&wallet, &holdings), 6);
    }

    #[test]
    fn test_direct_instructions_budget_the_sum_of_their_claims() {
        let mut holdings = holdings(&Pubkey::new_unique(), &Pubkey::new_unique(), &[3, 4]);
        holdings[1].compute_units = CLAIM_COMPUTE_UNITS + 1_500;
        let price_ix = ComputeBudgetInstruction::set_compute_unit_price(1_000);

        let instructions = direct_instructions(&holdings, Some(&price_ix));
        assert_eq!(
            instructions,
            vec![
                holdings[0].claim_ix.clone(),
                holdings[1].claim_ix.clone(),
                ComputeBudgetInstruction::set_compute_unit_limit(2 * CLAIM_COMPUTE_UNITS + 1_500),
                price_ix,
            ]
        );
        assert_eq!(direct_instructions(&holdings, None).len(), 3);
    }

    fn vault_batches(
        vault_key: &Pubkey,
        holdings: &[HoldingToClaim],
        create_destination_ix: Option<&Instruction>,
    ) -> usize {
        let batches = pack_holdings(holdings, |batch| {
            let instructions = vault_instructions(create_destination_ix, batch);
            let payload = Message::new(&instructions, Some(vault_key)).serialize();
            Ok(payload.len() <= vault_transaction_packing_budget(instructions.len()))
        })
        .unwrap();

        // The checked encoder takes every batch, and takes it with 32 bytes of
        // memo spent out of the payload's headroom, which is the room the
        // packing budget exists to leave. No payload carries a compute budget.
        for batch in &batches {
            let instructions = vault_instructions(create_destination_ix, batch);
            try_encode_vault_transaction(vault_key, &instructions).unwrap();
            assert!(
                instructions
                    .iter()
                    .all(|ix| ix.program_id != solana_compute_budget_interface::ID)
            );
            match create_destination_ix {
                Some(create_destination_ix) => {
                    assert_eq!(&instructions[0], create_destination_ix);
                    assert_eq!(instructions.len(), batch.len() + 1);
                }
                None => assert_eq!(instructions.len(), batch.len()),
            }

            let payload = Message::new(&instructions, Some(vault_key)).serialize();
            let budget = vault_transaction_payload_budget(instructions.len());
            assert!(
                payload.len() + SQUADS_IMPORT_MEMO_RESERVE_BYTES <= budget,
                "{} of {budget}",
                payload.len()
            );
        }

        // One more holding is over the packing budget.
        let overfull = vault_instructions(create_destination_ix, &holdings[..batches[0].len() + 1]);
        let payload = Message::new(&overfull, Some(vault_key)).serialize();
        assert!(payload.len() > vault_transaction_packing_budget(overfull.len()));

        batches[0].len()
    }

    #[test]
    fn test_vault_payload_takes_4_holdings_into_an_existing_destination() {
        let vault_key = Pubkey::new_unique();
        let holdings = holdings(&vault_key, &Pubkey::new_unique(), &[3; 30]);
        assert_eq!(vault_batches(&vault_key, &holdings, None), 4);
    }

    #[test]
    fn test_vault_payload_takes_3_holdings_when_every_payload_creates_the_destination() {
        let vault_key = Pubkey::new_unique();
        let mint_key = Pubkey::new_unique();
        let destination_key = get_associated_token_address(&vault_key, &mint_key);
        let holdings = holdings(&vault_key, &destination_key, &[3; 30]);
        let create_destination_ix = create_associated_token_account_idempotent(
            &vault_key,
            &vault_key,
            &mint_key,
            &spl_token_interface::ID,
        );
        assert_eq!(
            vault_batches(&vault_key, &holdings, Some(&create_destination_ix)),
            3
        );
    }

    #[test]
    fn test_month_list_names_each_month_once() {
        let holdings = holdings(&Pubkey::new_unique(), &Pubkey::new_unique(), &[3, 3, 4, 11]);
        assert_eq!(month_list(&holdings), "2026-03, 2026-04, 2026-11");
        assert_eq!(month_list(&holdings[..1]), "2026-03");
    }

    fn token_account(
        owner_program_key: Pubkey,
        authority_key: Pubkey,
        mint_key: Pubkey,
        amount: u64,
    ) -> Account {
        let mut data = vec![0; spl_token_interface::state::Account::LEN];
        spl_token_interface::state::Account {
            mint: mint_key,
            owner: authority_key,
            amount,
            state: spl_token_interface::state::AccountState::Initialized,
            ..Default::default()
        }
        .pack_into_slice(&mut data);
        Account {
            lamports: 1,
            data,
            owner: owner_program_key,
            executable: false,
            rent_epoch: 0,
        }
    }

    #[test]
    fn test_holding_balance_counts_only_a_token_account_of_the_mint() {
        let mint_key = Pubkey::new_unique();
        let authority_key = Pubkey::new_unique();

        let holding = token_account(spl_token_interface::ID, authority_key, mint_key, 42);
        assert_eq!(holding_balance(Some(&holding), &mint_key), Some(42));

        assert_eq!(holding_balance(None, &mint_key), None);
        let foreign_program = token_account(Pubkey::new_unique(), authority_key, mint_key, 42);
        assert_eq!(holding_balance(Some(&foreign_program), &mint_key), None);
        let other_mint = token_account(
            spl_token_interface::ID,
            authority_key,
            Pubkey::new_unique(),
            42,
        );
        assert_eq!(holding_balance(Some(&other_mint), &mint_key), None);
    }

    #[test]
    fn test_validate_existing_destination_refuses_an_authority_that_is_not_the_vault() {
        let vault_key = Pubkey::new_unique();
        let mint_key = Pubkey::new_unique();
        let destination_key = Pubkey::new_unique();
        let stranger_key = Pubkey::new_unique();
        let account = token_account(spl_token_interface::ID, stranger_key, mint_key, 0);

        let err =
            validate_existing_destination(&destination_key, &account, &mint_key, Some(&vault_key))
                .unwrap_err()
                .to_string();
        assert!(err.contains(&stranger_key.to_string()), "{err}");
        assert!(err.contains(&vault_key.to_string()), "{err}");

        // The vault's own account passes, and the direct path, which passes no
        // vault, is free to direct rewards wherever its signer chooses.
        let vault_owned = token_account(spl_token_interface::ID, vault_key, mint_key, 0);
        assert_eq!(
            validate_existing_destination(
                &destination_key,
                &vault_owned,
                &mint_key,
                Some(&vault_key)
            )
            .unwrap(),
            vault_key
        );
        assert_eq!(
            validate_existing_destination(&destination_key, &account, &mint_key, None).unwrap(),
            stranger_key
        );
    }

    #[test]
    fn test_validate_existing_destination_refuses_a_foreign_owner_and_mint() {
        let mint_key = Pubkey::new_unique();
        let destination_key = Pubkey::new_unique();
        let authority_key = Pubkey::new_unique();

        let foreign_program = token_account(Pubkey::new_unique(), authority_key, mint_key, 0);
        assert!(
            validate_existing_destination(&destination_key, &foreign_program, &mint_key, None)
                .unwrap_err()
                .to_string()
                .contains("not an SPL token account")
        );

        let other_mint = token_account(
            spl_token_interface::ID,
            authority_key,
            Pubkey::new_unique(),
            0,
        );
        assert!(
            validate_existing_destination(&destination_key, &other_mint, &mint_key, None)
                .unwrap_err()
                .to_string()
                .contains("mint mismatch")
        );
    }
}
