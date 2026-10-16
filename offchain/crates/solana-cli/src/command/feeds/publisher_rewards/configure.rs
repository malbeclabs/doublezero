use std::io::Write;

use anyhow::{Result, ensure};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_solana_client_tools::payer::{TransactionOutcome, Wallet};
use doublezero_solana_sdk::{
    feed_subscription::{
        ID,
        instruction::{
            FeedSubscriptionInstructionData, ValidatorOffchainAuthorization,
            account::ConfigureFeedPublisherRewardsAccounts,
        },
        state::find_feed_publisher_rewards_address,
    },
    try_build_instruction,
};
use solana_compute_budget_interface::ComputeBudgetInstruction;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

use super::rewards_mint_arg::RewardsMintArg;

/*
   # Direct path (the -k signer must be the validator identity)
   doublezero-solana feeds publisher-rewards configure \
       --node-id <PUBKEY> --rewards-token-owner <WALLET> \
       [--rewards-token-mint <MINT|2z|usdc|wsol>] [-k <KEYPAIR>]

   # Offchain path
   doublezero-solana feeds publisher-rewards configure \
       --node-id <PUBKEY> --rewards-token-owner <WALLET> \
       [--rewards-token-mint <MINT|2z|usdc|wsol>] \
       --signature <BASE58> --deadline-slot <ABS>
*/

// Measured against the program at a bump of 255, with headroom: creating the account used 13,540,
// and verifying an offchain signature added 28,444.
const CONFIGURE_COMPUTE_UNITS: u32 = 20_000;
const VERIFY_SIGNATURE_COMPUTE_UNITS: u32 = 40_000;

#[derive(Debug, Args)]
pub struct ConfigureCommand {
    /// Validator identity being configured.
    #[arg(long)]
    node_id: Pubkey,

    /// Mint to be paid in: a base58 pubkey or one of `2z`, `usdc`, `wsol`.
    #[arg(long, default_value = "2z")]
    rewards_token_mint: RewardsMintArg,

    /// Wallet whose associated token account is paid.
    #[arg(long = "rewards-token-owner", value_name = "REWARDS_TOKEN_OWNER")]
    rewards_token_owner_key: Pubkey,

    /// Base58 signature the validator identity made with `solana sign-offchain-message` over the
    /// `prepare-offchain-message` hex. Without it, the `-k` signer must be the validator identity.
    #[arg(long, requires = "deadline_slot")]
    signature: Option<Signature>,

    /// Deadline slot the signed message was prepared with.
    #[arg(long, requires = "signature")]
    deadline_slot: Option<u64>,

    #[command(flatten)]
    write_opts: crate::command::WriteVerbOptions,
}

impl ConfigureCommand {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        ensure!(
            self.node_id != Pubkey::default(),
            "--node-id must not be the default pubkey"
        );
        ensure!(
            self.rewards_token_owner_key != Pubkey::default(),
            "--rewards-token-owner must not be the default pubkey"
        );

        let wallet = crate::command::build_wallet(ctx, self.write_opts)?;
        let wallet_key = wallet.pubkey();
        let offchain_authorization = try_offchain_authorization(
            &self.node_id,
            &wallet_key,
            self.signature,
            self.deadline_slot,
        )?;
        let is_node_signer = offchain_authorization.is_none();

        let rewards_token_mint_key = self
            .rewards_token_mint
            .try_resolve(&wallet.connection)
            .await?;
        super::try_check_feed_publishers_reward_token(&wallet.connection, &rewards_token_mint_key)
            .await?;

        writeln!(out, "Feed subscription - Configure Feed Publisher Rewards")?;
        writeln!(out, "Node ID:        {}", self.node_id)?;
        writeln!(out, "Rewards owner:  {}", self.rewards_token_owner_key)?;
        writeln!(out, "Rewards mint:   {rewards_token_mint_key}")?;
        writeln!(
            out,
            "Auth path:      {}",
            if is_node_signer { "direct" } else { "offchain" }
        )?;

        // The signer funds the account when the program creates it, which also keeps the signer
        // required on the offchain path when `--fee-payer` pays the fee.
        let configure_ix = try_build_instruction(
            &ID,
            ConfigureFeedPublisherRewardsAccounts::new(
                &self.node_id,
                &rewards_token_mint_key,
                &wallet_key,
                is_node_signer,
            ),
            &FeedSubscriptionInstructionData::ConfigureFeedPublisherRewards {
                rewards_token_owner_key: self.rewards_token_owner_key,
                offchain_authorization,
            },
        )?;

        let feed_publisher_rewards_bump = find_feed_publisher_rewards_address(&self.node_id).1;
        let mut compute_unit_limit = CONFIGURE_COMPUTE_UNITS
            + Wallet::compute_units_for_bump_seed(feed_publisher_rewards_bump);
        if !is_node_signer {
            compute_unit_limit += VERIFY_SIGNATURE_COMPUTE_UNITS;
        }
        let mut instructions = vec![
            configure_ix,
            ComputeBudgetInstruction::set_compute_unit_limit(compute_unit_limit),
        ];
        instructions.extend(wallet.compute_unit_price_ix.clone());

        let transaction = wallet.new_transaction(&instructions).await?;
        let tx_outcome = wallet.send_or_simulate_transaction(&transaction).await?;

        if let TransactionOutcome::Executed(tx_sig) = tx_outcome {
            writeln!(out, "Configured feed publisher rewards: {tx_sig}")?;
            wallet.write_verbose_output(out, &[tx_sig]).await?;
        }

        Ok(())
    }
}

/// The authorization the instruction carries. Without one the validator identity signs the
/// transaction, so the signer has to be it.
fn try_offchain_authorization(
    node_id: &Pubkey,
    signer_key: &Pubkey,
    signature: Option<Signature>,
    deadline_slot: Option<u64>,
) -> Result<Option<ValidatorOffchainAuthorization>> {
    match signature.zip(deadline_slot) {
        Some((signature, deadline_slot)) => Ok(Some(ValidatorOffchainAuthorization {
            deadline_slot,
            signature: signature.into(),
        })),
        None => {
            ensure!(
                signer_key == node_id,
                "the signer {signer_key} is not the validator identity {node_id}. Run \
                 `doublezero-solana feeds publisher-rewards prepare-offchain-message`, sign its \
                 hex on the validator host with `solana sign-offchain-message`, then pass \
                 `--signature` and `--deadline-slot` here"
            );
            Ok(None)
        }
    }
}

#[cfg(test)]
mod tests {
    use clap::{Parser, error::ErrorKind};
    use solana_sdk::signature::{Keypair, Signer};

    use super::*;

    #[derive(Debug, Parser)]
    struct TestCli {
        #[command(flatten)]
        cmd: ConfigureCommand,
    }

    #[test]
    fn test_signature_and_deadline_slot_require_each_other() {
        let signature = Keypair::new().sign_message(b"message").to_string();
        for extra in [["--signature", &signature], ["--deadline-slot", "100"]] {
            let mut args = vec![
                "test",
                "--node-id",
                "11111111111111111111111111111111",
                "--rewards-token-owner",
                "11111111111111111111111111111111",
            ];
            args.extend_from_slice(&extra);
            assert_eq!(
                TestCli::try_parse_from(args).unwrap_err().kind(),
                ErrorKind::MissingRequiredArgument
            );
        }
    }

    #[test]
    fn test_offchain_authorization_for_each_path() {
        let node_id = Pubkey::new_unique();
        assert_eq!(
            try_offchain_authorization(&node_id, &node_id, None, None).unwrap(),
            None
        );

        let signer_key = Pubkey::new_unique();
        assert_eq!(
            try_offchain_authorization(&node_id, &signer_key, None, None)
                .unwrap_err()
                .to_string(),
            format!(
                "the signer {signer_key} is not the validator identity {node_id}. Run \
                 `doublezero-solana feeds publisher-rewards prepare-offchain-message`, sign its \
                 hex on the validator host with `solana sign-offchain-message`, then pass \
                 `--signature` and `--deadline-slot` here"
            )
        );

        let signature = Keypair::new().sign_message(b"message");
        assert_eq!(
            try_offchain_authorization(&node_id, &signer_key, Some(signature), Some(42_000))
                .unwrap(),
            Some(ValidatorOffchainAuthorization {
                deadline_slot: 42_000,
                signature: signature.into(),
            })
        );
    }
}
