use std::io;

use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_tools::{
    account_info::{
        try_next_enumerated_account, EnumeratedAccountInfoIter, NextAccountOptions, TryNextAccounts,
    },
    zero_copy::ZeroCopyAccount,
    Discriminator, DISCRIMINATOR_LEN,
};
use solana_account_info::AccountInfo;
use solana_instruction::AccountMeta;
use solana_msg::msg;
use solana_program_error::ProgramError;
use solana_pubkey::Pubkey;

use crate::state::Distribution;

/// Seed prefix every integration program must use for its per-epoch
/// "integration distribution" PDA (seeded as `[PREFIX, dz_epoch.as_seed()]`).
pub const INTEGRATION_DISTRIBUTION_SEED_PREFIX: &[u8] = b"integration_distribution";

/// Derivation of an integration's per-epoch distribution PDA.
pub fn find_integration_distribution_address(
    integration_program_id: &Pubkey,
    dz_epoch: crate::types::DoubleZeroEpoch,
) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[INTEGRATION_DISTRIBUTION_SEED_PREFIX, &dz_epoch.as_seed()],
        integration_program_id,
    )
}

/// Derivation of an integration's 2Z bucket PDA.
pub fn find_integration_bucket_address(
    integration_program_id: &Pubkey,
    integration_distribution_key: &Pubkey,
) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[
            crate::state::TOKEN_2Z_PDA_SEED_PREFIX,
            integration_distribution_key.as_ref(),
        ],
        integration_program_id,
    )
}

/// Instructions rev-distr CPIs integration programs with. Integration
/// programs deserialize this before their own instruction enum.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum IntegrationInstructionData {
    /// Transfer the epoch's contributor-share 2Z from the integration's
    /// bucket to the destination. See [`WithdrawIntegrationRewardsAccounts`]
    /// for the account list.
    ///
    /// Admins must register the integration (via
    /// `InitializeRewardsIntegration`) **before** the target `Distribution`
    /// is initialized. Each `Distribution` snapshots the registry count at
    /// creation, so late-registered integrations are skipped for that epoch
    /// and any revenue they've already accumulated for it stays with the
    /// integration.
    WithdrawIntegrationRewards,
}

impl IntegrationInstructionData {
    pub const WITHDRAW_INTEGRATION_REWARDS: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::integration_ix::withdraw_integration_rewards");
}

impl BorshDeserialize for IntegrationInstructionData {
    fn deserialize_reader<R: io::Read>(reader: &mut R) -> io::Result<Self> {
        match Discriminator::deserialize_reader(reader)? {
            Self::WITHDRAW_INTEGRATION_REWARDS => Ok(Self::WithdrawIntegrationRewards),
            _ => Err(io::Error::new(
                io::ErrorKind::InvalidData,
                "Invalid discriminator",
            )),
        }
    }
}

impl BorshSerialize for IntegrationInstructionData {
    fn serialize<W: io::Write>(&self, writer: &mut W) -> io::Result<()> {
        match self {
            Self::WithdrawIntegrationRewards => {
                Self::WITHDRAW_INTEGRATION_REWARDS.serialize(writer)
            }
        }
    }
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
            AccountMeta::new_readonly(spl_token_interface::ID, false),
        ]
    }
}

/// Handler-side view of [`WithdrawIntegrationRewardsAccounts`]. Integration
/// programs peel this out of their `accounts_iter` in one call, which
/// enforces the slot ordering, contract-level writable/signer flags, and
/// that the parent `Distribution`'s `dz_epoch` matches the integration's
/// local epoch.
pub struct WithdrawIntegrationRewardsHandlerAccounts<'a, 'b> {
    pub integration_distribution_info: (usize, &'a AccountInfo<'b>),
    pub integration_2z_bucket_info: (usize, &'a AccountInfo<'b>),
    pub destination_token_account_info: (usize, &'a AccountInfo<'b>),
    pub parent_distribution: ZeroCopyAccount<'a, 'b, Distribution>,
}

impl<'a, 'b> TryNextAccounts<'a, 'b, crate::types::DoubleZeroEpoch>
    for WithdrawIntegrationRewardsHandlerAccounts<'a, 'b>
{
    fn try_next_accounts(
        accounts_iter: &mut EnumeratedAccountInfoIter<'a, 'b>,
        integration_dz_epoch: crate::types::DoubleZeroEpoch,
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
        if parent_distribution.dz_epoch != integration_dz_epoch {
            msg!(
                "DZ epoch mismatch: integration={}, parent={}",
                integration_dz_epoch,
                parent_distribution.dz_epoch,
            );
            return Err(ProgramError::InvalidAccountData);
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
        let serialized =
            borsh::to_vec(&IntegrationInstructionData::WithdrawIntegrationRewards).unwrap();
        let expected =
            borsh::to_vec(&IntegrationInstructionData::WITHDRAW_INTEGRATION_REWARDS).unwrap();
        assert_eq!(serialized, expected);
        assert_eq!(serialized.len(), DISCRIMINATOR_LEN);
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
            spl_token_interface::ID,
        ];

        let metas: Vec<AccountMeta> = accounts.into();
        assert_eq!(metas.len(), 5);
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

        // Slot 4 (SPL Token program) is read-only, not signer.
        assert!(!metas[4].is_writable);
        assert!(!metas[4].is_signer);
    }
}
