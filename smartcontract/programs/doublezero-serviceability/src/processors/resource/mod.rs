use crate::{
    pda::{get_globalconfig_pda, get_resource_extension_pda},
    resource::IpBlockType,
    seeds::SEED_PREFIX,
    state::{
        device::Device, globalconfig::GlobalConfig, resource_extension::ResourceExtensionBorrowed,
    },
};
use doublezero_program_common::{create_account::try_create_account, types::NetworkV4};
use solana_program::{account_info::AccountInfo, entrypoint::ProgramResult, pubkey::Pubkey};

pub mod allocate;
pub mod create;
pub mod deallocate;

pub fn get_resource_extension_ip_block(
    program_id: &Pubkey,
    globalconfig: &GlobalConfig,
    associated_account: &AccountInfo,
    ip_block_type: IpBlockType,
) -> (NetworkV4, u32) {
    let mut device = None;
    if associated_account.key != &Pubkey::default() {
        assert_eq!(
            associated_account.owner, program_id,
            "Invalid PDA Account Owner (associated account)"
        );
        device = Some(Device::try_from(associated_account).expect(
            "Failed to deserialize associated account as Device when getting resource extension IP block",
        ));
    }
    match ip_block_type {
        IpBlockType::DeviceTunnelBlock => (globalconfig.device_tunnel_block, 2),
        IpBlockType::UserTunnelBlock => (globalconfig.user_tunnel_block, 2),
        IpBlockType::MulticastGroupBlock => (globalconfig.multicastgroup_block, 1),
        IpBlockType::DzPrefixBlock(_, index) => {
            assert!(
                device.is_some(),
                "Associated account must be a device for DzPrefixBlock"
            );
            (device.unwrap().dz_prefixes[index], 1)
        }
    }
}

pub fn create_ip_resource(
    program_id: &Pubkey,
    resource_account: &AccountInfo,
    associated_account: &AccountInfo,
    globalconfig_account: &AccountInfo,
    payer_account: &AccountInfo,
    accounts: &[AccountInfo],
    ip_block_type: IpBlockType,
) -> ProgramResult {
    // Check if the account is writable
    assert!(resource_account.is_writable, "PDA Account is not writable");

    let globalconfig = GlobalConfig::try_from(&globalconfig_account.data.borrow()[..])?;
    let (globalconfig_pda, _globalconfig_bump_seed) = get_globalconfig_pda(program_id);
    assert_eq!(
        globalconfig_account.key, &globalconfig_pda,
        "Invalid GlobalConfig PubKey"
    );

    let (expected_resource_pda, bump_seed, base_seed) =
        get_resource_extension_pda(program_id, ip_block_type);
    let (ip_block, ip_allocations) = get_resource_extension_ip_block(
        program_id,
        &globalconfig,
        associated_account,
        ip_block_type,
    );

    assert_eq!(
        resource_account.key, &expected_resource_pda,
        "Invalid Resource Account PubKey"
    );

    assert!(resource_account.data.borrow().is_empty());

    let data_size: usize = ResourceExtensionBorrowed::size(&ip_block, ip_allocations);
    match ip_block_type {
        IpBlockType::DzPrefixBlock(_, index) => {
            try_create_account(
                payer_account.key,           // Account paying for the new account
                resource_account.key,        // Account to be created
                resource_account.lamports(), // Current amount of lamports on the new account
                data_size,                   // Size in bytes to allocate for the data field
                program_id,                  // Set program owner to our program
                accounts,
                &[
                    SEED_PREFIX,
                    base_seed,
                    associated_account.key.to_bytes().as_ref(),
                    index.to_le_bytes().as_ref(),
                    &[bump_seed],
                ] as &[_],
            )?;
        }
        _ => {
            try_create_account(
                payer_account.key,           // Account paying for the new account
                resource_account.key,        // Account to be created
                resource_account.lamports(), // Current amount of lamports on the new account
                data_size,                   // Size in bytes to allocate for the data field
                program_id,                  // Set program owner to our program
                accounts,
                &[SEED_PREFIX, base_seed, &[bump_seed]] as &[_],
            )?;
        }
    };
    ResourceExtensionBorrowed::construct_ip_resource(
        resource_account,
        program_id,
        bump_seed,
        associated_account.key,
        &ip_block,
        ip_allocations,
    )?;

    Ok(())
}
