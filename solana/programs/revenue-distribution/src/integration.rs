use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_tools::{
    account_info::{
        try_next_enumerated_account, EnumeratedAccountInfoIter, NextAccountOptions, TryNextAccounts,
    },
    zero_copy::ZeroCopyAccount,
};
use solana_account_info::AccountInfo;
use solana_instruction::AccountMeta;
use solana_msg::msg;
use solana_program_error::ProgramError;
use solana_pubkey::Pubkey;

use crate::state::Distribution;

/// Instructions rev-distr CPIs integration programs with. Integration
/// programs deserialize this before their own instruction enum.
#[derive(Debug, Clone, PartialEq, Eq, BorshDeserialize, BorshSerialize)]
pub enum IntegrationInstructionData {
    /// Transfer the epoch's contributor-share 2Z from the integration's
    /// bucket to the destination and flip `is_collected = true`. See
    /// [`WithdrawIntegrationRewardsAccounts`] for the account list.
    ///
    /// Two timing rules apply:
    ///
    /// - Admins must register the integration (via
    ///   `InitializeRewardsIntegration`) **before** the target `Distribution`
    ///   is initialized. Each `Distribution` snapshots the registry count at
    ///   creation, so late-registered integrations are skipped for that
    ///   epoch and any revenue they've already accumulated for it stays
    ///   with the integration.
    /// - Handlers must succeed on an empty bucket (zero-transfer). If
    ///   rev-distr invokes this on an integration that hasn't yet
    ///   accumulated revenue, the handler must still flip `is_collected`
    ///   or `DistributeRewards` deadlocks.
    WithdrawIntegrationRewards,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct WithdrawIntegrationRewardsAccounts {
    pub integration_distribution_key: Pubkey,
    pub integration_2z_bucket_key: Pubkey,
    pub destination_token_account_key: Pubkey,
    pub parent_distribution_key: Pubkey,
}

impl From<WithdrawIntegrationRewardsAccounts> for Vec<AccountMeta> {
    fn from(accounts: WithdrawIntegrationRewardsAccounts) -> Self {
        let WithdrawIntegrationRewardsAccounts {
            integration_distribution_key,
            integration_2z_bucket_key,
            destination_token_account_key,
            parent_distribution_key,
        } = accounts;

        vec![
            AccountMeta::new(integration_distribution_key, false),
            AccountMeta::new(integration_2z_bucket_key, false),
            AccountMeta::new(destination_token_account_key, false),
            AccountMeta::new_readonly(parent_distribution_key, true),
        ]
    }
}

/// Handler-side view of [`WithdrawIntegrationRewardsAccounts`]. Integration
/// programs peel this out of their `accounts_iter` in one call, which
/// enforces the slot ordering and contract-level writable/signer flags.
/// Integration-specific checks (PDA seed derivation, epoch match) are left
/// to the caller.
pub struct WithdrawIntegrationRewardsHandlerAccounts<'a, 'b> {
    pub integration_distribution_info: (usize, &'a AccountInfo<'b>),
    pub integration_2z_bucket_info: (usize, &'a AccountInfo<'b>),
    pub destination_token_account_info: (usize, &'a AccountInfo<'b>),
    pub parent_distribution: ZeroCopyAccount<'a, 'b, Distribution>,
}

impl<'a, 'b> TryNextAccounts<'a, 'b, ()> for WithdrawIntegrationRewardsHandlerAccounts<'a, 'b> {
    fn try_next_accounts(
        accounts_iter: &mut EnumeratedAccountInfoIter<'a, 'b>,
        _: (),
    ) -> Result<Self, ProgramError> {
        let integration_distribution_info = try_next_enumerated_account(
            accounts_iter,
            NextAccountOptions {
                must_be_writable: true,
                ..Default::default()
            },
        )?;
        let integration_2z_bucket_info = try_next_enumerated_account(
            accounts_iter,
            NextAccountOptions {
                must_be_writable: true,
                ..Default::default()
            },
        )?;
        let destination_token_account_info = try_next_enumerated_account(
            accounts_iter,
            NextAccountOptions {
                must_be_writable: true,
                ..Default::default()
            },
        )?;
        let parent_distribution =
            ZeroCopyAccount::<Distribution>::try_next_accounts(accounts_iter, Some(&crate::ID))?;
        if !parent_distribution.info.is_signer {
            msg!("Account {} must be signer", parent_distribution.index);
            return Err(ProgramError::MissingRequiredSignature);
        }

        Ok(Self {
            integration_distribution_info,
            integration_2z_bucket_info,
            destination_token_account_info,
            parent_distribution,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn withdraw_integration_rewards_borsh_roundtrip() {
        let ix = IntegrationInstructionData::WithdrawIntegrationRewards;

        let serialized = borsh::to_vec(&ix).unwrap();
        let deserialized = IntegrationInstructionData::try_from_slice(&serialized).unwrap();
        assert_eq!(deserialized, ix);
    }

    #[test]
    fn withdraw_integration_rewards_discriminator_is_stable() {
        // The enum's discriminator is encoded as the first byte (Borsh enum
        // variant index). Future variants must be appended; existing ones
        // must keep their index for backward compatibility.
        let serialized =
            borsh::to_vec(&IntegrationInstructionData::WithdrawIntegrationRewards).unwrap();
        assert_eq!(serialized, vec![0]);
    }

    #[test]
    fn withdraw_integration_rewards_accounts_into_meta_preserves_order_and_flags() {
        let accounts = WithdrawIntegrationRewardsAccounts {
            integration_distribution_key: Pubkey::new_unique(),
            integration_2z_bucket_key: Pubkey::new_unique(),
            destination_token_account_key: Pubkey::new_unique(),
            parent_distribution_key: Pubkey::new_unique(),
        };
        let keys = [
            accounts.integration_distribution_key,
            accounts.integration_2z_bucket_key,
            accounts.destination_token_account_key,
            accounts.parent_distribution_key,
        ];

        let metas: Vec<AccountMeta> = accounts.into();
        assert_eq!(metas.len(), 4);
        for (i, meta) in metas.iter().enumerate() {
            assert_eq!(meta.pubkey, keys[i]);
        }

        // Slots 0-2 are writable, not signers.
        for meta in &metas[..3] {
            assert!(meta.is_writable);
            assert!(!meta.is_signer);
        }

        // Slot 3 (rev-distr Distribution) is read-only signer.
        assert!(!metas[3].is_writable);
        assert!(metas[3].is_signer);
    }
}
