use crate::{
    error::DoubleZeroError,
    globalstate::globalstate_get,
    pda::{
        get_device_tunnel_block_pda, get_globalconfig_pda, get_multicast_group_block_pda,
        get_user_tunnel_block_pda,
    },
    seeds::{
        SEED_DEVICE_TUNNEL_BLOCK, SEED_MULTICASTGROUP_BLOCK, SEED_PREFIX, SEED_USER_TUNNEL_BLOCK,
    },
    state::{globalconfig::GlobalConfig, resource_extension::ResourceExtensionBorrowed},
};
use borsh::{BorshDeserialize, BorshSerialize};
use borsh_incremental::BorshDeserializeIncremental;
use doublezero_program_common::create_account::try_create_account;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
use std::fmt;

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone, Default)]
pub enum IpBlockType {
    #[default]
    DeviceTunnelBlock,
    UserTunnelBlock,
    MulticastGroupBlock,
}

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct ResourceAllocateArgs {
    pub ip_block_type: IpBlockType,
}

impl fmt::Debug for ResourceAllocateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "ResourceAllocateArgs {{}}",)
    }
}

pub fn process_allocate_resource(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &ResourceAllocateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let resource_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let globalconfig_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_allocate_resource({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        globalconfig_account.owner, program_id,
        "Invalid GlobalConfig Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(resource_account.is_writable, "PDA Account is not writable");

    let globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let globalconfig = GlobalConfig::try_from(&globalconfig_account.data.borrow()[..])?;
    let (globalconfig_pda, _globalconfig_bump_seed) = get_globalconfig_pda(program_id);
    assert_eq!(
        globalconfig_account.key, &globalconfig_pda,
        "Invalid GlobalConfig PubKey"
    );

    let expected_resource_pda;
    let base_seed;
    let bump_seed;
    let base_net;
    let ip_allocations;
    match value.ip_block_type {
        IpBlockType::DeviceTunnelBlock => {
            (expected_resource_pda, bump_seed) = get_device_tunnel_block_pda(program_id);
            base_seed = SEED_DEVICE_TUNNEL_BLOCK;
            base_net = globalconfig.device_tunnel_block;
            ip_allocations = 2;
        }
        IpBlockType::UserTunnelBlock => {
            (expected_resource_pda, bump_seed) = get_user_tunnel_block_pda(program_id);
            base_seed = SEED_USER_TUNNEL_BLOCK;
            base_net = globalconfig.user_tunnel_block;
            ip_allocations = 2;
        }
        IpBlockType::MulticastGroupBlock => {
            (expected_resource_pda, bump_seed) = get_multicast_group_block_pda(program_id);
            base_seed = SEED_MULTICASTGROUP_BLOCK;
            base_net = globalconfig.multicastgroup_block;
            ip_allocations = 1;
        }
    }
    assert_eq!(
        resource_account.key, &expected_resource_pda,
        "Invalid Resource Account PubKey"
    );

    if resource_account.data.borrow().is_empty() {
        let data_size: usize = 8192; // TODO
        try_create_account(
            payer_account.key,           // Account paying for the new account
            resource_account.key,        // Account to be created
            resource_account.lamports(), // Current amount of lamports on the new account
            data_size,                   // Size in bytes to allocate for the data field
            program_id,                  // Set program owner to our program
            &[
                resource_account.clone(),
                payer_account.clone(),
                system_program.clone(),
            ],
            &[SEED_PREFIX, base_seed, &[bump_seed]],
        )?;
        ResourceExtensionBorrowed::construct_ip_resource(
            resource_account,
            *program_id,
            bump_seed,
            *program_id,
            base_net,
            ip_allocations,
        )?;
    } else {
        assert_eq!(
            resource_account.owner, program_id,
            "Invalid Resource Account Owner"
        );
    }

    let mut buffer = resource_account.data.borrow_mut();
    let mut resource = ResourceExtensionBorrowed::inplace_from(&mut buffer[..])?;
    resource.allocate().unwrap();

    Ok(())
}
