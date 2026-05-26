pub mod configure;
pub mod init;
pub mod prepare_offchain_message;
pub mod rewards_mint_arg;
pub mod show;

use anyhow::{Context, Result, bail};
use clap::{Args, Subcommand};
use doublezero_solana_client_tools::account::zero_copy::ZeroCopyAccountOwnedData;
use doublezero_solana_sdk::{Pubkey, shred_subscription::state::ShredRewardToken};
use solana_sdk::account::Account;

#[derive(Debug, Args)]
pub struct PublisherRewardsCommand {
    #[command(subcommand)]
    pub command: PublisherRewardsSubcommand,
}

#[derive(Debug, Subcommand)]
pub enum PublisherRewardsSubcommand {
    /// Initialize the ValidatorPublisherRewards PDA (permissionless).
    Init(init::InitCommand),
    /// Print the hex blob to be signed via `solana sign-offchain-message`.
    PrepareOffchainMessage(prepare_offchain_message::PrepareOffchainMessageCommand),
    /// Configure the ValidatorPublisherRewards PDA (auto-inits if missing).
    Configure(configure::ConfigureCommand),
    /// Print current ValidatorPublisherRewards fields.
    Show(show::ShowCommand),
}

impl PublisherRewardsCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self.command {
            PublisherRewardsSubcommand::Init(c) => c.try_into_execute().await,
            PublisherRewardsSubcommand::PrepareOffchainMessage(c) => c.try_into_execute().await,
            PublisherRewardsSubcommand::Configure(c) => c.try_into_execute().await,
            PublisherRewardsSubcommand::Show(c) => c.try_into_execute().await,
        }
    }
}

/// Validate that `rewards_token_mint` corresponds to a registered, enabled
/// `ShredRewardToken`. The caller passes the already-fetched account at the
/// SRT PDA (`None` means the account does not exist).
///
/// Used as a pre-flight by both `configure` (which spends a transaction) and
/// `prepare-offchain-message` (which produces a hex blob that would otherwise
/// only fail after a full offline round-trip + signing on the validator host).
pub(crate) fn validate_shred_reward_token(
    rewards_token_mint: &Pubkey,
    srt_pda: &Pubkey,
    account: Option<&Account>,
) -> Result<()> {
    let srt_account = account.with_context(|| {
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
    Ok(())
}

#[cfg(test)]
mod tests {
    use bytemuck::Zeroable;
    use doublezero_solana_sdk::{
        PrecomputedDiscriminator, shred_subscription::state::ShredRewardToken,
    };

    use super::*;

    /// Build an `Account` whose data is
    /// `[discriminator || bytemuck(ShredRewardToken)]`, matching the on-chain
    /// layout. `enabled` toggles `ShredRewardToken::FLAG_IS_ENABLED_BIT`.
    fn shred_reward_token_account(enabled: bool) -> Account {
        let mut shred_reward_token = ShredRewardToken::zeroed();
        if enabled {
            // Set the IS_ENABLED bit directly in the underlying flag bytes so
            // the test fixture does not need to import `ruint`. `flags` is the
            // first field after `mint_key` (32 bytes), and `Flags = ruint::U64`
            // is 8 bytes laid out little-endian — bit 1 lives in byte 0.
            let bytes = bytemuck::bytes_of_mut(&mut shred_reward_token);
            bytes[32] |= 1u8 << ShredRewardToken::FLAG_IS_ENABLED_BIT;
        }
        let mut data = Vec::with_capacity(8 + std::mem::size_of::<ShredRewardToken>());
        data.extend_from_slice(ShredRewardToken::discriminator_slice());
        data.extend_from_slice(bytemuck::bytes_of(&shred_reward_token));
        Account {
            data,
            ..Account::default()
        }
    }

    #[test]
    fn validate_shred_reward_token_none_is_not_registered() {
        let mint = Pubkey::new_unique();
        let pda = Pubkey::new_unique();
        let err = validate_shred_reward_token(&mint, &pda, None)
            .expect_err("None must be rejected as not-registered");
        let message = format!("{err:#}");
        assert!(
            message.contains("not a registered ShredRewardToken"),
            "got: {message}"
        );
    }

    #[test]
    fn validate_shred_reward_token_malformed_data_errors() {
        let mint = Pubkey::new_unique();
        let pda = Pubkey::new_unique();
        // Wrong discriminator + arbitrary trailing bytes → `from_account`
        // returns None and the helper surfaces a "malformed" error rather
        // than silently parsing junk.
        let bogus = Account {
            data: vec![0u8; 8 + std::mem::size_of::<ShredRewardToken>()],
            ..Account::default()
        };
        let err = validate_shred_reward_token(&mint, &pda, Some(&bogus))
            .expect_err("zero-discriminator data must be rejected");
        let message = format!("{err:#}");
        assert!(message.contains("is malformed"), "got: {message}");
    }

    #[test]
    fn validate_shred_reward_token_disabled_errors() {
        let mint = Pubkey::new_unique();
        let pda = Pubkey::new_unique();
        let account = shred_reward_token_account(false);
        let err = validate_shred_reward_token(&mint, &pda, Some(&account))
            .expect_err("disabled ShredRewardToken must be rejected");
        let message = format!("{err:#}");
        assert!(
            message.contains("registered but disabled"),
            "got: {message}"
        );
    }

    #[test]
    fn validate_shred_reward_token_enabled_ok() {
        let mint = Pubkey::new_unique();
        let pda = Pubkey::new_unique();
        let account = shred_reward_token_account(true);
        validate_shred_reward_token(&mint, &pda, Some(&account))
            .expect("enabled ShredRewardToken must pass pre-flight");
    }
}
