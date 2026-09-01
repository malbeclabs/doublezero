use std::{io::Write, str::FromStr};

use anyhow::{Context, Result, bail};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_solana_client_tools::payer::{TransactionOutcome, Wallet};
use doublezero_solana_sdk::{
    Pubkey,
    shred_subscription::{
        ID,
        instruction::{
            ShredSubscriptionInstructionData, ValidatorOffchainAuthorization,
            account::{
                ConfigureValidatorPublisherRewardsAccounts,
                InitializeValidatorPublisherRewardsAccounts,
            },
        },
        state::{find_shred_reward_token_address, find_validator_publisher_rewards_address},
    },
    try_build_instruction,
};
use solana_compute_budget_interface::ComputeBudgetInstruction;
use solana_sdk::{signature::Signature, signer::Signer};
use spl_associated_token_account_interface::instruction::create_associated_token_account_idempotent;

use super::rewards_mint_arg::RewardsMintArg;

/*
   # Direct path (the `-k` signer keypair must be the validator identity)
   doublezero-solana shreds publisher-rewards configure \
       --node-id <PUBKEY> --rewards-token-owner <WALLET> \
       [--rewards-token-mint <MINT|2z|usdc|wsol>] [-k <KEYPAIR>]

   # Offchain path
   doublezero-solana shreds publisher-rewards configure \
       --node-id <PUBKEY> --rewards-token-owner <WALLET> \
       [--rewards-token-mint <MINT|2z|usdc|wsol>] \
       --signature <BASE58> --deadline-slot <ABS>
*/

#[derive(Debug, Args)]
pub struct ConfigureCommand {
    /// Validator node identity being configured.
    #[arg(long)]
    pub node_id: Pubkey,

    /// Mint to receive rewards in. Must correspond to a registered, enabled
    /// `ShredRewardToken`. Accepts a base58 pubkey or one of the aliases
    /// `2z`, `usdc`, `wsol` (env-aware where applicable). Defaults to `2z`.
    #[arg(long, default_value = "2z")]
    pub rewards_token_mint: RewardsMintArg,

    /// Wallet that will own the ATA that receives rewards.
    #[arg(long)]
    pub rewards_token_owner: Pubkey,

    /// Base58-encoded ed25519 signature produced by the validator identity
    /// keypair via `solana sign-offchain-message`. When omitted, the `-k`
    /// signer keypair must equal `--node-id` and signs the transaction
    /// directly as the validator identity.
    #[arg(long, requires = "deadline_slot")]
    pub signature: Option<String>,

    /// Absolute slot deadline. Must match what was hashed when the signature
    /// was produced (see `prepare-offchain-message`). Required with `--signature`.
    #[arg(long, requires = "signature")]
    pub deadline_slot: Option<u64>,

    #[command(flatten)]
    pub write_opts: crate::command::WriteVerbOptions,
}

/// Resolved auth path after CLI parsing. The variant maps 1:1 to which auth
/// surface the on-chain `ConfigureValidatorPublisherRewards` instruction
/// expects: a Solana transaction signature from `validator_node` (Direct) or
/// an instruction-data ed25519 envelope (Offchain).
#[derive(Debug)]
pub(crate) enum ResolvedAuth {
    Direct,
    Offchain(ValidatorOffchainAuthorization),
}

impl ResolvedAuth {
    pub(crate) fn is_node_signer(&self) -> bool {
        matches!(self, ResolvedAuth::Direct)
    }
}

/// Resolve `ResolvedAuth` from CLI inputs. Pure: no I/O, no network.
///
/// - `node_id`: from `--node-id`, used to validate the direct-path signer match.
/// - `signer_pubkey`: the pubkey of the keypair that signs the transaction
///   (`-k`). With `--fee-payer` overriding the fee payer, this is the
///   signer-of-record, not necessarily the fee payer.
/// - `offchain`: `(--signature, --deadline-slot)` zipped — both present means
///   offchain path, both absent means direct path. Clap's `requires` enforces
///   both-or-neither, so the awkward middle case is unreachable.
///
/// In the direct path (`offchain.is_none()`), the signer signs as the
/// validator identity, so `--node-id` must equal `signer_pubkey`.
pub(crate) fn resolve_auth(
    node_id: Pubkey,
    signer_pubkey: Pubkey,
    offchain: Option<(&str, u64)>,
) -> Result<ResolvedAuth> {
    match offchain {
        Some((sig_b58, deadline_slot)) => {
            let sig = Signature::from_str(sig_b58).with_context(|| {
                "--signature must be a base58-encoded ed25519 signature \
                 (64 bytes / 88 base58 chars)"
            })?;
            let bytes: [u8; 64] = sig
                .as_ref()
                .try_into()
                .with_context(|| "decoded signature is not 64 bytes")?;
            Ok(ResolvedAuth::Offchain(ValidatorOffchainAuthorization {
                deadline_slot,
                signature: bytes,
            }))
        }
        None => {
            if node_id != signer_pubkey {
                bail!(
                    "in the direct path the signer keypair (-k) must be the validator \
                     identity, but its pubkey {signer_pubkey} does not match --node-id {node_id}. \
                     The validator identity keypair lives on the validator host, so the offchain \
                     workflow is usually what you want: \n  \
                     1. Workstation: \
                     `doublezero-solana shreds publisher-rewards prepare-offchain-message ...` \
                     to print the hex blob.\n  \
                     2. Validator host: `solana sign-offchain-message <HEX> \
                     --keypair <validator-identity>` to produce the base58 signature.\n  \
                     3. Workstation: re-run `configure` with \
                     `--signature <BASE58> --deadline-slot <ABS>`.\n\
                     Or, if the validator identity keypair is accessible locally, pass it as \
                     `-k` so its pubkey equals {node_id}."
                );
            }
            Ok(ResolvedAuth::Direct)
        }
    }
}

impl ConfigureCommand {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        if self.node_id == Pubkey::default() {
            bail!("--node-id must not be the default pubkey");
        }
        if self.rewards_token_owner == Pubkey::default() {
            bail!("--rewards-token-owner must not be the default pubkey");
        }

        let wallet = crate::command::build_wallet(ctx, self.write_opts)?;
        let rewards_token_mint = self.rewards_token_mint.resolve(&wallet.connection).await?;
        let wallet_key = wallet.pubkey();
        // When `--fee-payer` is set, the ATA rent must come from the fee
        // payer, not the signer. Otherwise an operator who passed
        // `--fee-payer` because the validator identity is rent-poor would
        // still see the transaction fail at submit. The `Wallet` does not
        // expose a helper for "the pubkey that actually pays this tx", so
        // re-derive it here; a follow-up will hoist this into `Wallet`.
        let funding_key = wallet
            .fee_payer
            .as_ref()
            .map(Signer::pubkey)
            .unwrap_or(wallet_key);

        let offchain = self.signature.as_deref().zip(self.deadline_slot);
        let auth = resolve_auth(self.node_id, wallet_key, offchain)?;
        let is_node_signer = auth.is_node_signer();

        // Capture bumps so the CU budget can be sized from the actual cost
        // of each PDA derivation rather than a single conservative ceiling.
        let (srt_pda, srt_bump) = find_shred_reward_token_address(&rewards_token_mint);
        let (vpr_pda, vpr_bump) = find_validator_publisher_rewards_address(&self.node_id);
        let (rewards_token_ata, ata_create_compute_units) =
            Wallet::ata_address_and_create_compute_units(
                &self.rewards_token_owner,
                &rewards_token_mint,
            );

        writeln!(
            out,
            "Shred subscription - Configure Validator Publisher Rewards"
        )?;
        writeln!(out, "Node ID:           {}", self.node_id)?;
        writeln!(out, "Rewards owner:     {}", self.rewards_token_owner)?;
        writeln!(out, "Rewards mint:      {rewards_token_mint}")?;
        writeln!(out, "Rewards ATA:       {rewards_token_ata}")?;
        writeln!(
            out,
            "Auth path:         {}",
            if is_node_signer { "direct" } else { "offchain" }
        )?;

        // Pre-flight: shred_reward_token must exist + be enabled; auto-init
        // validator publisher rewards if it doesn't exist yet; only push the
        // ATA-create instruction if the ATA isn't already there. Batched into
        // one RPC call.
        let accounts = wallet
            .connection
            .get_multiple_accounts(&[srt_pda, vpr_pda, rewards_token_ata])
            .await
            .context("failed to read pre-flight accounts")?;

        let srt_account = accounts.first().and_then(|a| a.as_ref());
        super::validate_shred_reward_token(&rewards_token_mint, &srt_pda, srt_account)?;
        let vpr_exists = accounts.get(1).and_then(|a| a.as_ref()).is_some();
        let ata_exists = accounts.get(2).and_then(|a| a.as_ref()).is_some();

        // CU budget built incrementally per pushed instruction. The on-chain
        // program re-derives each PDA, and the bump dominates the variation
        // in CU cost — see `Wallet::compute_units_for_bump_seed`. Base costs
        // come from prior runs of these instructions.
        const INIT_VPR_CU_BASE: u32 = 20_000;
        const CONFIGURE_VPR_CU_BASE: u32 = 20_000;
        const ED25519_VERIFY_CU: u32 = 150_000;
        const CHECK_CLI_VERSION_CU: u32 = 5_000;

        let mut compute_unit_limit: u32 = CHECK_CLI_VERSION_CU;
        let mut instructions = vec![super::super::build_check_cli_version_instruction()?];

        if !vpr_exists {
            writeln!(
                out,
                "Validator publisher rewards account missing; will initialize as part of this transaction."
            )?;
            let init_ix = try_build_instruction(
                &ID,
                InitializeValidatorPublisherRewardsAccounts::new(&wallet_key, &self.node_id),
                &ShredSubscriptionInstructionData::InitializeValidatorPublisherRewards(
                    self.node_id,
                ),
            )?;
            instructions.push(init_ix);
            compute_unit_limit += INIT_VPR_CU_BASE + Wallet::compute_units_for_bump_seed(vpr_bump);
        }

        let offchain_authorization = match &auth {
            ResolvedAuth::Direct => None,
            ResolvedAuth::Offchain(a) => Some(a.clone()),
        };
        let configure_ix = try_build_instruction(
            &ID,
            ConfigureValidatorPublisherRewardsAccounts::new(
                &self.node_id,
                &rewards_token_mint,
                is_node_signer,
            ),
            &ShredSubscriptionInstructionData::ConfigureValidatorPublisherRewards {
                rewards_token_owner_key: self.rewards_token_owner,
                offchain_authorization,
            },
        )?;
        instructions.push(configure_ix);
        // Configure re-derives both the VPR and SRT PDAs; the offchain auth
        // path additionally runs an ed25519 verify.
        compute_unit_limit += CONFIGURE_VPR_CU_BASE
            + Wallet::compute_units_for_bump_seed(vpr_bump)
            + Wallet::compute_units_for_bump_seed(srt_bump);
        if !is_node_signer {
            compute_unit_limit += ED25519_VERIFY_CU;
        }

        // Push the ATA-create only when the account doesn't already exist.
        // The idempotent variant is the on-chain race-condition safety net
        // (someone could race us between the read and the submit), not the
        // primary mechanism — skipping it on the happy path keeps the
        // transaction smaller and burns less compute. The fee-payer (or
        // signer, if no `--fee-payer` is set) pays the rent; the account is
        // owned by --rewards-token-owner.
        if !ata_exists {
            writeln!(
                out,
                "Rewards ATA missing; will create as part of this transaction."
            )?;
            instructions.push(create_associated_token_account_idempotent(
                &funding_key,
                &self.rewards_token_owner,
                &rewards_token_mint,
                &spl_token_interface::ID,
            ));
            compute_unit_limit += ata_create_compute_units;
        }

        instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(
            compute_unit_limit,
        ));
        if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
            instructions.push(compute_unit_price_ix.clone());
        }

        // In the direct path the `-k` signer signs both as the transaction
        // signer-of-record (fee payer, when `--fee-payer` is not set) and as
        // the validator identity. In the offchain path the validator
        // identity authorization is carried in instruction data instead, so
        // `-k` only needs to be a signer of the transaction.
        let transaction = wallet.new_transaction(&instructions).await?;

        let tx_outcome = wallet.send_or_simulate_transaction(&transaction).await?;
        let configure_executed = matches!(tx_outcome, TransactionOutcome::Executed(_));
        if let TransactionOutcome::Executed(tx_sig) = tx_outcome {
            writeln!(out, "Configured validator publisher rewards: {tx_sig}")?;
            wallet.write_verbose_output(out, &[tx_sig]).await?;
        }

        // Post-configure: distribute this validator's pending rewards for
        // any recent subscription epoch where accumulation has completed
        // but distribute hasn't run for this leaf yet. Skipped under
        // dry-run (the configure tx never actually wrote the ValidatorPublisherRewards
        // state we would distribute under).
        if configure_executed {
            let distribute_result = async {
                let network_env = wallet
                    .connection
                    .try_network_environment()
                    .await
                    .context("detecting network environment")?;
                super::distribute::try_distribute_pending(
                    &wallet,
                    &wallet.connection,
                    &self.node_id,
                    &self.rewards_token_owner,
                    network_env,
                    out,
                )
                .await
            }
            .await;
            match distribute_result {
                Ok(outcome) => {
                    writeln!(
                        out,
                        "\nDistribute pass complete: {} distributed, {} failed.",
                        outcome.distributed, outcome.failed,
                    )?;
                }
                Err(error) => {
                    eprintln!(
                        "\nDistribute pass failed (configure already landed; safe to re-run): {error:#}"
                    );
                }
            }
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use clap::Parser;
    use solana_sdk::signature::{Keypair, Signer};

    use super::*;

    #[derive(Debug, Parser)]
    struct TestCli {
        #[command(flatten)]
        cmd: ConfigureCommand,
    }

    #[test]
    fn signature_without_deadline_errors() {
        let result = TestCli::try_parse_from([
            "test",
            "--node-id",
            "11111111111111111111111111111111",
            "--rewards-token-owner",
            "11111111111111111111111111111111",
            "--signature",
            "5xyz",
        ]);
        assert!(result.is_err());
    }

    #[test]
    fn deadline_without_signature_errors() {
        let result = TestCli::try_parse_from([
            "test",
            "--node-id",
            "11111111111111111111111111111111",
            "--rewards-token-owner",
            "11111111111111111111111111111111",
            "--deadline-slot",
            "100",
        ]);
        assert!(result.is_err());
    }

    #[test]
    fn missing_node_id_errors() {
        let result = TestCli::try_parse_from([
            "test",
            "--rewards-token-owner",
            "11111111111111111111111111111111",
        ]);
        assert!(result.is_err());
    }

    #[test]
    fn resolve_auth_direct_path_matches_signer() {
        let signer = Pubkey::new_unique();
        let auth = resolve_auth(signer, signer, None).expect("matching pubkey resolves to Direct");
        assert!(auth.is_node_signer());
        assert!(matches!(auth, ResolvedAuth::Direct));
    }

    #[test]
    fn resolve_auth_direct_path_node_id_signer_mismatch_errors() {
        let signer = Pubkey::new_unique();
        let other = Pubkey::new_unique();
        let err = resolve_auth(other, signer, None).expect_err("mismatched node_id must error");
        let msg = err.to_string();
        assert!(msg.contains("does not match --node-id"), "got: {msg}");
        // The remedy must lead with the offchain workflow because the
        // validator identity keypair lives on the validator host.
        assert!(
            msg.contains("prepare-offchain-message"),
            "expected message to point at prepare-offchain-message, got: {msg}"
        );
    }

    #[test]
    fn resolve_auth_offchain_path_happy() {
        let node_id = Pubkey::new_unique();
        let signer = Pubkey::new_unique();
        let kp = Keypair::new();
        let sig = kp.sign_message(b"anything");
        let sig_b58 = sig.to_string();
        let auth = resolve_auth(node_id, signer, Some((&sig_b58, 42_000)))
            .expect("offchain path resolves");
        assert!(!auth.is_node_signer());
        match auth {
            ResolvedAuth::Offchain(envelope) => {
                assert_eq!(envelope.deadline_slot, 42_000);
                assert_eq!(envelope.signature, <[u8; 64]>::from(sig));
            }
            _ => panic!("expected Offchain"),
        }
    }

    #[test]
    fn resolve_auth_offchain_path_invalid_signature_errors() {
        let node_id = Pubkey::new_unique();
        let signer = Pubkey::new_unique();
        let err = resolve_auth(node_id, signer, Some(("not-base58!!", 1)))
            .expect_err("bad base58 must error");
        assert!(err.to_string().contains("base58-encoded ed25519 signature"));
    }
}
