use std::str::FromStr;

use anyhow::{Context, Result, bail};
use clap::Args;
use doublezero_solana_client_tools::{
    account::zero_copy::ZeroCopyAccountOwnedData,
    payer::{SolanaPayerOptions, TransactionOutcome, Wallet},
    rpc::SolanaConnection,
};
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
        state::{
            ShredRewardToken, find_shred_reward_token_address,
            find_validator_publisher_rewards_address,
        },
    },
    try_build_instruction,
};
use solana_sdk::{compute_budget::ComputeBudgetInstruction, signature::Signature};
use spl_associated_token_account_interface::{
    address::get_associated_token_address, instruction::create_associated_token_account_idempotent,
};

use super::rewards_mint_arg::RewardsMintArg;

/*
   # Direct path (fee-payer keypair `-k` doubles as the validator identity)
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
    /// keypair via `solana sign-offchain-message`. When omitted, the
    /// fee-payer keypair (`-k`) is used as the validator identity and signs
    /// the transaction directly.
    #[arg(long, requires = "deadline_slot")]
    pub signature: Option<String>,

    /// Absolute slot deadline. Must match what was hashed when the signature
    /// was produced (see `prepare-offchain-message`). Required with `--signature`.
    #[arg(long, requires = "signature")]
    pub deadline_slot: Option<u64>,

    #[command(flatten)]
    pub solana_payer_options: SolanaPayerOptions,
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
/// - `node_id`: from `--node-id`, used to validate the direct-path fee-payer match.
/// - `fee_payer_pubkey`: the pubkey of the wallet that pays for the transaction.
/// - `offchain`: `(--signature, --deadline-slot)` zipped — both present means
///   offchain path, both absent means direct path. Clap's `requires` enforces
///   both-or-neither, so the awkward middle case is unreachable.
///
/// In the direct path (`offchain.is_none()`), the fee-payer signs as the
/// validator identity, so `--node-id` must equal `fee_payer_pubkey`.
pub(crate) fn resolve_auth(
    node_id: Pubkey,
    fee_payer_pubkey: Pubkey,
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
            if node_id != fee_payer_pubkey {
                bail!(
                    "in the direct path the fee-payer keypair signs as the validator identity, \
                     but its pubkey {fee_payer_pubkey} does not match --node-id {node_id}. \
                     Either pass a fee-payer keypair (-k) whose pubkey is {node_id}, \
                     or use the offchain path with --signature + --deadline-slot."
                );
            }
            Ok(ResolvedAuth::Direct)
        }
    }
}

impl ConfigureCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        if self.rewards_token_owner == Pubkey::default() {
            bail!("--rewards-token-owner must not be the default pubkey");
        }

        let solana_connection =
            SolanaConnection::from(self.solana_payer_options.connection_options.clone());
        let rewards_token_mint = self.rewards_token_mint.resolve(&solana_connection).await?;

        let dz_connection = self
            .solana_payer_options
            .connection_options
            .clone()
            .into_shred_subscription_connection();
        let mut wallet = Wallet::try_from(self.solana_payer_options)?;
        wallet.connection = dz_connection;
        let wallet_key = wallet.pubkey();

        let offchain = self.signature.as_deref().zip(self.deadline_slot);
        let auth = resolve_auth(self.node_id, wallet_key, offchain)?;
        let is_node_signer = auth.is_node_signer();

        let rewards_token_ata =
            get_associated_token_address(&self.rewards_token_owner, &rewards_token_mint);

        println!("Shred subscription - Configure Validator Publisher Rewards");
        println!("Node ID:           {}", self.node_id);
        println!("Rewards owner:     {}", self.rewards_token_owner);
        println!("Rewards mint:      {rewards_token_mint}");
        println!("Rewards ATA:       {rewards_token_ata}");
        println!(
            "Auth path:         {}",
            if is_node_signer { "direct" } else { "offchain" }
        );

        // Pre-flight: shred_reward_token must exist + be enabled; auto-init
        // validator publisher rewards if it doesn't exist yet.
        let srt_pda = find_shred_reward_token_address(&rewards_token_mint).0;
        let vpr_pda = find_validator_publisher_rewards_address(&self.node_id).0;
        let accounts = wallet
            .connection
            .get_multiple_accounts(&[srt_pda, vpr_pda])
            .await
            .context("failed to read pre-flight accounts")?;

        let srt_account = accounts.first().and_then(|a| a.as_ref()).with_context(|| {
            format!("rewards token mint {rewards_token_mint} is not a registered ShredRewardToken")
        })?;
        let srt = ZeroCopyAccountOwnedData::<ShredRewardToken>::from_account(srt_account)
            .with_context(|| format!("ShredRewardToken account at {srt_pda} is malformed"))?;
        if !srt.is_enabled() {
            bail!(
                "rewards token mint {rewards_token_mint} is registered but disabled — \
                 pick an enabled mint or wait for the admin to re-enable it"
            );
        }
        let vpr_exists = accounts.get(1).and_then(|a| a.as_ref()).is_some();

        // Build instructions.
        let mut instructions = vec![super::super::build_check_cli_version_instruction()?];

        if !vpr_exists {
            println!(
                "Validator publisher rewards account missing; will initialize as part of this transaction."
            );
            let init_ix = try_build_instruction(
                &ID,
                InitializeValidatorPublisherRewardsAccounts::new(&wallet_key, &self.node_id),
                &ShredSubscriptionInstructionData::InitializeValidatorPublisherRewards(
                    self.node_id,
                ),
            )?;
            instructions.push(init_ix);
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

        // Ensure the rewards ATA exists so payouts to this validator are
        // deliverable. Idempotent: no-op if the ATA is already there. The
        // fee-payer pays the rent; the account is owned by --rewards-token-owner.
        instructions.push(create_associated_token_account_idempotent(
            &wallet_key,
            &self.rewards_token_owner,
            &rewards_token_mint,
            &spl_token_interface::ID,
        ));

        // CU budget: init (~20k) + configure (~20k baseline) + ed25519 verify
        // (~150k headroom) + create-ATA (~25k). Conservative single budget.
        instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(250_000));
        if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
            instructions.push(compute_unit_price_ix.clone());
        }

        // Single-signer transaction: the fee-payer keypair signs both as
        // fee-payer and (in the direct path) as the validator identity. In
        // the offchain path the on-chain validator-identity authorization is
        // carried in instruction data instead.
        let transaction = wallet.new_transaction(&instructions).await?;

        let tx_outcome = wallet.send_or_simulate_transaction(&transaction).await?;
        if let TransactionOutcome::Executed(tx_sig) = tx_outcome {
            println!("Configured validator publisher rewards: {tx_sig}");
            wallet.print_verbose_output(&[tx_sig]).await?;
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
    fn resolve_auth_direct_path_matches_fee_payer() {
        let fee_payer = Pubkey::new_unique();
        let auth =
            resolve_auth(fee_payer, fee_payer, None).expect("matching pubkey resolves to Direct");
        assert!(auth.is_node_signer());
        assert!(matches!(auth, ResolvedAuth::Direct));
    }

    #[test]
    fn resolve_auth_direct_path_node_id_fee_payer_mismatch_errors() {
        let fee_payer = Pubkey::new_unique();
        let other = Pubkey::new_unique();
        let err = resolve_auth(other, fee_payer, None).expect_err("mismatched node_id must error");
        let msg = err.to_string();
        assert!(msg.contains("does not match --node-id"), "got: {msg}");
    }

    #[test]
    fn resolve_auth_offchain_path_happy() {
        let node_id = Pubkey::new_unique();
        let fee_payer = Pubkey::new_unique();
        let kp = Keypair::new();
        let sig = kp.sign_message(b"anything");
        let sig_b58 = sig.to_string();
        let auth = resolve_auth(node_id, fee_payer, Some((&sig_b58, 42_000)))
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
        let fee_payer = Pubkey::new_unique();
        let err = resolve_auth(node_id, fee_payer, Some(("not-base58!!", 1)))
            .expect_err("bad base58 must error");
        assert!(err.to_string().contains("base58-encoded ed25519 signature"));
    }
}
