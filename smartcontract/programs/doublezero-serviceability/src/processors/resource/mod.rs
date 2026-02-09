use crate::{
    pda::{get_globalconfig_pda, get_resource_extension_pda},
    resource::ResourceType,
    seeds::SEED_PREFIX,
    state::{
        device::Device,
        globalconfig::GlobalConfig,
        resource_extension::{ResourceExtensionBorrowed, ResourceExtensionRange},
    },
};
use doublezero_program_common::create_account::try_create_account;
use solana_program::{account_info::AccountInfo, entrypoint::ProgramResult, pubkey::Pubkey};

pub mod allocate;
pub mod closeaccount;
pub mod create;
pub mod deallocate;

pub fn get_resource_extension_range(
    program_id: &Pubkey,
    globalconfig: &GlobalConfig,
    opt_associated_account: Option<&AccountInfo>,
    resource_type: ResourceType,
) -> ResourceExtensionRange {
    let mut device = None;
    if let Some(associated_account) = opt_associated_account {
        if associated_account.key != &Pubkey::default() {
            assert_eq!(
                associated_account.owner, program_id,
                "Invalid PDA Account Owner (associated account)"
            );
            device = Some(Device::try_from(associated_account).expect(
                "Failed to deserialize associated account as Device when getting resource extension IP block",
            ));
        }
    }
    match resource_type {
        ResourceType::DeviceTunnelBlock => {
            ResourceExtensionRange::IpBlock(globalconfig.device_tunnel_block, 2)
        }
        ResourceType::UserTunnelBlock => {
            ResourceExtensionRange::IpBlock(globalconfig.user_tunnel_block, 2)
        }
        ResourceType::MulticastGroupBlock => {
            ResourceExtensionRange::IpBlock(globalconfig.multicastgroup_block, 1)
        }
        ResourceType::DzPrefixBlock(_, index) => {
            assert!(
                device.is_some(),
                "Associated account must be a device for DzPrefixBlock"
            );
            ResourceExtensionRange::IpBlock(device.unwrap().dz_prefixes[index], 1)
        }
        ResourceType::TunnelIds(_, _) => {
            assert!(
                device.is_some(),
                "Associated account must be a device for DzPrefixBlock"
            );
            ResourceExtensionRange::IdRange(500, 4596)
        }
        ResourceType::LinkIds => ResourceExtensionRange::IdRange(0, 65535),
        ResourceType::SegmentRoutingIds => ResourceExtensionRange::IdRange(1, 65535),
        ResourceType::VrfIds => ResourceExtensionRange::IdRange(1, 1024),
    }
}

pub fn create_resource(
    program_id: &Pubkey,
    resource_account: &AccountInfo,
    associated_account: Option<&AccountInfo>,
    globalconfig_account: &AccountInfo,
    payer_account: &AccountInfo,
    accounts: &[AccountInfo],
    resource_type: ResourceType,
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
        get_resource_extension_pda(program_id, resource_type);
    let resource_range =
        get_resource_extension_range(program_id, &globalconfig, associated_account, resource_type);

    assert_eq!(
        resource_account.key, &expected_resource_pda,
        "Invalid Resource Account PubKey"
    );

    let data_size: usize = ResourceExtensionBorrowed::size(&resource_range);

    if resource_account.data_is_empty() {
        match resource_type {
            ResourceType::DzPrefixBlock(_, index) | ResourceType::TunnelIds(_, index) => {
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
                        associated_account.unwrap().key.to_bytes().as_ref(),
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
    } else {
        let current_size = resource_account.data.borrow().len();
        if current_size != data_size {
            doublezero_program_common::resize_account::resize_account_if_needed(
                resource_account,
                payer_account,
                accounts,
                data_size,
            )?;
        }

        // account exists, has been resized if needed, so clear out existing data
        let mut resource_data = resource_account.data.borrow_mut();
        resource_data.fill(0);
    }

    let default_pubkey = Pubkey::default();
    ResourceExtensionBorrowed::construct_resource(
        resource_account,
        payer_account.key,
        bump_seed,
        match associated_account {
            Some(acc) => acc.key,
            None => &default_pubkey,
        },
        &resource_range,
    )?;

    // Reserve first two IPs for DzPrefixBlock (device tunnel endpoints).
    // Contributors configure these IPs on loopback interfaces as user tunnel endpoints:
    // - Index 0: First tunnel endpoint (e.g. Loopback100, unicast)
    // - Index 1: Second tunnel endpoint (e.g. multicast)
    // For small prefixes (e.g. /32) that only have 1 IP, the second reservation is skipped.
    if let ResourceType::DzPrefixBlock(_, _) = resource_type {
        let mut buffer = resource_account.data.borrow_mut();
        let mut resource = ResourceExtensionBorrowed::inplace_from(&mut buffer[..])?;
        resource.allocate(1)?; // Index 0
        let _ = resource.allocate(1); // Index 1 (best-effort for small prefixes)
    }

    Ok(())
}
