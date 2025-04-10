use crate::{doublezeroclient::DoubleZeroClient, DZClient};
use double_zero_sla_program::{
    instructions::DoubleZeroInstruction,
    pda::get_globalstate_pda,
    processors::globalstate::{
        device_allowlist::{
            add::AddDeviceAllowlistGlobalConfigArgs, remove::RemoveDeviceAllowlistGlobalConfigArgs,
        },
        foundation_allowlist::{
            add::AddFoundationAllowlistGlobalConfigArgs,
            remove::RemoveFoundationAllowlistGlobalConfigArgs,
        },
        user_allowlist::{
            add::AddUserAllowlistGlobalConfigArgs, remove::RemoveUserAllowlistGlobalConfigArgs,
        },
    },
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

pub trait AllowlistService {
    fn add_foundation_allowlist(&self, user_pk: Pubkey) -> eyre::Result<Signature>;
    fn remove_foundation_allowlist(&self, user_pk: Pubkey) -> eyre::Result<Signature>;
    fn add_user_allowlist(&self, user_pk: Pubkey) -> eyre::Result<Signature>;
    fn remove_user_allowlist(&self, user_pk: Pubkey) -> eyre::Result<Signature>;
    fn add_device_allowlist(&self, device_pk: Pubkey) -> eyre::Result<Signature>;
    fn remove_device_allowlist(&self, device_pk: Pubkey) -> eyre::Result<Signature>;
}

impl AllowlistService for DZClient {
    fn add_foundation_allowlist(&self, pubkey: Pubkey) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_globalstate_pda(&self.program_id);

        self.execute_transaction(
            DoubleZeroInstruction::AddFoundationAllowlistGlobalConfig(
                AddFoundationAllowlistGlobalConfigArgs { pubkey },
            ),
            vec![AccountMeta::new(pda_pubkey, false)],
        )
    }

    fn remove_foundation_allowlist(&self, pubkey: Pubkey) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_globalstate_pda(&self.program_id);

        self.execute_transaction(
            DoubleZeroInstruction::RemoveFoundationAllowlistGlobalConfig(
                RemoveFoundationAllowlistGlobalConfigArgs { pubkey },
            ),
            vec![AccountMeta::new(pda_pubkey, false)],
        )
    }

    fn add_device_allowlist(&self, pubkey: Pubkey) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_globalstate_pda(&self.program_id);

        self.execute_transaction(
            DoubleZeroInstruction::AddDeviceAllowlistGlobalConfig(
                AddDeviceAllowlistGlobalConfigArgs { pubkey },
            ),
            vec![AccountMeta::new(pda_pubkey, false)],
        )
    }

    fn remove_device_allowlist(&self, pubkey: Pubkey) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_globalstate_pda(&self.program_id);

        self.execute_transaction(
            DoubleZeroInstruction::RemoveDeviceAllowlistGlobalConfig(
                RemoveDeviceAllowlistGlobalConfigArgs { pubkey },
            ),
            vec![AccountMeta::new(pda_pubkey, false)],
        )
    }

    fn add_user_allowlist(&self, pubkey: Pubkey) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_globalstate_pda(&self.program_id);

        self.execute_transaction(
            DoubleZeroInstruction::AddUserAllowlistGlobalConfig(AddUserAllowlistGlobalConfigArgs {
                pubkey,
            }),
            vec![AccountMeta::new(pda_pubkey, false)],
        )
    }

    fn remove_user_allowlist(&self, pubkey: Pubkey) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_globalstate_pda(&self.program_id);

        self.execute_transaction(
            DoubleZeroInstruction::RemoveUserAllowlistGlobalConfig(
                RemoveUserAllowlistGlobalConfigArgs { pubkey },
            ),
            vec![AccountMeta::new(pda_pubkey, false)],
        )
    }
}
