use crate::{
    error::DoubleZeroError,
    globalstate::globalstate_get,
    helper::*,
    state::{accounttype::AccountType, device::*},
    types::*,
};
use borsh::{BorshDeserialize, BorshSerialize};
use core::fmt;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct DeviceUpdateArgs {
    pub code: Option<String>,
    pub device_type: Option<DeviceType>,
    pub contributor_pk: Option<Pubkey>,
    pub public_ip: Option<std::net::Ipv4Addr>,
    pub dz_prefixes: Option<NetworkV4List>,
    pub metrics_publisher_pk: Option<Pubkey>,
    pub bgp_asn: Option<u32>,
    pub dia_bgp_asn: Option<u32>,
    pub mgmt_vrf: Option<String>,
    pub dns_servers: Option<Vec<std::net::Ipv4Addr>>,
    pub ntp_servers: Option<Vec<std::net::Ipv4Addr>>,
    pub interfaces: Option<Vec<Interface>>,
}

impl fmt::Debug for DeviceUpdateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "code: {:?}, device_type: {:?}, contributor_pk: {:?}, \
public_ip: {:?}, dz_prefixes: {:?}, metrics_publisher_pk: {:?}, \
bgp_asn: {:?}, dia_bgp_asn: {:?}, mgmt_vrf: {:?}, dns_servers: {:?}, \
ntp_servers: {:?}",
            self.code,
            self.device_type,
            self.contributor_pk,
            self.public_ip.map(|public_ip| public_ip.to_string()),
            self.dz_prefixes.as_ref().map(|net| net.to_string()),
            self.metrics_publisher_pk,
            self.bgp_asn,
            self.dia_bgp_asn,
            self.mgmt_vrf.as_ref(),
            self.dns_servers,
            self.ntp_servers,
        )
    }
}

pub fn process_update_device(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &DeviceUpdateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let device_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_update_device({:?})", value);

    // Check the owner of the accounts
    assert_eq!(
        device_account.owner, program_id,
        "Invalid PDA Account Owner"
    );
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
    assert!(device_account.is_writable, "PDA Account is not writable");

    let mut device: Device = Device::try_from(device_account)?;
    assert_eq!(
        device.account_type,
        AccountType::Device,
        "Invalid Device Account Type"
    );

    let globalstate = globalstate_get(globalstate_account)?;

    // Check if the payer is in the foundation allowlist or the owner of the device
    if !globalstate.foundation_allowlist.contains(payer_account.key)
        && device.owner != *payer_account.key
    {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    if let Some(code) = &value.code {
        device.code = code.clone();
    }
    if let Some(device_type) = value.device_type {
        device.device_type = device_type;
    }
    if let Some(contributor_pk) = value.contributor_pk {
        device.contributor_pk = contributor_pk;
    }
    if let Some(public_ip) = value.public_ip {
        device.public_ip = public_ip;
    }
    if let Some(dz_prefixes) = &value.dz_prefixes {
        device.dz_prefixes = dz_prefixes.clone();
    }
    if let Some(metrics_publisher_pk) = &value.metrics_publisher_pk {
        device.metrics_publisher_pk = *metrics_publisher_pk;
    }
    if let Some(bgp_asn) = value.bgp_asn {
        device.bgp_asn = bgp_asn;
    }
    if let Some(dia_bgp_asn) = value.dia_bgp_asn {
        device.dia_bgp_asn = dia_bgp_asn;
    }
    if let Some(mgmt_vrf) = &value.mgmt_vrf {
        device.mgmt_vrf = mgmt_vrf.clone();
    }
    if let Some(dns_servers) = &value.dns_servers {
        device.dns_servers = dns_servers.clone();
    }
    if let Some(ntp_servers) = &value.ntp_servers {
        device.ntp_servers = ntp_servers.clone();
    }
    if let Some(interfaces) = &value.interfaces {
        if interfaces
            .iter()
            .any(|i| i.version != CURRENT_INTERFACE_VERSION)
        {
            return Err(DoubleZeroError::InvalidInterfaceVersion.into());
        }
        device.interfaces = interfaces.clone();
    }

    account_write(device_account, &device, payer_account, system_program)?;

    #[cfg(test)]
    msg!("Updated: {:?}", device);

    Ok(())
}
