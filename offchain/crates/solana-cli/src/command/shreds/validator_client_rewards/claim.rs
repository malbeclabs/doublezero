use std::{io::Write, num::NonZeroUsize};

use anyhow::{Context, Result, bail, ensure};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_solana_client_tools::{
    account::zero_copy::ZeroCopyAccountOwnedData,
    payer::{TransactionOutcome, Wallet},
    rpc::{SolanaConnection, try_fetch_multiple_accounts},
    squads::{OptionalSquadsArgs, try_write_vault_transaction, vault_transaction_payload_budget},
    transaction::MAX_TRANSACTION_SIZE,
};
use doublezero_solana_sdk::{
    shred_subscription::{
        ID,
        instruction::{
            ClaimHoldingId, ShredSubscriptionInstructionData,
            account::ClaimValidatorClientRewardsAccounts,
        },
        state::{
            ValidatorClientRewards, find_claim_holding_address, find_program_config_address,
            find_validator_client_rewards_address, parse_program_config_shred_oracle_key,
        },
    },
    try_build_instruction,
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

/*
   doublezero-solana shreds validator-client-rewards claim \
       --client-id <ID> --rewards-token-mint <PUBKEY> \
       [--subscription-epoch <EPOCH> ...] \
       [--destination-token-account <PUBKEY>] \
       [--max-transactions <N>] \
       [--multisig <PUBKEY> [--vault-index <U8>]]

   When no --subscription-epoch is given, every outstanding holding for the
   client and mint is discovered and claimed across as many transactions as
   needed. Each transaction takes as many holdings as fit its size limit.

   With --multisig, the Squads vault stands in for the wallet as manager, and
   the transactions are printed as base58 payloads for import into Squads
   rather than signed and sent.
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
    /// Destination token account. Defaults to ATA(manager, rewards_token_mint),
    /// where the manager is the wallet, or the vault with --multisig.
    #[arg(long)]
    pub destination_token_account: Option<Pubkey>,
    /// Emit at most N transactions. Holdings are claimed in epoch order, so
    /// the first N are deterministic. Defaults to all of them.
    #[arg(long, value_name = "N")]
    pub max_transactions: Option<NonZeroUsize>,
    #[command(flatten)]
    pub squads: OptionalSquadsArgs,
    #[command(flatten)]
    pub write_opts: crate::command::WriteVerbOptions,
}

pub(crate) fn resolve_destination(
    manager: &Pubkey,
    mint: &Pubkey,
    override_destination: Option<Pubkey>,
) -> Pubkey {
    override_destination.unwrap_or_else(|| get_associated_token_address(manager, mint))
}

/// Refuse an actor that is not the recorded manager. `actor` names what was
/// checked, the wallet or the vault, so the message says which key fell short.
pub(crate) fn validate_manager(
    actor: &str,
    actor_key: &Pubkey,
    validator_client_rewards_manager_key: &Pubkey,
) -> Result<()> {
    ensure!(
        actor_key == validator_client_rewards_manager_key,
        "manager mismatch: {actor} is {actor_key}, validator client rewards manager is {validator_client_rewards_manager_key}"
    );
    Ok(())
}

// How far back (in subscription epochs) auto-discovery probes from the current
// epoch. Holdings older than the on-chain abandonment window are swept, so this
// covers every holding that can still exist.
const MAX_DISCOVERY_LOOKBACK: u64 = 90;

#[derive(Debug)]
struct HoldingToClaim {
    epoch: u64,
    bump_seed: u8,
    holding_pda: Pubkey,
    pre_balance: u64,
}

// Who signs for the manager: the wallet itself, or a Squads vault the multisig
// signs for once a payload is imported and approved.
enum Actor {
    Wallet(Box<Wallet>),
    Vault {
        vault_key: Pubkey,
        connection: SolanaConnection,
    },
}

impl Actor {
    fn key(&self) -> Pubkey {
        match self {
            Self::Wallet(wallet) => wallet.pubkey(),
            Self::Vault { vault_key, .. } => *vault_key,
        }
    }

    fn label(&self) -> &'static str {
        match self {
            Self::Wallet(_) => "wallet",
            Self::Vault { .. } => "vault",
        }
    }

    fn connection(&self) -> &SolanaConnection {
        match self {
            Self::Wallet(wallet) => &wallet.connection,
            Self::Vault { connection, .. } => connection,
        }
    }
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
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        // The vault path builds no wallet, so --multisig runs on a machine with no
        // keypair. Every signing and sending option is inert there.
        let connection =
            crate::command::solana_connection(ctx, &self.write_opts.connection_options);
        let actor = match self.squads.try_find_vault_address(&connection).await? {
            Some(vault_key) => Actor::Vault {
                vault_key,
                connection,
            },
            None => Actor::Wallet(Box::new(crate::command::build_wallet(
                ctx,
                self.write_opts,
            )?)),
        };
        let actor_key = actor.key();
        let connection = actor.connection();

        let validator_client_rewards_key = find_validator_client_rewards_address(self.client_id).0;
        let program_config_key = find_program_config_address().0;

        // Single fetch: validator client rewards, program config.
        let accounts = try_fetch_multiple_accounts(
            connection,
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
        let validator_client_rewards =
            ZeroCopyAccountOwnedData::<ValidatorClientRewards>::from_account(
                validator_client_rewards_account,
            )
            .with_context(|| {
                format!("failed to decode ValidatorClientRewards at {validator_client_rewards_key}")
            })?;
        // Refusing here is what stops a payload the multisig can never execute
        // from consuming an approval round.
        validate_manager(
            actor.label(),
            &actor_key,
            &validator_client_rewards.manager_key,
        )?;

        let config_account = accounts
            .get(1)
            .and_then(|a| a.as_ref())
            .with_context(|| format!("ProgramConfig {program_config_key} not found onchain"))?;
        let rent_beneficiary_key = parse_program_config_shred_oracle_key(&config_account.data)
            .context("failed to parse shred_oracle_key from ProgramConfig")?;

        // Resolve the set of holdings to claim: explicit epochs (validated), or
        // every outstanding holding discovered on chain.
        let holdings = if self.subscription_epochs.is_empty() {
            let target = validator_client_rewards.claim_holding_count as usize;
            if target == 0 {
                writeln!(
                    out,
                    "No outstanding claim holdings for client_id {} (claim_holding_count is 0).",
                    self.client_id
                )?;
                return Ok(());
            }
            // The shred-subscription program stamps `current_subscription_epoch`
            // from the Clock of the cluster it runs on, which is exactly the
            // cluster `connection` talks to, so the live epoch there is the
            // discovery ceiling.
            let current_epoch = connection
                .get_epoch_info()
                .await
                .context("fetching current epoch")?
                .epoch;
            let discovered = discover_holdings(
                connection,
                &validator_client_rewards_key,
                &self.rewards_token_mint,
                current_epoch,
            )
            .await?;
            if discovered.is_empty() {
                writeln!(
                    out,
                    "No claim holdings for client_id {} found for mint {} within the last {MAX_DISCOVERY_LOOKBACK} epochs.",
                    self.client_id, self.rewards_token_mint,
                )?;
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
            writeln!(
                out,
                "Discovered {} outstanding holding(s) for client_id {} (mint {}).",
                discovered.len(),
                self.client_id,
                self.rewards_token_mint,
            )?;
            discovered
        } else {
            validate_explicit_holdings(
                connection,
                &validator_client_rewards_key,
                &self.rewards_token_mint,
                &self.subscription_epochs,
            )
            .await?
        };

        if holdings.is_empty() {
            writeln!(
                out,
                "Nothing to claim for client_id {} (mint {}); no valid holdings.",
                self.client_id, self.rewards_token_mint,
            )?;
            return Ok(());
        }

        // Resolve the destination token account. An existing one has to be a
        // token account for the mint. A missing one is an error on the direct
        // path, and on the vault path it is created by every payload, so long
        // as it is the vault's own associated token account.
        let destination_key = resolve_destination(
            &actor_key,
            &self.rewards_token_mint,
            self.destination_token_account,
        );
        let destination_account = connection
            .get_account_with_commitment(&destination_key, CommitmentConfig::confirmed())
            .await
            .with_context(|| format!("fetching destination token account {destination_key}"))?
            .value;
        let (destination_authority_key, create_destination_ix) = match (destination_account, &actor)
        {
            (Some(destination_account), _) => {
                if destination_account.owner != spl_token_interface::ID {
                    bail!(
                        "destination {destination_key} is not an SPL token account (owner = {})",
                        destination_account.owner
                    );
                }
                let destination_token =
                    spl_token_interface::state::Account::unpack(&destination_account.data)
                        .with_context(|| {
                            format!("unpacking destination token account {destination_key}")
                        })?;
                if destination_token.mint != self.rewards_token_mint {
                    bail!(
                        "destination {destination_key} mint mismatch: expected {}, found {}",
                        self.rewards_token_mint,
                        destination_token.mint
                    );
                }
                (destination_token.owner, None)
            }
            (None, Actor::Wallet(_)) => bail!(
                "destination token account {destination_key} does not exist. \
                     Run: `spl-token create-account --owner {actor_key} {} --fee-payer {actor_key}`",
                self.rewards_token_mint
            ),
            (None, Actor::Vault { vault_key, .. }) => {
                ensure!(
                    self.destination_token_account.is_none(),
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
                        &self.rewards_token_mint,
                        &spl_token_interface::ID,
                    )),
                )
            }
        };

        let check_cli_version_ix = super::super::build_check_cli_version_instruction()?;
        let build_claim_ix = |batch: &[HoldingToClaim]| {
            build_claim_instruction(
                self.client_id,
                &actor_key,
                &destination_key,
                &rent_beneficiary_key,
                &self.rewards_token_mint,
                batch,
            )
        };

        // Grow each transaction one holding at a time while a trial still fits.
        // The direct path measures the v0 transaction the wallet sends, counting
        // the fee payer and both compute budget instructions whether or not a
        // price is configured, so packing does not shift with the flags. The
        // vault path measures the legacy message the base58 payload carries
        // against what Squads leaves of the transaction that wraps it.
        let batches = match &actor {
            Actor::Wallet(wallet) => pack_holdings(&holdings, |batch| {
                let instructions = direct_instructions(
                    &check_cli_version_ix,
                    build_claim_ix(batch)?,
                    batch.len(),
                    Some(&ComputeBudgetInstruction::set_compute_unit_price(0)),
                );
                Ok(wallet.try_transaction_size(&instructions)? <= MAX_TRANSACTION_SIZE)
            })?,
            Actor::Vault { vault_key, .. } => pack_holdings(&holdings, |batch| {
                let instructions = vault_instructions(
                    &check_cli_version_ix,
                    create_destination_ix.as_ref(),
                    build_claim_ix(batch)?,
                );
                let payload = Message::new(&instructions, Some(vault_key)).serialize();
                Ok(payload.len() <= vault_transaction_payload_budget(instructions.len()))
            })?,
        };

        let emitted_count = self
            .max_transactions
            .map_or(batches.len(), |max| max.get().min(batches.len()));
        let (emitted, withheld) = batches.split_at(emitted_count);

        let total_holdings = holdings.len();
        let total_pre_balance = holdings.iter().fold(0u64, |total, holding| {
            total.saturating_add(holding.pre_balance)
        });

        writeln!(
            out,
            "Shred subscription - Claim Validator Client Rewards \
             (client_id={}, mint={}, holdings={total_holdings}, transactions={emitted_count})",
            self.client_id, self.rewards_token_mint,
        )?;
        if !withheld.is_empty() {
            writeln!(
                out,
                "  {} more transaction(s) withheld by --max-transactions",
                withheld.len()
            )?;
        }

        match &actor {
            Actor::Wallet(wallet) => {
                writeln!(out, "  manager               : {actor_key}")?;
                writeln!(out, "  destination           : {destination_key}")?;
                writeln!(out, "  destination authority : {destination_authority_key}")?;
                writeln!(out, "  rent recovers         : {rent_beneficiary_key}")?;

                execute_direct(
                    out,
                    wallet,
                    emitted,
                    &check_cli_version_ix,
                    &build_claim_ix,
                    total_holdings,
                    total_pre_balance,
                    &validator_client_rewards_key,
                )
                .await?;
            }
            Actor::Vault {
                vault_key,
                connection,
            } => {
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
                writeln!(out, "  rent recovers         : {rent_beneficiary_key}")?;
                writeln!(out)?;
                writeln!(
                    out,
                    "Nothing was signed or sent. --keypair, --fee-payer, --dry-run, \
                     --with-compute-unit-price and --verbose have no effect with --multisig."
                )?;

                for (index, batch) in emitted.iter().enumerate() {
                    let instructions = vault_instructions(
                        &check_cli_version_ix,
                        create_destination_ix.as_ref(),
                        build_claim_ix(batch)?,
                    );
                    let batch_pre_balance = batch.iter().fold(0u64, |total, holding| {
                        total.saturating_add(holding.pre_balance)
                    });

                    writeln!(out)?;
                    writeln!(out, "{}", "-".repeat(72))?;
                    writeln!(out, "Vault transaction {} of {emitted_count}", index + 1)?;
                    writeln!(
                        out,
                        "  holdings        : {} (epochs {})",
                        batch.len(),
                        epoch_range(batch)
                    )?;
                    writeln!(out, "  pre-claim total : {batch_pre_balance}")?;
                    try_write_vault_transaction(out, connection, vault_key, &instructions)?;
                }

                writeln!(out)?;
                writeln!(out, "{}", "-".repeat(72))?;
                writeln!(out, "Recap of the {emitted_count} payload(s) above:")?;
                for (index, batch) in emitted.iter().enumerate() {
                    writeln!(
                        out,
                        "  {}: {} holding(s), epochs {}",
                        index + 1,
                        batch.len(),
                        epoch_range(batch)
                    )?;
                }
                writeln!(
                    out,
                    "The payloads are independent: import them in any order, and a partial \
                     import is safe. Re-running this command discovers only the holdings \
                     that are still outstanding."
                )?;
            }
        }

        write_withheld_note(out, withheld)
    }
}

/// Sign and send each batch with the wallet, reporting what each transaction
/// drained and the holding count left afterwards.
#[allow(clippy::too_many_arguments)]
async fn execute_direct(
    out: &mut impl Write,
    wallet: &Wallet,
    batches: &[&[HoldingToClaim]],
    check_cli_version_ix: &Instruction,
    build_claim_ix: &impl Fn(&[HoldingToClaim]) -> Result<Instruction>,
    total_holdings: usize,
    total_pre_balance: u64,
    validator_client_rewards_key: &Pubkey,
) -> Result<()> {
    let batch_count = batches.len();

    // Batches are independent, so a later failure does not undo an earlier
    // executed batch.
    let mut executed_holdings = 0;
    let mut last_executed = false;
    for (batch_index, batch) in batches.iter().enumerate() {
        let instructions = direct_instructions(
            check_cli_version_ix,
            build_claim_ix(batch)?,
            batch.len(),
            wallet.compute_unit_price_ix.as_ref(),
        );

        if batch_count > 1 {
            writeln!(
                out,
                "\nTransaction {}/{batch_count}: {} holding(s), epochs {}",
                batch_index + 1,
                batch.len(),
                epoch_range(batch),
            )?;
        }

        let transaction = wallet.new_transaction(&instructions).await?;
        let tx_outcome = wallet.send_or_simulate_transaction(&transaction).await?;

        if let TransactionOutcome::Executed(tx_sig) = tx_outcome {
            executed_holdings += batch.len();
            last_executed = true;
            writeln!(out, "Claimed: {tx_sig}")?;
            // The on-chain handler transfers the full balance of each
            // holding, but these balances were read pre-tx. A top-up
            // between the read and the claim makes the actual drained amount
            // higher. Diff the destination balance before/after for the
            // authoritative number.
            for holding in batch.iter() {
                writeln!(
                    out,
                    "  epoch {}: {} from {} (pre-claim)",
                    holding.epoch, holding.pre_balance, holding.holding_pda,
                )?;
            }
            wallet.write_verbose_output(out, &[tx_sig]).await?;
        }
    }

    if last_executed {
        writeln!(
            out,
            "\nPre-claim total: {total_pre_balance} ({executed_holdings}/{total_holdings} holding(s) claimed across {batch_count} transaction(s))."
        )?;

        // Re-fetch the validator client rewards account to report the
        // post-tx claim_holding_count.
        match wallet
            .connection
            .try_fetch_zero_copy_data_with_commitment::<ValidatorClientRewards>(
                validator_client_rewards_key,
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
                eprintln!("warning: post-claim validator client rewards re-fetch failed: {err}");
                writeln!(out, "Remaining claim holding count: (unavailable)")?;
            }
        }
    }

    Ok(())
}

fn build_claim_instruction(
    client_id: u16,
    manager_key: &Pubkey,
    destination_key: &Pubkey,
    rent_beneficiary_key: &Pubkey,
    mint_key: &Pubkey,
    batch: &[HoldingToClaim],
) -> Result<Instruction> {
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
        .collect();

    let instruction = try_build_instruction(
        &ID,
        ClaimValidatorClientRewardsAccounts::new(
            client_id,
            manager_key,
            destination_key,
            rent_beneficiary_key,
            mint_key,
            &epochs,
        ),
        &ShredSubscriptionInstructionData::ClaimValidatorClientRewards(claim_holding_ids),
    )?;
    Ok(instruction)
}

/// The instruction list the wallet signs: the version check, the claim, and the
/// compute budget. The trial passes a placeholder price so it is always counted.
fn direct_instructions(
    check_cli_version_ix: &Instruction,
    claim_ix: Instruction,
    holding_count: usize,
    compute_unit_price_ix: Option<&Instruction>,
) -> Vec<Instruction> {
    // ~30k CU per holding (token transfer + close + state decrement), plus
    // the check-cli-version instruction.
    let compute_unit_limit = 30_000u32.saturating_mul(holding_count as u32 + 1);
    let mut instructions = vec![
        check_cli_version_ix.clone(),
        claim_ix,
        ComputeBudgetInstruction::set_compute_unit_limit(compute_unit_limit),
    ];
    instructions.extend(compute_unit_price_ix.cloned());
    instructions
}

/// The payload a vault imports: the version check, the destination create when
/// the account is missing, and the claim. No compute budget instructions, since
/// Squads sets the budget on its own execute transaction and a budget
/// instruction reached through a CPI is a no-op that only burns compute units.
fn vault_instructions(
    check_cli_version_ix: &Instruction,
    create_destination_ix: Option<&Instruction>,
    claim_ix: Instruction,
) -> Vec<Instruction> {
    let mut instructions = vec![check_cli_version_ix.clone()];
    instructions.extend(create_destination_ix.cloned());
    instructions.push(claim_ix);
    instructions
}

/// Split epoch-ordered holdings into consecutive batches, growing each one
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
            "the holding for epoch {} does not fit a transaction on its own",
            holdings[start].epoch
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

fn epoch_range(batch: &[HoldingToClaim]) -> String {
    match (batch.first(), batch.last()) {
        (Some(first), Some(last)) if first.epoch != last.epoch => {
            format!("{}..={}", first.epoch, last.epoch)
        }
        (Some(first), _) => first.epoch.to_string(),
        (None, _) => String::from("none"),
    }
}

fn write_withheld_note(out: &mut impl Write, withheld: &[&[HoldingToClaim]]) -> Result<()> {
    if withheld.is_empty() {
        return Ok(());
    }
    let holding_count = withheld.iter().map(|batch| batch.len()).sum::<usize>();
    let epochs = withheld
        .iter()
        .flat_map(|batch| batch.iter().map(|holding| holding.epoch))
        .collect::<Vec<_>>();
    writeln!(
        out,
        "\nWithheld {} transaction(s) covering {holding_count} holding(s) (epochs {}..={}) \
         because of --max-transactions. Re-run this command once the transactions above have \
         executed, or name those epochs with --subscription-epoch.",
        withheld.len(),
        epochs[0],
        epochs[epochs.len() - 1],
    )?;
    Ok(())
}

/// Discover every outstanding claim holding for `validator_client_rewards_key`/`mint`
/// by probing every holding PDA in
/// `[ceiling_epoch - MAX_DISCOVERY_LOOKBACK, ceiling_epoch]`. Returns the
/// holdings that exist, sorted by epoch.
async fn discover_holdings(
    connection: &SolanaConnection,
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
    let probed = try_fetch_multiple_accounts(connection, &keys)
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
    connection: &SolanaConnection,
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
    let accounts = try_fetch_multiple_accounts(connection, &keys)
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
    use doublezero_solana_client_tools::{
        rpc::NetworkEnvironment, squads::try_encode_vault_transaction,
    };
    use solana_sdk::signature::Keypair;

    use super::*;

    #[derive(Parser)]
    struct Cli {
        #[command(flatten)]
        cmd: ClaimCommand,
    }

    fn parse(extra: &[&str]) -> Result<ClaimCommand, clap::Error> {
        let mint = Pubkey::new_unique().to_string();
        let mut args = vec!["test", "--client-id", "7", "--rewards-token-mint", &mint];
        args.extend_from_slice(extra);
        Cli::try_parse_from(args).map(|cli| cli.cmd)
    }

    fn holdings(count: usize) -> Vec<HoldingToClaim> {
        (0..count)
            .map(|index| HoldingToClaim {
                epoch: 700 + index as u64,
                bump_seed: 255,
                holding_pda: Pubkey::new_unique(),
                pre_balance: 10,
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

    struct Fixture {
        client_id: u16,
        manager_key: Pubkey,
        destination_key: Pubkey,
        rent_beneficiary_key: Pubkey,
        mint_key: Pubkey,
        check_cli_version_ix: Instruction,
    }

    impl Fixture {
        fn new(manager_key: Pubkey) -> Self {
            Self {
                client_id: 7,
                manager_key,
                destination_key: Pubkey::new_unique(),
                rent_beneficiary_key: Pubkey::new_unique(),
                mint_key: Pubkey::new_unique(),
                check_cli_version_ix: super::super::super::build_check_cli_version_instruction()
                    .unwrap(),
            }
        }

        fn claim_ix(&self, batch: &[HoldingToClaim]) -> Instruction {
            build_claim_instruction(
                self.client_id,
                &self.manager_key,
                &self.destination_key,
                &self.rent_beneficiary_key,
                &self.mint_key,
                batch,
            )
            .unwrap()
        }

        fn direct(&self, batch: &[HoldingToClaim], with_price: bool) -> Vec<Instruction> {
            let price_ix = ComputeBudgetInstruction::set_compute_unit_price(1_000);
            direct_instructions(
                &self.check_cli_version_ix,
                self.claim_ix(batch),
                batch.len(),
                with_price.then_some(&price_ix),
            )
        }

        fn vault(
            &self,
            batch: &[HoldingToClaim],
            create_destination_ix: Option<&Instruction>,
        ) -> Vec<Instruction> {
            vault_instructions(
                &self.check_cli_version_ix,
                create_destination_ix,
                self.claim_ix(batch),
            )
        }
    }

    #[test]
    fn test_parses_required_args_with_implicit_destination() {
        let cmd = parse(&["--subscription-epoch", "100"]).unwrap();
        assert_eq!(cmd.client_id, 7);
        assert_eq!(cmd.subscription_epochs, vec![100]);
        assert!(cmd.destination_token_account.is_none());
        assert!(cmd.squads.multisig.is_none());
        assert!(cmd.max_transactions.is_none());
    }

    #[test]
    fn test_parses_explicit_destination() {
        let destination = Pubkey::new_unique();
        let cmd = parse(&["--destination-token-account", &destination.to_string()]).unwrap();
        assert_eq!(cmd.destination_token_account, Some(destination));
    }

    #[test]
    fn test_parses_multisig_and_vault_index() {
        let multisig = Pubkey::new_unique();
        let cmd = parse(&["--multisig", &multisig.to_string(), "--vault-index", "2"]).unwrap();
        assert_eq!(cmd.squads.multisig, Some(multisig));
        assert_eq!(cmd.squads.vault_index, 2);
    }

    #[test]
    fn test_vault_index_requires_multisig() {
        assert!(parse(&["--vault-index", "2"]).is_err());
    }

    #[test]
    fn test_max_transactions_refuses_zero() {
        assert!(parse(&["--max-transactions", "0"]).is_err());
        let cmd = parse(&["--max-transactions", "1"]).unwrap();
        assert_eq!(cmd.max_transactions, NonZeroUsize::new(1));
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
        let wallet_key = Pubkey::new_unique();
        assert!(validate_manager("wallet", &wallet_key, &wallet_key).is_ok());
    }

    #[test]
    fn test_validate_manager_mismatch_names_the_actor_checked() {
        let actor_key = Pubkey::new_unique();
        let manager_key = Pubkey::new_unique();
        for actor in ["wallet", "vault"] {
            let message = validate_manager(actor, &actor_key, &manager_key)
                .unwrap_err()
                .to_string();
            assert!(message.contains("manager mismatch"));
            assert!(
                message.contains(&format!("{actor} is {actor_key}")),
                "{message}"
            );
            assert!(message.contains(&manager_key.to_string()));
        }
    }

    #[test]
    fn test_allows_missing_subscription_epoch() {
        // Omitting --subscription-epoch is valid: the command discovers and
        // claims every outstanding holding for the client and mint.
        let cmd = parse(&[]).unwrap();
        assert!(cmd.subscription_epochs.is_empty());
    }

    #[test]
    fn test_pack_holdings_grows_each_batch_until_it_stops_fitting() {
        let holdings = holdings(7);
        let batches = pack_holdings(&holdings, |batch| Ok(batch.len() <= 3)).unwrap();

        let sizes = batches.iter().map(|batch| batch.len()).collect::<Vec<_>>();
        assert_eq!(sizes, [3, 3, 1]);
        assert_eq!(batches[1][0].epoch, 703);
        assert_eq!(batches[2][0].epoch, 706);
    }

    #[test]
    fn test_pack_holdings_refuses_a_holding_that_fits_nowhere() {
        let holdings = holdings(2);
        let error = pack_holdings(&holdings, |batch| {
            Ok(!batch.iter().any(|holding| holding.epoch == 701))
        })
        .unwrap_err()
        .to_string();
        assert!(error.contains("epoch 701"), "{error}");
    }

    #[test]
    fn test_pack_holdings_of_nothing_is_no_batches() {
        assert!(pack_holdings(&[], |_| Ok(true)).unwrap().is_empty());
    }

    fn direct_batches(wallet: &Wallet, fixture: &Fixture, holdings: &[HoldingToClaim]) -> usize {
        let batches = pack_holdings(holdings, |batch| {
            Ok(wallet.try_transaction_size(&fixture.direct(batch, true))? <= MAX_TRANSACTION_SIZE)
        })
        .unwrap();

        // Every batch fits as sent, with and without a configured price, and
        // the first would not take one more holding.
        for batch in &batches {
            for with_price in [false, true] {
                let size = wallet
                    .try_transaction_size(&fixture.direct(batch, with_price))
                    .unwrap();
                assert!(size <= MAX_TRANSACTION_SIZE, "{size}");
            }
        }
        let overfull = &holdings[..batches[0].len() + 1];
        assert!(
            wallet
                .try_transaction_size(&fixture.direct(overfull, true))
                .unwrap()
                > MAX_TRANSACTION_SIZE
        );

        batches[0].len()
    }

    #[test]
    fn test_direct_transaction_takes_19_holdings_with_one_signer() {
        let wallet = wallet(None);
        let fixture = Fixture::new(wallet.pubkey());
        assert_eq!(direct_batches(&wallet, &fixture, &holdings(91)), 19);
    }

    #[test]
    fn test_direct_transaction_takes_16_holdings_with_a_distinct_fee_payer() {
        let wallet = wallet(Some(Keypair::new()));
        let fixture = Fixture::new(wallet.pubkey());
        assert_eq!(direct_batches(&wallet, &fixture, &holdings(91)), 16);
    }

    fn vault_batches(
        vault_key: &Pubkey,
        fixture: &Fixture,
        holdings: &[HoldingToClaim],
        create_destination_ix: Option<&Instruction>,
    ) -> usize {
        let batches = pack_holdings(holdings, |batch| {
            let instructions = fixture.vault(batch, create_destination_ix);
            let payload = Message::new(&instructions, Some(vault_key)).serialize();
            Ok(payload.len() <= vault_transaction_payload_budget(instructions.len()))
        })
        .unwrap();

        // The checked encoder takes every batch and refuses the first with one
        // more holding, so the trial and the encoder agree.
        for batch in &batches {
            let instructions = fixture.vault(batch, create_destination_ix);
            try_encode_vault_transaction(vault_key, &instructions).unwrap();
            assert_eq!(instructions[0], fixture.check_cli_version_ix);
            if let Some(create_destination_ix) = create_destination_ix {
                assert_eq!(&instructions[1], create_destination_ix);
            }
        }
        let overfull = fixture.vault(&holdings[..batches[0].len() + 1], create_destination_ix);
        assert!(try_encode_vault_transaction(vault_key, &overfull).is_err());

        batches[0].len()
    }

    #[test]
    fn test_vault_payload_takes_12_holdings_into_an_existing_destination() {
        let vault_key = Pubkey::new_unique();
        let fixture = Fixture::new(vault_key);
        assert_eq!(vault_batches(&vault_key, &fixture, &holdings(91), None), 12);
    }

    #[test]
    fn test_vault_payload_takes_10_holdings_when_every_payload_creates_the_destination() {
        let vault_key = Pubkey::new_unique();
        let mut fixture = Fixture::new(vault_key);
        fixture.destination_key = get_associated_token_address(&vault_key, &fixture.mint_key);
        let create_destination_ix = create_associated_token_account_idempotent(
            &vault_key,
            &vault_key,
            &fixture.mint_key,
            &spl_token_interface::ID,
        );
        assert_eq!(
            vault_batches(
                &vault_key,
                &fixture,
                &holdings(91),
                Some(&create_destination_ix)
            ),
            10
        );
    }

    #[test]
    fn test_withheld_note_names_the_count_and_the_epochs() {
        let holdings = holdings(5);
        let batches = pack_holdings(&holdings, |batch| Ok(batch.len() <= 2)).unwrap();
        let (_, withheld) = batches.split_at(1);

        let mut out = Vec::new();
        write_withheld_note(&mut out, withheld).unwrap();
        let note = String::from_utf8(out).unwrap();
        assert!(
            note.contains("Withheld 2 transaction(s) covering 3 holding(s) (epochs 702..=704)"),
            "{note}"
        );

        let mut out = Vec::new();
        write_withheld_note(&mut out, &[]).unwrap();
        assert!(out.is_empty());
    }

    #[test]
    fn test_epoch_range_collapses_a_single_epoch() {
        let holdings = holdings(3);
        assert_eq!(epoch_range(&holdings), "700..=702");
        assert_eq!(epoch_range(&holdings[..1]), "700");
    }
}
