use crate::{
    error::DoubleZeroError,
    pda::get_resource_extension_pda,
    processors::{
        resource::{allocate_specific_ip, deallocate_ip},
        validation::validate_program_account,
    },
    resource::ResourceType,
    serializer::try_acc_write,
    state::{
        feature_flags::{is_feature_enabled, FeatureFlag},
        globalstate::GlobalState,
        multicastgroup::*,
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use doublezero_program_common::{types::NetworkV4, validate_account_code};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
use std::fmt;

#[cfg(test)]
use solana_program::msg;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct MulticastGroupUpdateArgs {
    pub code: Option<String>,
    pub multicast_ip: Option<std::net::Ipv4Addr>,
    pub max_bandwidth: Option<u64>,
    pub publisher_count: Option<u32>,
    pub subscriber_count: Option<u32>,
    /// When true, onchain allocation is used for multicast_ip changes.
    /// Requires ResourceExtension account (MulticastGroupBlock).
    #[incremental(default = false)]
    pub use_onchain_allocation: bool,
}

impl fmt::Debug for MulticastGroupUpdateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "code: {:?}, multicast_ip: {:?}, max_bandwidth: {:?}, publisher_count: {:?}, subscriber_count: {:?}, use_onchain_allocation: {}",
            self.code, self.multicast_ip, self.max_bandwidth, self.publisher_count, self.subscriber_count, self.use_onchain_allocation
        )
    }
}

pub fn process_update_multicastgroup(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &MulticastGroupUpdateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let multicastgroup_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Optional: ResourceExtension account for onchain allocation (before payer)
    // Account layout WITH allocation (use_onchain_allocation = true):
    //   [mgroup, globalstate, multicast_group_block, payer, system]
    // Account layout WITHOUT (legacy, use_onchain_allocation = false):
    //   [mgroup, globalstate, payer, system]
    let resource_extension_account = if value.use_onchain_allocation {
        Some(next_account_info(accounts_iter)?)
    } else {
        None
    };

    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_update_multicastgroup({:?})", value);

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
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );
    assert!(
        multicastgroup_account.is_writable,
        "PDA Account is not writable"
    );
    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = GlobalState::try_from(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Parse the multicastgroup account
    let mut multicastgroup: MulticastGroup = MulticastGroup::try_from(multicastgroup_account)?;

    if let Some(ref code) = value.code {
        multicastgroup.code =
            validate_account_code(code).map_err(|_| DoubleZeroError::InvalidAccountCode)?;
    }
    if let Some(ref multicast_ip) = value.multicast_ip {
        // Handle onchain allocation for IP changes
        if let Some(multicast_group_block_ext) = resource_extension_account.as_ref() {
            if !is_feature_enabled(globalstate.feature_flags, FeatureFlag::OnChainAllocation) {
                return Err(DoubleZeroError::FeatureNotEnabled.into());
            }

            let (expected_pda, _, _) =
                get_resource_extension_pda(program_id, ResourceType::MulticastGroupBlock);
            validate_program_account!(
                multicast_group_block_ext,
                program_id,
                writable = true,
                pda = Some(&expected_pda),
                "MulticastGroupBlock"
            );

            // Deallocate the old IP (if it was allocated)
            if multicastgroup.multicast_ip != std::net::Ipv4Addr::UNSPECIFIED {
                deallocate_ip(multicast_group_block_ext, multicastgroup.multicast_ip);
            }

            // Allocate the new specific IP
            let new_ip_net =
                NetworkV4::new(*multicast_ip, 32).map_err(|_| DoubleZeroError::InvalidArgument)?;
            allocate_specific_ip(multicast_group_block_ext, new_ip_net)?;
        }

        multicastgroup.multicast_ip = *multicast_ip;
    }
    if let Some(ref max_bandwidth) = value.max_bandwidth {
        multicastgroup.max_bandwidth = *max_bandwidth;
    }
    if let Some(ref publisher_count) = value.publisher_count {
        multicastgroup.publisher_count = *publisher_count;
    }
    if let Some(ref subscriber_count) = value.subscriber_count {
        multicastgroup.subscriber_count = *subscriber_count;
    }

    try_acc_write(
        &multicastgroup,
        multicastgroup_account,
        payer_account,
        accounts,
    )?;

    #[cfg(test)]
    msg!("Updated: {:?}", multicastgroup);

    Ok(())
}
