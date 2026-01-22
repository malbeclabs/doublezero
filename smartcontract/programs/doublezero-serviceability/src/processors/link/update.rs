use crate::{
    error::DoubleZeroError,
    serializer::try_acc_write,
    state::{contributor::Contributor, device::Device, globalstate::GlobalState, link::*},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use doublezero_program_common::validate_account_code;

use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    pubkey::Pubkey,
};
#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct LinkUpdateArgs {
    pub code: Option<String>,
    pub contributor_pk: Option<Pubkey>,
    pub tunnel_type: Option<LinkLinkType>,
    pub bandwidth: Option<u64>,
    pub mtu: Option<u32>,
    pub delay_ns: Option<u64>,
    pub jitter_ns: Option<u64>,
    pub status: Option<LinkStatus>,
    pub delay_override_ns: Option<u64>,
    pub desired_status: Option<LinkDesiredStatus>,
}

impl fmt::Debug for LinkUpdateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let mut parts = Vec::new();
        if let Some(ref code) = self.code {
            parts.push(format!("code: {:?}", code));
        }
        if let Some(ref contributor_pk) = self.contributor_pk {
            parts.push(format!("contributor_pk: {:?}", contributor_pk));
        }
        if let Some(ref tunnel_type) = self.tunnel_type {
            parts.push(format!("tunnel_type: {:?}", tunnel_type));
        }
        if let Some(bandwidth) = self.bandwidth {
            parts.push(format!("bandwidth: {:?}", bandwidth));
        }
        if let Some(mtu) = self.mtu {
            parts.push(format!("mtu: {:?}", mtu));
        }
        if let Some(delay_ns) = self.delay_ns {
            parts.push(format!("delay_ns: {:?}", delay_ns));
        }
        if let Some(jitter_ns) = self.jitter_ns {
            parts.push(format!("jitter_ns: {:?}", jitter_ns));
        }
        if let Some(ref status) = self.status {
            parts.push(format!("status: {:?}", status));
        }
        if let Some(delay_override_ns) = self.delay_override_ns {
            parts.push(format!("delay_override_ns: {:?}", delay_override_ns));
        }
        if let Some(ref desired_status) = self.desired_status {
            parts.push(format!("desired_status: {:?}", desired_status));
        }
        write!(f, "{}", parts.join(", "))
    }
}

pub fn process_update_link(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &LinkUpdateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let link_account = next_account_info(accounts_iter)?;
    let contributor_account = next_account_info(accounts_iter)?;
    let side_z_account: Option<&AccountInfo> = if accounts.len() > 5 {
        Some(next_account_info(accounts_iter)?)
    } else {
        None
    };
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_update_link({:?})", value);

    // Check if the payer is a signer
    assert!(
        payer_account.is_signer,
        "Payer must be a signer {:?}",
        payer_account
    );

    // Check the owner of the accounts
    assert_eq!(link_account.owner, program_id, "Invalid PDA Account Owner");
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(link_account.is_writable, "PDA Account is not writable");

    let globalstate = GlobalState::try_from(globalstate_account)?;
    let contributor = Contributor::try_from(contributor_account)?;

    if contributor.owner != *payer_account.key
        && !globalstate.foundation_allowlist.contains(payer_account.key)
    {
        msg!("contributor owner: {:?}", contributor.owner);
        return Err(DoubleZeroError::NotAllowed.into());
    }
    if let Some(side_z_account) = side_z_account {
        if side_z_account.owner != program_id {
            return Err(DoubleZeroError::InvalidAccountOwner.into());
        }
    }

    // Deserialize the optional side_z device account
    let side_z: Option<Device> = if let Some(side_z_account) = side_z_account {
        Some(Device::try_from(side_z_account)?)
    } else {
        None
    };

    // Deserialize the link account
    let mut link: Link = Link::try_from(link_account)?;

    if side_z.is_none() {
        // Link should be owned by the contributor A
        if link.contributor_pk != *contributor_account.key {
            msg!("link contributor_pk: {:?}", link.contributor_pk);
            return Err(DoubleZeroError::NotAllowed.into());
        }
    } else if let Some(side_z) = side_z {
        // Link should be owned by the side_z device's contributor B
        if link.side_z_pk != *side_z_account.unwrap().key {
            return Err(DoubleZeroError::InvalidAccountOwner.into());
        }
        if side_z.contributor_pk != *contributor_account.key {
            msg!("side_z contributor_pk: {:?}", side_z.contributor_pk);
            return Err(DoubleZeroError::NotAllowed.into());
        }
    }

    // can be updated by contributor A
    if link.contributor_pk == *contributor_account.key {
        if let Some(ref code) = value.code {
            link.code =
                validate_account_code(code).map_err(|_| DoubleZeroError::InvalidAccountCode)?;
        }
        if let Some(tunnel_type) = value.tunnel_type {
            link.link_type = tunnel_type;
        }
        if let Some(bandwidth) = value.bandwidth {
            link.bandwidth = bandwidth;
        }
        if let Some(mtu) = value.mtu {
            link.mtu = mtu;
        }
        if let Some(delay_ns) = value.delay_ns {
            link.delay_ns = delay_ns;
        }
        if let Some(jitter_ns) = value.jitter_ns {
            link.jitter_ns = jitter_ns;
        }
    }
    // Can be updated by both contributors A and B
    if let Some(delay_override_ns) = value.delay_override_ns {
        link.delay_override_ns = delay_override_ns;
    }
    if let Some(desired_status) = value.desired_status {
        link.desired_status = desired_status;
    }

    if let Some(status) = value.status {
        // Only foundation allowlist can update the status directly
        if globalstate.foundation_allowlist.contains(payer_account.key) {
            link.status = status;
        } else {
            return Err(DoubleZeroError::NotAllowed.into());
        }
    }

    link.check_status_transition();

    try_acc_write(&link, link_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Updated: {:?}", link);

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_deserialize_link_update_args_before_delay_override_ns() {
        #[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default, Debug)]
        pub struct LinkUpdateArgsBeforeDelayOverrideNs {
            pub code: Option<String>,
            pub contributor_pk: Option<Pubkey>,
            pub tunnel_type: Option<LinkLinkType>,
            pub bandwidth: Option<u64>,
            pub mtu: Option<u32>,
            pub delay_ns: Option<u64>,
            pub jitter_ns: Option<u64>,
            pub status: Option<LinkStatus>,
        }

        let contributor_pk = Pubkey::new_unique();

        let args_before = LinkUpdateArgsBeforeDelayOverrideNs {
            code: Some("test-code".to_string()),
            contributor_pk: Some(contributor_pk),
            tunnel_type: Some(LinkLinkType::WAN),
            bandwidth: Some(10_000_000_000),
            mtu: Some(1500),
            delay_ns: Some(1_000_000),
            jitter_ns: Some(100_000),
            status: Some(LinkStatus::Activated),
        };

        let expected = LinkUpdateArgs {
            code: Some("test-code".to_string()),
            contributor_pk: Some(contributor_pk),
            tunnel_type: Some(LinkLinkType::WAN),
            bandwidth: Some(10_000_000_000),
            mtu: Some(1500),
            delay_ns: Some(1_000_000),
            jitter_ns: Some(100_000),
            status: Some(LinkStatus::Activated),
            delay_override_ns: None,
            desired_status: None,
        };

        let serialized = borsh::to_vec(&args_before).unwrap();
        let deserialized = LinkUpdateArgs::try_from(&serialized[..]).unwrap();

        assert_eq!(expected, deserialized);
    }
}
