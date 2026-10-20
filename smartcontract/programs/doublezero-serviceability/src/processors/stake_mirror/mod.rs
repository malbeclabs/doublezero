use crate::pda::get_stake_mirror_pda;
use solana_program::{account_info::AccountInfo, pubkey::Pubkey};

pub mod write;

/// Split a stake's mirror out of the trailing accounts, leaving the rest for `authorize`.
///
/// The mirror rides after the fixed accounts and is found by its address rather than its position,
/// so a caller with no stake is not forced to send one and a caller that sends only a `Permission`
/// account still works. `stake_ref` is `None` when the instruction has no stake to look for: a
/// catalog feed, or a group created on `MULTICAST_ADMIN`.
///
/// Returns `(mirror, rest)`. `rest` filters by key rather than by identity: a caller may pass the
/// mirror twice, and a stray copy left among the candidates would be read as the `Permission`
/// account.
pub(crate) fn split_stake_mirror<'a, 'b: 'a>(
    program_id: &Pubkey,
    stake_ref: Option<&Pubkey>,
    accounts_iter: &mut impl Iterator<Item = &'a AccountInfo<'b>>,
) -> (Option<&'a AccountInfo<'b>>, Vec<&'a AccountInfo<'b>>) {
    let tail: Vec<&AccountInfo> = accounts_iter.collect();
    let mirror_key = stake_ref.map(|stake_ref| get_stake_mirror_pda(program_id, stake_ref).0);
    let mirror = mirror_key.and_then(|key| tail.iter().copied().find(|a| a.key == &key));
    let rest = tail
        .into_iter()
        .filter(|a| Some(*a.key) != mirror_key)
        .collect();

    (mirror, rest)
}
