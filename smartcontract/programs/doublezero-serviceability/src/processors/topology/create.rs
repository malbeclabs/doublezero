use crate::{
    error::DoubleZeroError,
    pda::{get_resource_extension_pda, get_topology_pda},
    processors::{resource::allocate_id, validation::validate_program_account},
    resource::ResourceType,
    seeds::{SEED_PREFIX, SEED_TOPOLOGY},
    serializer::{try_acc_create, try_acc_write},
    state::{
        accounttype::AccountType,
        device::Device,
        globalstate::GlobalState,
        interface::{Interface, LoopbackType},
        topology::{FlexAlgoNodeSegment, TopologyConstraint, TopologyInfo},
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};

pub const MAX_TOPOLOGY_NAME_LEN: usize = 32;

#[derive(BorshSerialize, BorshDeserializeIncremental, Debug, Clone, PartialEq)]
pub struct TopologyCreateArgs {
    pub name: String,
    pub constraint: TopologyConstraint,
}

/// Accounts layout:
/// [0]  topology PDA        (writable, to be created)
/// [1]  admin_group_bits    (writable, ResourceExtension)
/// [2]  globalstate         (readonly)
/// [3]  payer               (writable, signer, must be in foundation_allowlist)
/// [4]  system_program
/// [5]  segment_routing_ids (writable, ResourceExtension) — only if Vpnv4 loopbacks passed
/// [6+] Vpnv4 loopback Interface accounts (writable) — optional, for backfill
///
/// If no Vpnv4 loopbacks are passed, account [5] can be omitted.
pub fn process_topology_create(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &TopologyCreateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();
    let topology_account = next_account_info(accounts_iter)?;
    let admin_group_bits_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    assert!(payer_account.is_signer, "Payer account must be a signer");

    // Authorization: foundation keys only
    let globalstate = GlobalState::try_from(&globalstate_account.data.borrow()[..])?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        msg!("TopologyCreate: unauthorized — foundation key required");
        return Err(DoubleZeroError::Unauthorized.into());
    }

    // Validate name length
    if value.name.len() > MAX_TOPOLOGY_NAME_LEN {
        msg!(
            "TopologyCreate: name exceeds {} bytes",
            MAX_TOPOLOGY_NAME_LEN
        );
        return Err(DoubleZeroError::InvalidArgument.into());
    }

    // Validate and verify topology PDA
    let (expected_pda, bump_seed) = get_topology_pda(program_id, &value.name);
    assert_eq!(
        topology_account.key, &expected_pda,
        "TopologyCreate: invalid topology PDA for name '{}'",
        value.name
    );

    if !topology_account.data_is_empty() {
        msg!("TopologyCreate: topology '{}' already exists", value.name);
        return Err(ProgramError::AccountAlreadyInitialized);
    }

    // Validate AdminGroupBits resource account
    let (expected_ab_pda, _, _) =
        get_resource_extension_pda(program_id, ResourceType::AdminGroupBits);
    validate_program_account!(
        admin_group_bits_account,
        program_id,
        writable = true,
        pda = &expected_ab_pda,
        "AdminGroupBits"
    );

    // Allocate admin_group_bit (lowest available bit in IdRange)
    let admin_group_bit = allocate_id(admin_group_bits_account)? as u8;
    let flex_algo_number = 128u8
        .checked_add(admin_group_bit)
        .ok_or(DoubleZeroError::ArithmeticOverflow)?;

    // Create the topology PDA account
    let topology = TopologyInfo {
        account_type: AccountType::Topology,
        owner: *payer_account.key,
        bump_seed,
        name: value.name.clone(),
        admin_group_bit,
        flex_algo_number,
        constraint: value.constraint,
    };

    try_acc_create(
        &topology,
        topology_account,
        payer_account,
        system_program,
        program_id,
        &[
            SEED_PREFIX,
            SEED_TOPOLOGY,
            value.name.as_bytes(),
            &[bump_seed],
        ],
    )?;

    // Backfill Vpnv4 loopbacks (remaining accounts after system_program)
    // Convention: if any Device accounts are passed, segment_routing_ids must be
    // the last account; Device accounts precede it.
    let remaining: Vec<&AccountInfo> = accounts_iter.collect();
    if !remaining.is_empty() {
        let (device_accounts, tail) = remaining.split_at(remaining.len() - 1);
        let segment_routing_ids_account = tail[0];

        // Validate the SegmentRoutingIds account
        let (expected_sr_pda, _, _) =
            crate::pda::get_resource_extension_pda(program_id, ResourceType::SegmentRoutingIds);
        assert_eq!(
            segment_routing_ids_account.key, &expected_sr_pda,
            "TopologyCreate: invalid SegmentRoutingIds PDA"
        );

        for device_account in device_accounts {
            if device_account.owner != program_id {
                continue;
            }
            let mut device = Device::try_from(&device_account.data.borrow()[..])?;
            let mut modified = false;
            for iface in device.interfaces.iter_mut() {
                let iface_v2 = iface.into_current_version();
                if iface_v2.loopback_type != LoopbackType::Vpnv4 {
                    continue;
                }
                // Skip if already has a segment for this topology (idempotent)
                if iface_v2
                    .flex_algo_node_segments
                    .iter()
                    .any(|s| &s.topology == topology_account.key)
                {
                    continue;
                }
                let node_segment_idx = allocate_id(segment_routing_ids_account)?;
                // Mutate the interface in place — upgrade to V2 if needed
                match iface {
                    Interface::V2(ref mut v2) => {
                        v2.flex_algo_node_segments.push(FlexAlgoNodeSegment {
                            topology: *topology_account.key,
                            node_segment_idx,
                        });
                    }
                    _ => {
                        // Upgrade to current version (V2) with the segment added
                        let mut upgraded = iface.into_current_version();
                        upgraded.flex_algo_node_segments.push(FlexAlgoNodeSegment {
                            topology: *topology_account.key,
                            node_segment_idx,
                        });
                        *iface = Interface::V2(upgraded);
                    }
                }
                modified = true;
            }
            if modified {
                try_acc_write(&device, device_account, payer_account, accounts)?;
            }
        }
    }

    msg!(
        "TopologyCreate: created '{}' bit={} algo={} constraint={:?}",
        value.name,
        admin_group_bit,
        flex_algo_number,
        value.constraint
    );
    Ok(())
}
