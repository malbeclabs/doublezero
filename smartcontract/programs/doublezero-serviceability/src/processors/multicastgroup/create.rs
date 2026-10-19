use crate::{
    authorize::authorize,
    error::{DoubleZeroError, Validate},
    pda::{get_multicastgroup_pda, get_resource_extension_pda},
    processors::{
        resource::allocate_ip, stake_mirror::split_stake_mirror,
        validation::validate_program_account,
    },
    resource::ResourceType,
    seeds::{SEED_MULTICAST_GROUP, SEED_PREFIX},
    serializer::{try_acc_create, try_acc_write},
    state::{
        accounttype::AccountType,
        feature_flags::{is_feature_enabled, FeatureFlag},
        globalstate::GlobalState,
        multicastgroup::*,
        permission::permission_flags,
        stake_mirror::StakeMirror,
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

/// How many multicast groups one stake may create. Each group takes an address from a pool every
/// feed shares, and a bond comes back after its hold, so without a limit one bond could empty the
/// pool and a few could keep it empty.
pub const MAX_GROUPS_PER_STAKE: u8 = 4;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct MulticastGroupCreateArgs {
    pub code: String,
    pub max_bandwidth: u64,
    pub owner: Pubkey,
    /// When true, onchain allocation is used (ResourceExtension accounts required).
    /// Performs atomic create+allocate+activate in a single transaction.
    #[incremental(default = false)]
    pub use_onchain_allocation: bool,
    /// The `BuilderStake` PDA on Solana whose mirror admits this create in place of
    /// `MULTICAST_ADMIN`. Zero is the admin path, unchanged from before RFC-28.
    #[incremental(default = Pubkey::default())]
    pub stake_ref: Pubkey,
}

impl fmt::Debug for MulticastGroupCreateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "code: {}, max_bandwidth: {}, owner: {}, use_onchain_allocation: {}, stake_ref: {}",
            self.code, self.max_bandwidth, self.owner, self.use_onchain_allocation, self.stake_ref
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
    let mut globalstate = GlobalState::try_from(globalstate_account)?;
    globalstate.account_index += 1;

    // Get the PDA pubkey and bump seed for the account multicastgroup & check if it matches the account
    let (expected_pda_account, bump_seed) =
        get_multicastgroup_pda(program_id, globalstate.account_index);
    assert_eq!(
        mgroup_account.key, &expected_pda_account,
        "Invalid MulticastGroup Pubkey"
    );
    // Two things admit this call: a builder's own stake, or MULTICAST_ADMIN (Permission account)
    // or foundation/sentinel (legacy). A caller naming a stake gets the stake path's answer and
    // never falls through to the admin one, so an admin creating a group for a builder sends no
    // stake and works exactly as it did.
    //
    // The tail holds the stake's StakeMirror on the stake path, and the payer's Permission account
    // or nothing on the admin path.
    let claimed_mirror = if value.stake_ref != Pubkey::default() {
        // Before the mirror is looked for, so a cluster without staked feeds answers NotAllowed
        // whatever the caller attached.
        if !is_feature_enabled(globalstate.feature_flags, FeatureFlag::AllowStakedFeeds) {
            msg!("Staked feeds are not enabled on this cluster");
            return Err(DoubleZeroError::NotAllowed.into());
        }
        let (mirror_account, _) =
            split_stake_mirror(program_id, Some(&value.stake_ref), accounts_iter);
        let mirror_account = mirror_account.ok_or(DoubleZeroError::StakeMirrorMissing)?;
        let mirror =
            verify_stake_admits_group(program_id, mirror_account, payer_account.key, value)?;
        Some((mirror_account, mirror))
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
        code,
        multicast_ip: allocate_ip(multicast_group_block_ext, 1)?.ip(),
        max_bandwidth: value.max_bandwidth,
        status: MulticastGroupStatus::Activated,
        publisher_count: 0,
        subscriber_count: 0,
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

    // Count the group against the stake that admitted it.
    if let Some((mirror_account, mut mirror)) = claimed_mirror {
        // `verify_stake_admits_group` keeps the count under the limit. Checked here as well, so
        // the counter cannot wrap if the limit or the stored count ever changes.
        mirror.groups_created = mirror
            .groups_created
            .checked_add(1)
            .ok_or(DoubleZeroError::StakeGroupLimitReached)?;
        try_acc_write(&mirror, mirror_account, payer_account, accounts)?;
    }

    Ok(())
}

/// Check that the payer's own stake admits this create, and return the mirror to charge.
///
/// Every check reads what the ledger holds. `stake_ref` and `owner` are caller-supplied arguments,
/// so two lines carry the weight. The mirror's `builder` must be the payer, or one builder's bond
/// creates groups for anyone who names it. The group's `owner` must be the payer too, or a bond
/// creates a group whose publish rights someone else controls.
///
/// A stake that already backs a feed still admits a create. Groups exist before the feed that
/// lists them, and a live feed can add one later, so `feed_key` is not the limit here.
/// `groups_created` is.
fn verify_stake_admits_group(
    program_id: &Pubkey,
    mirror_account: &AccountInfo,
    payer: &Pubkey,
    value: &MulticastGroupCreateArgs,
) -> Result<StakeMirror, DoubleZeroError> {
    if !mirror_account.is_writable {
        msg!("Stake mirror must be writable so the group can be counted against it");
        return Err(DoubleZeroError::InvalidArgument);
    }
    // An account at the right address is not a mirror. Anyone can name a stake nobody posted and
    // attach the empty account its seeds derive.
    if mirror_account.data_is_empty() || mirror_account.owner != program_id {
        msg!("No stake mirror written for stake {}", value.stake_ref);
        return Err(DoubleZeroError::StakeMirrorMissing);
    }

    let mirror =
        StakeMirror::try_from(mirror_account).map_err(|_| DoubleZeroError::InvalidAccountType)?;
    mirror.validate()?;

    if mirror.stake_ref != value.stake_ref {
        msg!("Stake mirror names a different stake: {}", mirror.stake_ref);
        return Err(DoubleZeroError::InvalidArgument);
    }
    if &mirror.builder != payer {
        msg!(
            "Stake {} belongs to builder {}",
            value.stake_ref,
            mirror.builder
        );
        return Err(DoubleZeroError::NotAllowed);
    }
    if &value.owner != payer {
        msg!("A group created on a stake must be owned by the builder that created it");
        return Err(DoubleZeroError::InvalidArgument);
    }
    // The same tier check `CreateFeed` makes, so a feed and the groups it lists follow one rule.
    // `builder-stake` prices a bond by the tier its committed rate falls in, so the tier is what the
    // builder paid for, and the mirror's `committed_rate_bits_per_sec` is only the record of what
    // it declared. A mirror with tier `None` covers nothing.
    //
    // The check applies to each group on its own, not to the stake: four groups can each declare
    // the full tier. Nothing enforces or bills a group's `max_bandwidth`.
    if !mirror.tier.covers(value.max_bandwidth) {
        msg!(
            "Tier {} covers up to {} bits/sec, group asks for {}",
            mirror.tier,
            mirror.tier.max_rate_bits_per_sec(),
            value.max_bandwidth
        );
        return Err(DoubleZeroError::StakeDoesNotCoverRate);
    }
    if mirror.groups_created >= MAX_GROUPS_PER_STAKE {
        msg!(
            "Stake {} has created {} groups, the most one stake may",
            value.stake_ref,
            mirror.groups_created
        );
        return Err(DoubleZeroError::StakeGroupLimitReached);
    }

    Ok(mirror)
}
