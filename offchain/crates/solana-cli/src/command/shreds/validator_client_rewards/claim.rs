use anyhow::{Context, Result, anyhow, bail};
use clap::Args;
use doublezero_solana_client_tools::payer::{SolanaPayerOptions, TransactionOutcome, Wallet};
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
    commitment_config::CommitmentConfig, compute_budget::ComputeBudgetInstruction,
    instruction::AccountMeta, program_pack::Pack, pubkey::Pubkey,
};
use spl_associated_token_account_interface::address::get_associated_token_address;

/*
   doublezero-solana shreds validator-client-rewards claim \
       --client-id <ID> --rewards-token-mint <PUBKEY> \
       --subscription-epoch <EPOCH> [--subscription-epoch <EPOCH> ...] \
       [--destination-token-account <PUBKEY>]
*/

#[derive(Debug, Args)]
pub struct ClaimCommand {
    /// Validator client ID.
    #[arg(long)]
    pub client_id: u16,
    /// Token mint that holdings are denominated in.
    #[arg(long)]
    pub rewards_token_mint: Pubkey,
    /// One or more subscription epochs to claim.
    #[arg(long = "subscription-epoch", required = true, num_args = 1..)]
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

pub(crate) fn validate_manager(wallet: &Pubkey, vcr_manager: &Pubkey) -> Result<()> {
    if wallet != vcr_manager {
        return Err(anyhow!(
            "manager mismatch: wallet is {wallet}, VCR manager is {vcr_manager}"
        ));
    }
    Ok(())
}

/// Upper bound on epochs per claim tx. Each `ClaimHoldingId` adds 9 bytes of
/// instruction data and the holding account adds 32 bytes to the account list,
/// so beyond ~20 the tx blows past the 1232-byte packet limit. 16 is a
/// conservative cap that leaves room for the destination/rent/program-config
/// accounts and the CheckCliVersion ix.
pub(crate) const MAX_CLAIM_EPOCHS_PER_TX: usize = 16;

impl ClaimCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        if self.subscription_epochs.len() > MAX_CLAIM_EPOCHS_PER_TX {
            bail!(
                "too many --subscription-epoch values ({}); max {} per tx. Split into multiple `claim` calls.",
                self.subscription_epochs.len(),
                MAX_CLAIM_EPOCHS_PER_TX
            );
        }

        let dz_connection = self
            .solana_payer_options
            .connection_options
            .clone()
            .into_shred_subscription_connection();
        let mut wallet = Wallet::try_from(self.solana_payer_options)?;
        wallet.connection = dz_connection;
        let wallet_key = wallet.pubkey();

        // Derive every PDA we need.
        let vcr_key = find_validator_client_rewards_address(self.client_id).0;
        let program_config_key = find_program_config_address().0;
        let holding_keys: Vec<Pubkey> = self
            .subscription_epochs
            .iter()
            .map(|epoch| find_claim_holding_address(&vcr_key, *epoch, &self.rewards_token_mint).0)
            .collect();

        // Single getMultipleAccounts call: VCR, ProgramConfig, every holding.
        let mut all_keys = vec![vcr_key, program_config_key];
        all_keys.extend(holding_keys.iter().copied());
        let accounts = wallet
            .connection
            .get_multiple_accounts(&all_keys)
            .await
            .with_context(|| "fetching VCR + program config + holdings")?;

        let vcr_account = accounts.first().and_then(|a| a.as_ref()).ok_or_else(|| {
            anyhow!(
                "validator client rewards not initialized for client-id {} (PDA {})",
                self.client_id,
                vcr_key
            )
        })?;
        let vcr_info = parse_validator_client_rewards(&vcr_account.data)
            .ok_or_else(|| anyhow!("failed to parse ValidatorClientRewards at {vcr_key}"))?;
        validate_manager(&wallet_key, &vcr_info.manager_key)?;

        let cfg_account = accounts
            .get(1)
            .and_then(|a| a.as_ref())
            .ok_or_else(|| anyhow!("ProgramConfig {program_config_key} not found onchain"))?;
        let rent_beneficiary = parse_program_config_shred_oracle_key(&cfg_account.data)
            .ok_or_else(|| anyhow!("failed to parse shred_oracle_key from ProgramConfig"))?;

        // Validate every holding. Track per-holding pre-claim balances so the
        // post-tx output can report what was drained from each.
        let mut missing: Vec<u64> = Vec::new();
        let mut wrong_owner: Vec<(u64, Pubkey)> = Vec::new();
        let mut wrong_mint: Vec<(u64, Pubkey)> = Vec::new();
        let mut zero_balance: Vec<u64> = Vec::new();
        let mut total_drained: u64 = 0;
        // (epoch, holding_pda, pre_claim_balance)
        let mut pre_claim_balances: Vec<(u64, Pubkey, u64)> = Vec::new();
        for ((epoch, key), maybe_acct) in self
            .subscription_epochs
            .iter()
            .zip(holding_keys.iter())
            .zip(accounts.iter().skip(2))
        {
            match maybe_acct.as_ref() {
                None => missing.push(*epoch),
                Some(acct) => {
                    if acct.owner != spl_token_interface::ID {
                        wrong_owner.push((*epoch, acct.owner));
                        continue;
                    }
                    match spl_token_interface::state::Account::unpack(&acct.data) {
                        Ok(token) => {
                            if token.mint != self.rewards_token_mint {
                                wrong_mint.push((*epoch, token.mint));
                                continue;
                            }
                            if token.amount == 0 {
                                zero_balance.push(*epoch);
                            }
                            total_drained = total_drained.saturating_add(token.amount);
                            pre_claim_balances.push((*epoch, *key, token.amount));
                        }
                        Err(err) => bail!("holding {key} (epoch {epoch}) failed to unpack: {err}"),
                    }
                }
            }
        }
        if !missing.is_empty() || !wrong_owner.is_empty() || !wrong_mint.is_empty() {
            let mut issues: Vec<String> = Vec::new();
            if !missing.is_empty() {
                issues.push(format!(
                    "  - holdings not initialized for epochs: {missing:?}. Run `shreds validator-client-rewards init-holding ...` first."
                ));
            }
            if !wrong_owner.is_empty() {
                let epochs: Vec<u64> = wrong_owner.iter().map(|(e, _)| *e).collect();
                issues.push(format!(
                    "  - holdings for epochs {epochs:?} are not SPL token accounts (epoch, owner): {wrong_owner:?}"
                ));
            }
            if !wrong_mint.is_empty() {
                issues.push(format!(
                    "  - holdings for the following epochs are for the wrong mint (epoch, found_mint): {wrong_mint:?}"
                ));
            }
            bail!("claim holdings have issues:\n{}", issues.join("\n"));
        }
        for epoch in &zero_balance {
            eprintln!(
                "warning: epoch {epoch} holding has 0 balance; will still close and recover rent"
            );
        }

        // Resolve destination ATA and validate it.
        let destination = resolve_destination(
            &wallet_key,
            &self.rewards_token_mint,
            self.destination_token_account,
        );
        let dest_account = wallet
            .connection
            .get_account_with_commitment(&destination, CommitmentConfig::confirmed())
            .await
            .with_context(|| format!("fetching destination token account {destination}"))?
            .value
            .ok_or_else(|| {
                anyhow!(
                    "destination token account {destination} does not exist. \
                     Run: `spl-token create-account --owner {wallet_key} {} --fee-payer {wallet_key}`",
                    self.rewards_token_mint
                )
            })?;
        if dest_account.owner != spl_token_interface::ID {
            bail!(
                "destination {destination} is not an SPL token account (owner = {})",
                dest_account.owner
            );
        }
        let dest_token = spl_token_interface::state::Account::unpack(&dest_account.data)
            .with_context(|| format!("unpacking destination token account {destination}"))?;
        if dest_token.mint != self.rewards_token_mint {
            bail!(
                "destination {destination} mint mismatch: expected {}, found {}",
                self.rewards_token_mint,
                dest_token.mint
            );
        }

        // Build the holding-id payload (re-derive bumps).
        let claim_holding_ids: Vec<ClaimHoldingId> = self
            .subscription_epochs
            .iter()
            .map(|epoch| {
                let (_addr, bump) =
                    find_claim_holding_address(&vcr_key, *epoch, &self.rewards_token_mint);
                ClaimHoldingId {
                    subscription_epoch: *epoch,
                    bump_seed: bump,
                }
            })
            .collect();

        println!(
            "Shred subscription - Claim Validator Client Rewards (client_id={}, mint={}, epochs={})",
            self.client_id,
            self.rewards_token_mint,
            self.subscription_epochs
                .iter()
                .map(u64::to_string)
                .collect::<Vec<_>>()
                .join(",")
        );
        println!("  manager       : {wallet_key}");
        println!("  destination   : {destination}");
        println!("  rent recovers : {rent_beneficiary}");

        // Build the claim instruction.
        let claim_accounts = ClaimValidatorClientRewardsAccounts::new(
            self.client_id,
            &wallet_key,
            &destination,
            &rent_beneficiary,
            &self.rewards_token_mint,
            &self.subscription_epochs,
        );
        let metas: Vec<AccountMeta> = claim_accounts.into();
        let ix = try_build_instruction(
            &ID,
            metas,
            &ShredSubscriptionInstructionData::ClaimValidatorClientRewards(claim_holding_ids),
        )?;

        let mut instructions = vec![super::super::build_check_cli_version_instruction()?, ix];

        // ~30k CU per holding (token transfer + close + state decrement), plus
        // the check-cli-version ix. Bounded by MAX_CLAIM_EPOCHS_PER_TX.
        let cu_limit: u32 = 30_000u32.saturating_mul(self.subscription_epochs.len() as u32 + 1);
        instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(cu_limit));
        if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
            instructions.push(compute_unit_price_ix.clone());
        }

        let transaction = wallet.new_transaction(&instructions).await?;
        let tx_outcome = wallet.send_or_simulate_transaction(&transaction).await?;

        if let TransactionOutcome::Executed(tx_sig) = tx_outcome {
            println!("Claimed: {tx_sig}");
            // The on-chain handler transfers the full balance of each holding,
            // but the balances reported here were read pre-tx — if a top-up
            // landed between the read and our claim, the actual drained amount
            // is higher. To get the authoritative number, diff the destination
            // ATA balance before and after.
            for (epoch, holding_pda, drained) in &pre_claim_balances {
                println!("  epoch {epoch}: {drained} from {holding_pda} (pre-claim)");
            }
            println!("Pre-claim total: {total_drained}");

            // Re-fetch the VCR to report the post-tx claim_holding_count.
            let post_count = match wallet
                .connection
                .get_account_with_commitment(&vcr_key, CommitmentConfig::confirmed())
                .await
            {
                Ok(resp) => resp.value.and_then(|acct| {
                    parse_validator_client_rewards(&acct.data).map(|i| i.claim_holding_count)
                }),
                Err(err) => {
                    eprintln!("warning: post-claim VCR re-fetch failed: {err}");
                    None
                }
            };
            match post_count {
                Some(count) => println!("Remaining claim holding count: {count}"),
                None => println!("Remaining claim holding count: (unavailable)"),
            }

            wallet.print_verbose_output(&[tx_sig]).await?;
        }

        Ok(())
    }
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
    fn parses_required_args_with_implicit_destination() {
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
        assert_eq!(cli.cmd.subscription_epochs, vec![100u64]);
        assert!(cli.cmd.destination_token_account.is_none());
    }

    #[test]
    fn parses_explicit_destination() {
        let mint = Pubkey::new_unique();
        let dest = Pubkey::new_unique();
        let cli = Cli::try_parse_from([
            "test",
            "--client-id",
            "7",
            "--rewards-token-mint",
            &mint.to_string(),
            "--subscription-epoch",
            "100",
            "--destination-token-account",
            &dest.to_string(),
        ])
        .unwrap();
        assert_eq!(cli.cmd.destination_token_account, Some(dest));
    }

    #[test]
    fn resolve_destination_uses_override_when_provided() {
        let manager = Pubkey::new_unique();
        let mint = Pubkey::new_unique();
        let override_dest = Pubkey::new_unique();
        assert_eq!(
            resolve_destination(&manager, &mint, Some(override_dest)),
            override_dest
        );
    }

    #[test]
    fn resolve_destination_defaults_to_ata() {
        let manager = Pubkey::new_unique();
        let mint = Pubkey::new_unique();
        let expected = get_associated_token_address(&manager, &mint);
        assert_eq!(resolve_destination(&manager, &mint, None), expected);
    }

    #[test]
    fn validate_manager_matches() {
        let wallet = Pubkey::new_unique();
        assert!(validate_manager(&wallet, &wallet).is_ok());
    }

    #[test]
    fn validate_manager_mismatch() {
        let wallet = Pubkey::new_unique();
        let manager = Pubkey::new_unique();
        let err = validate_manager(&wallet, &manager).unwrap_err();
        let msg = format!("{err}");
        assert!(msg.contains("manager mismatch"));
        assert!(msg.contains(&wallet.to_string()));
        assert!(msg.contains(&manager.to_string()));
    }

    #[test]
    fn rejects_missing_subscription_epoch() {
        let mint = Pubkey::new_unique();
        let result = Cli::try_parse_from([
            "test",
            "--client-id",
            "7",
            "--rewards-token-mint",
            &mint.to_string(),
        ]);
        assert!(result.is_err());
    }

    #[test]
    fn max_claim_epochs_keeps_tx_under_packet_limit() {
        // Sanity-check the cap: at MAX_CLAIM_EPOCHS_PER_TX, the holding-id
        // payload + per-holding account metas should stay well under the
        // 1232-byte Solana packet limit (accounting for the ~256 bytes of
        // fixed overhead from signature/header/fixed-accounts/blockhash).
        let payload_per_epoch = 9; // ClaimHoldingId = u64 + u8
        let account_meta_per_epoch = 32; // one Pubkey per holding
        let approx_per_epoch = payload_per_epoch + account_meta_per_epoch;
        let approx_overhead = 256;
        let total = approx_overhead + approx_per_epoch * MAX_CLAIM_EPOCHS_PER_TX;
        assert!(
            total < 1232,
            "MAX_CLAIM_EPOCHS_PER_TX={MAX_CLAIM_EPOCHS_PER_TX} produces approx tx size {total} >= 1232"
        );
    }
}
