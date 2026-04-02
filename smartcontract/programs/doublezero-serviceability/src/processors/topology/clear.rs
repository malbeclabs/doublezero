use crate::{
    error::DoubleZeroError,
    pda::get_topology_pda,
    serializer::try_acc_write,
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
pub struct TopologyClearArgs {
    pub name: String,
}

/// Accounts layout:
/// [0] topology PDA     (readonly, for key validation)
/// [1] globalstate      (readonly)
/// [2] payer            (writable, signer, must be in foundation_allowlist)
/// [3+] Link accounts   (writable) — remove topology pubkey from link_topologies on each
pub fn process_topology_clear(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &TopologyClearArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let topology_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_topology_clear(name={})", value.name);

    // Payer must be a signer
    if !payer_account.is_signer {
        msg!("TopologyClear: payer must be a signer");
        return Err(DoubleZeroError::Unauthorized.into());
    }

    // Authorization: foundation keys only
    let globalstate = GlobalState::try_from(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        msg!("TopologyClear: unauthorized — foundation key required");
        return Err(DoubleZeroError::Unauthorized.into());
    }

    // Validate topology PDA
    let (expected_pda, _) = get_topology_pda(program_id, &value.name);
    assert_eq!(
        topology_account.key, &expected_pda,
        "TopologyClear: invalid topology PDA for name '{}'",
        value.name
    );

    // We don't require the topology to still exist (it may already be closed).
    // The validation above confirms the key matches the expected PDA for the name.

    let topology_key = topology_account.key;
    let mut cleared_count: usize = 0;

    // Process remaining Link accounts: remove topology key from link_topologies
    for link_account in accounts_iter {
        if link_account.data_is_empty() {
            continue;
        }
        let mut link = match Link::try_from(link_account) {
            Ok(l) => l,
            Err(_) => continue,
        };
        let before_len = link.link_topologies.len();
        link.link_topologies.retain(|k| k != topology_key);
        if link.link_topologies.len() < before_len {
            try_acc_write(&link, link_account, payer_account, accounts)?;
            cleared_count += 1;
        }
    }

    msg!(
        "TopologyClear: removed topology '{}' from {} link(s)",
        value.name,
        cleared_count
    );
    Ok(())
}
