use crate::{
    authorize::authorize,
    error::{DoubleZeroError, Validate},
    pda::{get_index_pda, get_multicastgroup_pda, get_resource_extension_pda},
    processors::{
        bond_mirror::split_bond_mirror, index::create::create_index_account, resource::allocate_ip,
        validation::validate_program_account,
    },
    resource::ResourceType,
    seeds::{SEED_MULTICAST_GROUP, SEED_PREFIX},
    serializer::{try_acc_create, try_acc_write},
    state::{
        accounttype::AccountType,
        bond_mirror::BondMirror,
        feature_flags::{is_feature_enabled, FeatureFlag},
        globalstate::GlobalState,
        multicastgroup::*,
        permission::permission_flags,
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use doublezero_program_common::validate_account_code;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};
use std::fmt;

/// How many multicast groups one bond may create. Each group takes an address from a pool every
/// feed shares, and a bond comes back after its hold, so without a limit one bond could empty the
/// pool and a few could keep it empty.
pub const MAX_GROUPS_PER_BOND: u8 = 4;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct MulticastGroupCreateArgs {
    pub code: String,
    pub max_bandwidth: u64,
    pub owner: Pubkey,
    /// When true, onchain allocation is used (ResourceExtension accounts required).
    /// Performs atomic create+allocate+activate in a single transaction.
    #[incremental(default = false)]
    pub use_onchain_allocation: bool,
    /// The `BuilderBond` PDA on Solana whose mirror admits this create in place of
    /// `MULTICAST_ADMIN`. Zero is the admin path, unchanged from before RFC-28.
    #[incremental(default = Pubkey::default())]
    pub bond_ref: Pubkey,
}

impl fmt::Debug for MulticastGroupCreateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "code: {}, max_bandwidth: {}, owner: {}, use_onchain_allocation: {}, bond_ref: {}",
            self.code, self.max_bandwidth, self.owner, self.use_onchain_allocation, self.bond_ref
        )
    }
}

pub fn process_create_multicastgroup(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &MulticastGroupCreateArgs,
) -> ProgramResult {
    if !value.use_onchain_allocation {
        return Err(DoubleZeroError::InvalidArgument.into());
    }

    let accounts_iter = &mut accounts.iter();

    let mgroup_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Account layout: [mgroup, globalstate, multicast_group_block, payer, system]
    let multicast_group_block_ext = next_account_info(accounts_iter)?;

    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_create_multicastgroup({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Validate and normalize code
    let code =
        validate_account_code(&value.code).map_err(|_| DoubleZeroError::InvalidAccountCode)?;

    // Check the owner of the accounts
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(mgroup_account.is_writable, "PDA Account is not writable");

    // Parse the global state account & check if the payer is in the allowlist
    let mut globalstate = GlobalState::load(program_id, globalstate_account)?;
    globalstate.account_index += 1;

    // Get the PDA pubkey and bump seed for the account multicastgroup & check if it matches the account
    let (expected_pda_account, bump_seed) =
        get_multicastgroup_pda(program_id, globalstate.account_index);
    assert_eq!(
        mgroup_account.key, &expected_pda_account,
        "Invalid MulticastGroup Pubkey"
    );
    // Two things admit this call: a builder's own bond, or MULTICAST_ADMIN (Permission account)
    // or foundation/sentinel (legacy). A caller naming a bond gets the bond path's answer and
    // never falls through to the admin one, so an admin creating a group for a builder sends no
    // bond and works exactly as it did.
    //
    // The tail holds the bond's BondMirror on the bond path, and the payer's Permission account
    // or nothing on the admin path.
    let claimed_mirror = if value.bond_ref != Pubkey::default() {
        // Before the mirror is looked for, so a cluster without bonded feeds answers NotAllowed
        // whatever the caller attached.
        if !is_feature_enabled(globalstate.feature_flags, FeatureFlag::AllowBondedFeeds) {
            msg!("Bonded feeds are not enabled on this cluster");
            return Err(DoubleZeroError::NotAllowed.into());
        }
        let (mirror_account, rest) =
            split_bond_mirror(program_id, Some(&value.bond_ref), accounts_iter);
        let mirror_account = mirror_account.ok_or(DoubleZeroError::BondMirrorMissing)?;
        let mirror =
            verify_bond_admits_group(program_id, mirror_account, payer_account.key, value)?;

        // A bonded group claims its code in the Index, so a duplicate fails. Anyone with a bond can
        // create a group, and clients resolve a group by its code and take the first match, so two
        // groups with one code would send an admin's grant or a subscriber to the wrong group.
        // Found by its address, like the mirror. Checked here, before anything is allocated, so a
        // taken code says so rather than failing inside the Index create below.
        let (index_key, _) = get_index_pda(program_id, SEED_MULTICAST_GROUP, &code);
        let index_account = rest
            .into_iter()
            .find(|a| a.key == &index_key)
            .ok_or_else(|| {
                msg!(
                    "A bonded group must pass the Index account for its code, {}",
                    index_key
                );
                DoubleZeroError::InvalidArgument
            })?;
        if !index_account.data_is_empty() {
            msg!("Multicast group code {} is already taken", code);
            return Err(ProgramError::AccountAlreadyInitialized);
        }
        Some((mirror_account, mirror, index_account))
    } else {
        authorize(
            program_id,
            accounts_iter,
            payer_account.key,
            &globalstate,
            permission_flags::MULTICAST_ADMIN,
        )?;
        None
    };

    // Check if the account is already initialized
    if !mgroup_account.data_is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }

    // Atomic create+allocate+activate.
    let (expected_pda, _, _) =
        get_resource_extension_pda(program_id, ResourceType::MulticastGroupBlock);
    validate_program_account!(
        multicast_group_block_ext,
        program_id,
        writable = true,
        pda = &expected_pda,
        "MulticastGroupBlock"
    );

    let multicastgroup = MulticastGroup {
        account_type: AccountType::MulticastGroup,
        owner: value.owner,
        index: globalstate.account_index,
        bump_seed,
        tenant_pk: Pubkey::default(),
        code: code.clone(),
        multicast_ip: allocate_ip(multicast_group_block_ext, 1)?.ip(),
        max_bandwidth: value.max_bandwidth,
        status: MulticastGroupStatus::Activated,
        publisher_count: 0,
        subscriber_count: 0,
        // Zero on the admin path, which is what marks a group an admin created.
        bond_ref: value.bond_ref,
    };

    try_acc_create(
        &multicastgroup,
        mgroup_account,
        payer_account,
        system_program,
        program_id,
        &[
            SEED_PREFIX,
            SEED_MULTICAST_GROUP,
            &globalstate.account_index.to_le_bytes(),
            &[bump_seed],
        ],
    )?;
    try_acc_write(&globalstate, globalstate_account, payer_account, accounts)?;

    // Count the group against the bond that admitted it, and claim its code.
    if let Some((mirror_account, mut mirror, index_account)) = claimed_mirror {
        create_index_account(
            program_id,
            index_account,
            mgroup_account,
            payer_account,
            system_program,
            SEED_MULTICAST_GROUP,
            &code,
        )?;
        // `verify_bond_admits_group` keeps the count under the limit. Checked here as well, so
        // the counter cannot wrap if the limit or the stored count ever changes.
        mirror.groups_created = mirror
            .groups_created
            .checked_add(1)
            .ok_or(DoubleZeroError::BondGroupLimitReached)?;
        try_acc_write(&mirror, mirror_account, payer_account, accounts)?;
    }

    Ok(())
}

/// Check that the payer's own bond admits this create, and return the mirror to charge.
///
/// Every check reads what the ledger holds. `bond_ref` and `owner` are caller-supplied arguments,
/// so two lines carry the weight. The mirror's `builder` must be the payer, or one builder's bond
/// creates groups for anyone who names it. The group's `owner` must be the payer too, or a bond
/// creates a group whose publish rights someone else controls.
///
/// A bond that already backs a feed still admits a create. Groups exist before the feed that
/// lists them, and a live feed can add one later, so `feed_key` is not the limit here.
/// `groups_created` is.
fn verify_bond_admits_group(
    program_id: &Pubkey,
    mirror_account: &AccountInfo,
    payer: &Pubkey,
    value: &MulticastGroupCreateArgs,
) -> Result<BondMirror, DoubleZeroError> {
    if !mirror_account.is_writable {
        msg!("Bond mirror must be writable so the group can be counted against it");
        return Err(DoubleZeroError::InvalidArgument);
    }
    // An account at the right address is not a mirror. Anyone can name a bond nobody posted and
    // attach the empty account its seeds derive.
    if mirror_account.data_is_empty() || mirror_account.owner != program_id {
        msg!("No bond mirror written for bond {}", value.bond_ref);
        return Err(DoubleZeroError::BondMirrorMissing);
    }

    let mirror =
        BondMirror::try_from(mirror_account).map_err(|_| DoubleZeroError::InvalidAccountType)?;
    mirror.validate()?;

    if mirror.bond_ref != value.bond_ref {
        msg!("Bond mirror names a different bond: {}", mirror.bond_ref);
        return Err(DoubleZeroError::InvalidArgument);
    }
    if &mirror.builder != payer {
        msg!(
            "Bond {} belongs to builder {}",
            value.bond_ref,
            mirror.builder
        );
        return Err(DoubleZeroError::NotAllowed);
    }
    if &value.owner != payer {
        msg!("A group created on a bond must be owned by the builder that created it");
        return Err(DoubleZeroError::InvalidArgument);
    }
    // The same tier check `CreateFeed` makes, so a feed and the groups it lists follow one rule.
    // `builder-bond` prices a bond by the tier its committed rate falls in, so the tier is what the
    // builder paid for, and the mirror's `committed_rate_bits_per_sec` is only the record of what
    // it declared. A mirror with tier `None` covers nothing.
    //
    // The check applies to each group on its own, not to the bond: four groups can each declare
    // the full tier. Nothing enforces or bills a group's `max_bandwidth`.
    if !mirror.tier.covers(value.max_bandwidth) {
        msg!(
            "Tier {} covers up to {} bits/sec, group asks for {}",
            mirror.tier,
            mirror.tier.max_rate_bits_per_sec(),
            value.max_bandwidth
        );
        return Err(DoubleZeroError::BondDoesNotCoverRate);
    }
    if mirror.groups_created >= MAX_GROUPS_PER_BOND {
        msg!(
            "Bond {} has created {} groups, the most one bond may",
            value.bond_ref,
            mirror.groups_created
        );
        return Err(DoubleZeroError::BondGroupLimitReached);
    }

    Ok(mirror)
}
