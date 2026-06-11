use crate::{
    error::DoubleZeroError,
    pda::{get_globalstate_pda, get_resource_extension_pda, get_topology_pda},
    processors::{resource::allocate_id, validation::validate_program_account},
    resource::ResourceType,
    seeds::{SEED_PREFIX, SEED_TOPOLOGY},
    serializer::try_acc_create,
    state::{
        accounttype::AccountType,
        globalstate::GlobalState,
        topology::{validate_topology_name, TopologyConstraint, TopologyInfo},
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

    // Validate GlobalState singleton PDA
    validate_program_account!(
        globalstate_account,
        program_id,
        writable = false,
        pda = &get_globalstate_pda(program_id).0,
        "GlobalState"
    );

    // Authorization: foundation keys only
    let globalstate = GlobalState::try_from(&globalstate_account.data.borrow()[..])?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        msg!("TopologyCreate: unauthorized — foundation key required");
        return Err(DoubleZeroError::Unauthorized.into());
    }

    // Normalize name to canonical uppercase form and validate format.
    let name = value.name.to_ascii_uppercase();
    validate_topology_name(&name).map_err(|e| {
        msg!("TopologyCreate: invalid name '{}': {}", name, e);
        ProgramError::from(e)
    })?;

    // Validate and verify topology PDA. The account is still empty here
    // (we're about to create it), so we cannot use validate_program_account!
    // which asserts non-empty data. Check the PDA and writability directly.
    let (expected_pda, bump_seed) = get_topology_pda(program_id, &name);
    assert_eq!(
        topology_account.key, &expected_pda,
        "TopologyCreate: invalid topology PDA for name '{}'",
        name
    );
    assert!(
        topology_account.is_writable,
        "Topology Account is not writable"
    );

    if !topology_account.data_is_empty() {
        msg!("TopologyCreate: topology '{}' already exists", name);
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
        name: name.clone(),
        admin_group_bit,
        flex_algo_number,
        constraint: value.constraint,
        reference_count: 0,
    };

    try_acc_create(
        &topology,
        topology_account,
        payer_account,
        system_program,
        program_id,
        &[SEED_PREFIX, SEED_TOPOLOGY, name.as_bytes(), &[bump_seed]],
    )?;

    msg!(
        "TopologyCreate: created '{}' bit={} algo={} constraint={:?}",
        name,
        admin_group_bit,
        flex_algo_number,
        value.constraint
    );
    Ok(())
}
