use crate::{
    authorize::authorize,
    error::DoubleZeroError,
    pda::get_bond_mirror_pda,
    seeds::{SEED_BOND_MIRROR, SEED_PREFIX},
    serializer::{try_acc_create, try_acc_write},
    state::{
        accounttype::AccountType,
        bond_mirror::{BondMirror, BondTier},
        feature_flags::{is_feature_enabled, FeatureFlag},
        globalstate::GlobalState,
        permission::permission_flags,
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

#[derive(BorshSerialize, BorshDeserializeIncremental, Clone, PartialEq)]
pub struct BondMirrorWriteArgs {
    /// The `BuilderBond` account on Solana this mirrors. The PDA seed, so it is immutable.
    pub bond_ref: Pubkey,
    pub builder: Pubkey,
    pub tier: BondTier,
    pub committed_rate_bits_per_sec: u64,
    /// The Solana slot these values were read at.
    pub source_slot: u64,
}

impl fmt::Debug for BondMirrorWriteArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "bond_ref: {}, builder: {}, tier: {}, committed_rate_bits_per_sec: {}, source_slot: {}",
            self.bond_ref,
            self.builder,
            self.tier,
            self.committed_rate_bits_per_sec,
            self.source_slot
        )
    }
}

/// Copy a builder's Solana bond onto the DZ ledger.
///
/// This is an assertion, not a proof. A DZ ledger program cannot read a Solana account, so the
/// caller is a relayer reporting what it saw, and the mirror is worth exactly as much as the key
/// that signed it. `BOND_ORACLE` is that key, revocable through the `Permission` account. See the
/// trust anchors section of the RFC-28 architecture notes.
///
/// Accounts: `[bond_mirror, globalstate, payer, system_program, (permission)]`.
pub fn process_write_bond_mirror(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &BondMirrorWriteArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let bond_mirror_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    assert!(payer_account.is_signer, "Payer must be a signer");
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert!(
        bond_mirror_account.is_writable,
        "BondMirror Account is not writable"
    );

    let globalstate = GlobalState::load(program_id, globalstate_account)?;
    authorize(
        program_id,
        accounts_iter,
        payer_account.key,
        &globalstate,
        permission_flags::BOND_ORACLE,
    )?;

    // Ships dormant with the rest of RFC-28. A cluster that cannot create a bonded feed has no
    // use for a mirror, and this keeps the whole feature behind one switch.
    if !is_feature_enabled(globalstate.feature_flags, FeatureFlag::AllowBondedFeeds) {
        msg!("Bonded feeds are not enabled on this cluster");
        return Err(DoubleZeroError::NotAllowed.into());
    }

    if value.bond_ref == Pubkey::default() {
        msg!("BondMirror must name the bond it mirrors");
        return Err(DoubleZeroError::InvalidArgument.into());
    }
    if value.builder == Pubkey::default() {
        msg!("BondMirror must name a builder");
        return Err(DoubleZeroError::InvalidArgument.into());
    }

    let (expected_pda, bump_seed) = get_bond_mirror_pda(program_id, &value.bond_ref);
    assert_eq!(
        bond_mirror_account.key, &expected_pda,
        "Invalid BondMirror PubKey"
    );

    // A mirror that already exists carries two things this write must not destroy: the feed that
    // claimed the bond, and the slot the stored values came from.
    let existing = if bond_mirror_account.data_is_empty() {
        None
    } else {
        Some(BondMirror::try_from(bond_mirror_account)?)
    };

    if let Some(existing) = &existing {
        // Polling makes a repeated write harmless. It does nothing about ordering: a retry
        // carrying an older read can arrive after a fresher one and walk the mirror backwards.
        // The program enforces the ordering rather than trusting the relayer to.
        if value.source_slot <= existing.source_slot {
            msg!(
                "Bond mirror already holds slot {}, refusing a write from slot {}",
                existing.source_slot,
                value.source_slot
            );
            return Err(DoubleZeroError::InvalidArgument.into());
        }
        // The builder is not a seed here, so a write could otherwise reassign a funded bond to
        // somebody else's builder key.
        if existing.builder != value.builder {
            msg!("Bond mirror names builder {}", existing.builder);
            return Err(DoubleZeroError::InvalidArgument.into());
        }
    }

    let mirror = BondMirror {
        account_type: AccountType::BondMirror,
        owner: existing
            .as_ref()
            .map(|e| e.owner)
            .unwrap_or(*payer_account.key),
        bump_seed,
        bond_ref: value.bond_ref,
        builder: value.builder,
        tier: value.tier,
        committed_rate_bits_per_sec: value.committed_rate_bits_per_sec,
        source_slot: value.source_slot,
        // Whoever signed this write is what the mirror is worth. Always the current signer, so
        // revoking a key and rewriting reassigns the blame.
        relayer: *payer_account.key,
        // Carried forward, never taken from the caller. `CreateFeed` sets it to claim the bond,
        // and zeroing it here would let a second feed spend the same bond.
        feed_key: existing.as_ref().map(|e| e.feed_key).unwrap_or_default(),
        // Carried forward for the same reason. Zeroing it would hand the bond a fresh set of
        // multicast addresses on every relayer write.
        groups_created: existing.map(|e| e.groups_created).unwrap_or_default(),
    };

    if bond_mirror_account.data_is_empty() {
        try_acc_create(
            &mirror,
            bond_mirror_account,
            payer_account,
            system_program,
            program_id,
            &[
                SEED_PREFIX,
                SEED_BOND_MIRROR,
                value.bond_ref.as_ref(),
                &[bump_seed],
            ],
        )?;
    } else {
        try_acc_write(&mirror, bond_mirror_account, payer_account, accounts)?;
    }

    msg!(
        "Mirrored bond {} for builder {} at slot {}",
        value.bond_ref,
        value.builder,
        value.source_slot
    );

    Ok(())
}
