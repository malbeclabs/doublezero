use crate::pda::get_bond_mirror_pda;
use solana_program::{account_info::AccountInfo, pubkey::Pubkey};

pub mod write;

/// Split a bond's mirror out of the trailing accounts, leaving the rest for `authorize`.
///
/// The mirror rides after the fixed accounts and is found by its address rather than its position,
/// so a caller with no bond is not forced to send one and a caller that sends only a `Permission`
/// account still works. `bond_ref` is `None` when the instruction has no bond to look for: a
/// catalog feed, or a group created on `MULTICAST_ADMIN`.
///
/// Returns `(mirror, rest)`. `rest` filters by key rather than by identity: a caller may pass the
/// mirror twice, and a stray copy left among the candidates would be read as the `Permission`
/// account.
pub(crate) fn split_bond_mirror<'a, 'b: 'a>(
    program_id: &Pubkey,
    bond_ref: Option<&Pubkey>,
    accounts_iter: &mut impl Iterator<Item = &'a AccountInfo<'b>>,
) -> (Option<&'a AccountInfo<'b>>, Vec<&'a AccountInfo<'b>>) {
    let tail: Vec<&AccountInfo> = accounts_iter.collect();
    let mirror_key = bond_ref.map(|bond_ref| get_bond_mirror_pda(program_id, bond_ref).0);
    let mirror = mirror_key.and_then(|key| tail.iter().copied().find(|a| a.key == &key));
    let rest = tail
        .into_iter()
        .filter(|a| Some(*a.key) != mirror_key)
        .collect();

    (mirror, rest)
}
