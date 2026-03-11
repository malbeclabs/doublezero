use crate::{
    error::DoubleZeroError,
    pda::{get_accesspass_pda, get_user_old_pda, get_user_pda},
    state::{
        accesspass::{AccessPass, AccessPassStatus, AccessPassType},
        accounttype::AccountType,
        device::{Device, DeviceStatus},
        globalstate::GlobalState,
        tenant::Tenant,
        user::*,
    },
};
use doublezero_program_common::types::NetworkV4;
use solana_program::{
    account_info::AccountInfo, clock::Clock, msg, program_error::ProgramError, pubkey::Pubkey,
    sysvar::Sysvar,
};
use std::net::Ipv4Addr;

use crate::{processors::validation::validate_program_account, serializer::try_acc_write};

#[derive(PartialEq)]
pub enum PDAVersion {
    V1,
    V2,
}

/// Accounts needed by `create_user_core`.
pub struct CreateUserCoreAccounts<'a, 'b> {
    pub user_account: &'a AccountInfo<'b>,
    pub device_account: &'a AccountInfo<'b>,
    pub accesspass_account: &'a AccountInfo<'b>,
    pub globalstate_account: &'a AccountInfo<'b>,
    pub tenant_account: Option<&'a AccountInfo<'b>>,
    pub payer_account: &'a AccountInfo<'b>,
}

/// Result returned by `create_user_core` containing mutable state for callers to finish writing.
pub struct CreateUserCoreResult {
    pub user: User,
    pub device: Device,
    pub accesspass: AccessPass,
    pub globalstate: GlobalState,
    pub pda_ver: PDAVersion,
    pub bump_old_seed: u8,
    pub bump_seed: u8,
}

/// Shared validation and state setup for CreateUser and CreateSubscribeUser.
///
/// Performs all common checks (payer signer, account emptiness, access pass validation,
/// PDA derivation, device validation, max users checks, epoch check) and sets up the
/// initial User struct with Pending status.
///
/// Callers are responsible for:
/// - Parsing resource extension accounts (if dz_prefix_count > 0)
/// - Onchain allocation + try_activate
/// - Account creation (try_acc_create) and write-back
/// - Multicast subscription logic (CreateSubscribeUser only)
#[allow(clippy::too_many_arguments)]
pub fn create_user_core(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    core: &CreateUserCoreAccounts,
    user_type: UserType,
    cyoa_type: UserCYOA,
    client_ip: Ipv4Addr,
    tunnel_endpoint: Ipv4Addr,
    is_publisher: bool,
) -> Result<CreateUserCoreResult, ProgramError> {
    // Check if the payer is a signer
    assert!(core.payer_account.is_signer, "Payer must be a signer");

    if !core.user_account.data_is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }
    if core.accesspass_account.data_is_empty() {
        return Err(DoubleZeroError::AccessPassNotFound.into());
    }
    validate_program_account!(
        core.accesspass_account,
        program_id,
        writable = false,
        pda = None::<&Pubkey>,
        "AccessPass"
    );

    // Validate tenant account if provided
    if let Some(tenant_account) = core.tenant_account {
        if tenant_account.data_is_empty() {
            return Err(DoubleZeroError::InvalidTenantPubkey.into());
        }

        validate_program_account!(
            tenant_account,
            program_id,
            writable = true,
            pda = None::<&Pubkey>,
            "Tenant"
        );

        if tenant_account.data.borrow()[0] != AccountType::Tenant as u8 {
            return Err(DoubleZeroError::InvalidAccountType.into());
        }
    }

    let mut globalstate = GlobalState::try_from(core.globalstate_account)?;
    globalstate.account_index += 1;

    let (expected_old_pda_account, bump_old_seed) =
        get_user_old_pda(program_id, globalstate.account_index);
    let (expected_pda_account, bump_seed) = get_user_pda(program_id, &client_ip, user_type);

    let pda_ver = if core.user_account.key == &expected_pda_account {
        PDAVersion::V2
    } else if core.user_account.key == &expected_old_pda_account {
        PDAVersion::V1
    } else {
        msg!(
            "Invalid User PDA. expected: {} or {}, found: {}",
            expected_pda_account,
            expected_old_pda_account,
            core.user_account.key
        );
        return Err(DoubleZeroError::InvalidUserPubkey.into());
    };

    // Check account Types
    if core.device_account.data_is_empty()
        || core.device_account.data.borrow()[0] != AccountType::Device as u8
    {
        return Err(DoubleZeroError::InvalidDevicePubkey.into());
    }
    if core.device_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }

    let (accesspass_pda, _) = get_accesspass_pda(program_id, &client_ip, core.payer_account.key);
    let (accesspass_dynamic_pda, _) =
        get_accesspass_pda(program_id, &Ipv4Addr::UNSPECIFIED, core.payer_account.key);
    assert!(
        core.accesspass_account.key == &accesspass_pda
            || core.accesspass_account.key == &accesspass_dynamic_pda,
        "Invalid AccessPass PDA",
    );

    // Read Access Pass
    let mut accesspass = AccessPass::try_from(core.accesspass_account)?;
    if accesspass.user_payer != *core.payer_account.key {
        msg!(
            "Invalid user_payer accesspass.{{user_payer: {}}} = {{ user_payer: {} }}",
            accesspass.user_payer,
            core.payer_account.key
        );
        return Err(DoubleZeroError::Unauthorized.into());
    }
    if accesspass.is_dynamic() && accesspass.client_ip == Ipv4Addr::UNSPECIFIED {
        accesspass.client_ip = client_ip; // lock to the first used IP
    }
    if !accesspass.allow_multiple_ip() && accesspass.client_ip != client_ip {
        msg!(
            "Invalid client_ip accesspass.{{client_ip: {}}} = {{ client_ip: {} }}",
            accesspass.client_ip,
            client_ip
        );
        return Err(DoubleZeroError::Unauthorized.into());
    }

    // Enforce tenant_allowlist: if access-pass has a non-default tenant in its
    // allowlist, the user's tenant must be in that list.
    if accesspass
        .tenant_allowlist
        .iter()
        .any(|pk| *pk != Pubkey::default())
    {
        let user_tenant_pk = core.tenant_account.map(|a| *a.key).unwrap_or_default();
        if !accesspass.tenant_allowlist.contains(&user_tenant_pk) {
            msg!(
                "Tenant {} not in access-pass tenant_allowlist {:?}",
                user_tenant_pk,
                accesspass.tenant_allowlist
            );
            return Err(DoubleZeroError::TenantNotInAccessPassAllowlist.into());
        }
    } else if let Some(tenant_account) = core.tenant_account {
        let tenant = Tenant::try_from(tenant_account)?;
        msg!(
            "Access-pass has no tenant_allowlist, but user creation specifies tenant {}",
            tenant.code
        );
        return Err(DoubleZeroError::TenantNotInAccessPassAllowlist.into());
    }

    // Check Initial epoch
    let clock = Clock::get()?;
    let current_epoch = clock.epoch;
    if accesspass.last_access_epoch < current_epoch {
        msg!(
            "Invalid epoch last_access_epoch: {} < current_epoch: {}",
            accesspass.last_access_epoch,
            current_epoch
        );
        return Err(DoubleZeroError::AccessPassUnauthorized.into());
    }

    // Read validator_pubkey from AccessPass
    let validator_pubkey = match &accesspass.accesspass_type {
        AccessPassType::SolanaValidator(pk) => *pk,
        _ => Pubkey::default(),
    };

    let mut device = Device::try_from(core.device_account)?;

    let is_qa = globalstate.qa_allowlist.contains(core.payer_account.key);

    // Only activated devices can have users, or if in foundation allowlist
    if device.status != DeviceStatus::Activated
        && !globalstate
            .foundation_allowlist
            .contains(core.payer_account.key)
        && !is_qa
    {
        msg!("{:?}", device);
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    if device.users_count + device.reserved_seats >= device.max_users && !is_qa {
        msg!("{:?}", device);
        return Err(DoubleZeroError::MaxUsersExceeded.into());
    }

    // Check per-type limits (when max > 0, the limit is enforced)
    match user_type {
        UserType::Multicast => {
            if is_publisher {
                if device.max_multicast_publishers > 0
                    && device.multicast_publishers_count >= device.max_multicast_publishers
                    && !is_qa
                {
                    msg!(
                        "Max multicast publishers exceeded: count={}, max={}",
                        device.multicast_publishers_count,
                        device.max_multicast_publishers
                    );
                    return Err(DoubleZeroError::MaxMulticastPublishersExceeded.into());
                }
            } else if device.max_multicast_subscribers > 0
                && device.multicast_subscribers_count >= device.max_multicast_subscribers
                && !is_qa
            {
                msg!(
                    "Max multicast subscribers exceeded: count={}, max={}",
                    device.multicast_subscribers_count,
                    device.max_multicast_subscribers
                );
                return Err(DoubleZeroError::MaxMulticastSubscribersExceeded.into());
            }
        }
        _ => {
            if device.max_unicast_users > 0
                && device.unicast_users_count >= device.max_unicast_users
                && !is_qa
            {
                msg!(
                    "Max unicast users exceeded: count={}, max={}",
                    device.unicast_users_count,
                    device.max_unicast_users
                );
                return Err(DoubleZeroError::MaxUnicastUsersExceeded.into());
            }
        }
    }

    // All validations passed - now update counters
    accesspass.connection_count += 1;
    accesspass.status = AccessPassStatus::Connected;

    device.reference_count += 1;
    device.users_count += 1;
    match user_type {
        UserType::Multicast => {
            if is_publisher {
                device.multicast_publishers_count += 1;
            } else {
                device.multicast_subscribers_count += 1;
            }
        }
        _ => {
            device.unicast_users_count += 1;
        }
    }

    // Handle tenant reference counting and get tenant_pk
    let tenant_pk = if let Some(tenant_account) = core.tenant_account {
        let mut tenant = Tenant::try_from(tenant_account)?;

        tenant.reference_count = tenant
            .reference_count
            .checked_add(1)
            .ok_or(DoubleZeroError::InvalidIndex)?;

        try_acc_write(&tenant, tenant_account, core.payer_account, accounts)?;

        *tenant_account.key
    } else {
        Pubkey::default()
    };

    let user = User {
        account_type: AccountType::User,
        owner: *core.payer_account.key,
        bump_seed: if pda_ver == PDAVersion::V1 {
            bump_old_seed
        } else {
            bump_seed
        },
        index: if pda_ver == PDAVersion::V1 {
            globalstate.account_index
        } else {
            0
        },
        tenant_pk,
        user_type,
        device_pk: *core.device_account.key,
        cyoa_type,
        client_ip,
        dz_ip: Ipv4Addr::UNSPECIFIED,
        tunnel_id: 0,
        tunnel_net: NetworkV4::default(),
        status: UserStatus::Pending,
        publishers: vec![],
        subscribers: vec![],
        validator_pubkey,
        tunnel_endpoint,
    };

    Ok(CreateUserCoreResult {
        user,
        device,
        accesspass,
        globalstate,
        pda_ver,
        bump_old_seed,
        bump_seed,
    })
}
