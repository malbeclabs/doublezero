use crate::{
    error::DoubleZeroError,
    pda::{get_globalstate_pda, get_link_pda, get_topology_pda},
    processors::validation::validate_program_account,
    serializer::try_acc_close,
    state::{globalstate::GlobalState, link::Link},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, Debug, Clone, PartialEq)]
pub struct TopologyDeleteArgs {
    pub name: String,
}

/// Accounts layout:
/// [0] topology PDA     (writable, to be closed)
/// [1] globalstate      (readonly)
/// [2] payer            (writable, signer, must be in foundation_allowlist)
/// [3] system_program
/// [4+] Link accounts   (readonly) — guard: fail if any references this topology
pub fn process_topology_delete(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &TopologyDeleteArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let topology_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_topology_delete(name={})", value.name);

    // Payer must be a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Validate the Topology PDA (writable — about to be closed).
    validate_program_account!(
        topology_account,
        program_id,
        writable = true,
        pda = &get_topology_pda(program_id, &value.name).0,
        "Topology"
    );

    // Validate GlobalState singleton PDA.
    validate_program_account!(
        globalstate_account,
        program_id,
        writable = false,
        pda = &get_globalstate_pda(program_id).0,
        "GlobalState"
    );

    // Authorization: foundation keys only
    let globalstate = GlobalState::try_from(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        msg!("TopologyDelete: unauthorized — foundation key required");
        return Err(DoubleZeroError::Unauthorized.into());
    }

    // Check remaining Link accounts — fail if any reference this topology.
    for link_account in accounts_iter {
        validate_program_account!(link_account, program_id, writable = false, "Link");
        let link = Link::try_from(link_account)?;
        assert_eq!(
            link_account.key,
            &get_link_pda(program_id, link.index).0,
            "Invalid Link PDA"
        );
        if link.link_topologies.contains(topology_account.key) {
            msg!(
                "TopologyDelete: link {} still references topology {}",
                link_account.key,
                topology_account.key
            );
            return Err(DoubleZeroError::ReferenceCountNotZero.into());
        }
    }

    // Close the topology PDA (transfer lamports to payer, zero data)
    // NOTE: We do NOT deallocate the admin-group bit — bits are permanently retired.
    // If a bit were reused for a new topology, any IS-IS router still advertising
    // link memberships for the deleted topology would classify traffic onto the new
    // topology's flex-algo path until the network fully converges, causing misrouting.
    // Admin-group bits are a cheap resource (128 total), so permanent allocation is safe.
    try_acc_close(topology_account, payer_account)?;

    msg!("TopologyDelete: closed topology '{}'", value.name);
    Ok(())
}
