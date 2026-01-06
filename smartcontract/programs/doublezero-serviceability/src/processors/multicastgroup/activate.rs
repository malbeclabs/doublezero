use crate::{
    error::DoubleZeroError,
    pda::get_resource_extension_pda,
    resource::{IdOrIp, ResourceType},
    serializer::try_acc_write,
    state::{
        globalstate::GlobalState,
        multicastgroup::*,
        resource_extension::{Allocator, ResourceExtensionBorrowed},
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone)]
pub struct MulticastGroupActivateArgs {
    #[incremental(default = std::net::Ipv4Addr::UNSPECIFIED)]
    pub multicast_ip: std::net::Ipv4Addr,
}

impl fmt::Debug for MulticastGroupActivateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "",)
    }
}

pub fn process_activate_multicastgroup(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &MulticastGroupActivateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let multicastgroup_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;
    // Optional: ResourceExtension account for on-chain IP allocation
    let resource_extension_account = accounts_iter.next();

    #[cfg(test)]
    msg!("process_activate_multicastgroup({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(
        multicastgroup_account.owner, program_id,
        "Invalid PDA Account Owner"
    );
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    // Check if the account is writable
    assert!(
        multicastgroup_account.is_writable,
        "PDA Account is not writable"
    );

    let globalstate = GlobalState::try_from(globalstate_account)?;

    // Authorization: allow activator_authority_pk OR foundation_allowlist
    let is_activator = globalstate.activator_authority_pk == *payer_account.key;
    let is_foundation = globalstate.foundation_allowlist.contains(payer_account.key);
    if !is_activator && !is_foundation {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut multicastgroup: MulticastGroup = MulticastGroup::try_from(multicastgroup_account)?;

    if multicastgroup.status != MulticastGroupStatus::Pending {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    // Allocate multicast IP from ResourceExtension or use provided value
    if let Some(resource_ext) = resource_extension_account {
        // Validate ResourceExtension account
        assert_eq!(
            resource_ext.owner, program_id,
            "Invalid ResourceExtension Account Owner"
        );
        assert!(
            resource_ext.is_writable,
            "ResourceExtension Account is not writable"
        );
        assert!(
            !resource_ext.data.borrow().is_empty(),
            "ResourceExtension Account is empty"
        );

        // Validate PDA matches expected MulticastGroupBlock PDA
        let (expected_pda, _, _) =
            get_resource_extension_pda(program_id, ResourceType::MulticastGroupBlock);
        assert_eq!(
            resource_ext.key, &expected_pda,
            "Invalid ResourceExtension PDA for MulticastGroupBlock"
        );

        // Allocate from ResourceExtension bitmap
        let mut buffer = resource_ext.data.borrow_mut();
        let mut resource = ResourceExtensionBorrowed::inplace_from(&mut buffer[..])?;

        let allocated = resource.allocate()?;

        match allocated {
            IdOrIp::Ip(network) => {
                multicastgroup.multicast_ip = network.ip();
                // Calculate slot index from allocated IP for later deallocation
                multicastgroup.multicast_slot = calculate_multicast_slot(&resource, &network)?;
            }
            IdOrIp::Id(_) => {
                return Err(DoubleZeroError::InvalidArgument.into());
            }
        }
    } else {
        // Legacy behavior: use provided multicast_ip
        multicastgroup.multicast_ip = value.multicast_ip;
        multicastgroup.multicast_slot = u32::MAX;
    }

    multicastgroup.status = MulticastGroupStatus::Activated;

    try_acc_write(
        &multicastgroup,
        multicastgroup_account,
        payer_account,
        accounts,
    )?;

    msg!("Activated: {:?}", multicastgroup);

    Ok(())
}

/// Calculate the slot index from an allocated IP for later deallocation.
fn calculate_multicast_slot(
    resource: &ResourceExtensionBorrowed,
    allocated: &doublezero_program_common::types::NetworkV4,
) -> Result<u32, DoubleZeroError> {
    match &resource.allocator {
        Allocator::Ip(ip_allocator) => {
            let base_ip_int = u32::from_be_bytes(ip_allocator.base_net.ip().octets());
            let allocated_ip_int = u32::from_be_bytes(allocated.ip().octets());
            let offset = allocated_ip_int
                .checked_sub(base_ip_int)
                .ok_or(DoubleZeroError::AllocationFailed)?;
            Ok(offset / ip_allocator.allocation_size)
        }
        Allocator::Id(_) => Err(DoubleZeroError::InvalidArgument),
    }
}
