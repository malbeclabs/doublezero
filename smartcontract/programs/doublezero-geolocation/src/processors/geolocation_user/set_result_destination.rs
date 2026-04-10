use crate::{
    error::GeolocationError,
    serializer::try_acc_write,
    state::{geo_probe::GeoProbe, geolocation_user::GeolocationUser},
    validation::validate_public_ip,
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};
use std::{collections::HashSet, net::Ipv4Addr};

#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone)]
pub struct SetResultDestinationArgs {
    pub destination: String,
}

// RFC 1035 §2.3.4
const MAX_DOMAIN_LENGTH: usize = 253;
const MAX_LABEL_LENGTH: usize = 63;

fn validate_domain(host: &str) -> Result<(), ProgramError> {
    if host.len() > MAX_DOMAIN_LENGTH {
        msg!("Domain too long: {} chars", host.len());
        return Err(ProgramError::InvalidInstructionData);
    }

    let labels: Vec<&str> = host.split('.').collect();
    if labels.len() < 2 {
        msg!("Domain must have at least two labels");
        return Err(ProgramError::InvalidInstructionData);
    }

    for label in &labels {
        if label.is_empty() || label.len() > MAX_LABEL_LENGTH {
            msg!("Invalid domain label length: {}", label.len());
            return Err(ProgramError::InvalidInstructionData);
        }
        if label.starts_with('-') || label.ends_with('-') {
            msg!("Domain label cannot start or end with hyphen");
            return Err(ProgramError::InvalidInstructionData);
        }
        if !label.chars().all(|c| c.is_ascii_alphanumeric() || c == '-') {
            msg!("Domain label contains invalid characters");
            return Err(ProgramError::InvalidInstructionData);
        }
    }

    Ok(())
}

fn validate_destination(destination: &str) -> Result<(), ProgramError> {
    let colon_pos = destination.rfind(':').ok_or_else(|| {
        msg!("Invalid destination format: missing port separator");
        ProgramError::InvalidInstructionData
    })?;
    let host = &destination[..colon_pos];
    let port_str = &destination[colon_pos + 1..];

    let _port: u16 = port_str.parse().map_err(|_| {
        msg!("Invalid destination port: {}", port_str);
        ProgramError::InvalidInstructionData
    })?;

    // Try as IP first
    if let Ok(ip) = host.parse::<Ipv4Addr>() {
        validate_public_ip(&ip)?;
        return Ok(());
    }

    // Validate as domain
    validate_domain(host)?;
    Ok(())
}

pub fn process_set_result_destination(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    args: &SetResultDestinationArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    if !payer_account.is_signer {
        msg!("Payer must be a signer");
        return Err(ProgramError::MissingRequiredSignature);
    }

    if user_account.owner != program_id {
        msg!("Invalid GeolocationUser account owner");
        return Err(ProgramError::IllegalOwner);
    }
    if !user_account.is_writable {
        msg!("GeolocationUser account must be writable");
        return Err(ProgramError::InvalidAccountData);
    }

    let mut user = GeolocationUser::try_from(user_account)?;

    if user.owner != *payer_account.key {
        msg!("Signer is not the account owner");
        return Err(GeolocationError::Unauthorized.into());
    }

    if args.destination.is_empty() {
        user.result_destination = String::new();
    } else {
        validate_destination(&args.destination)?;
        user.result_destination = args.destination.clone();
    }

    // Collect unique probe pubkeys from the user's targets.
    let mut unique_probes: HashSet<Pubkey> = HashSet::new();
    for target in &user.targets {
        if !unique_probes.contains(&target.geoprobe_pk) {
            unique_probes.insert(target.geoprobe_pk);
        }
    }

    // Remaining accounts are the probe accounts to bump target_update_count on.
    let remaining: Vec<&AccountInfo> = accounts_iter.collect();
    if remaining.len() != unique_probes.len() {
        msg!(
            "Expected {} probe accounts, got {}",
            unique_probes.len(),
            remaining.len()
        );
        return Err(GeolocationError::TooManyReferencedProbes.into());
    }

    for probe_account in &remaining {
        if probe_account.owner != program_id {
            msg!("Invalid GeoProbe account owner");
            return Err(ProgramError::IllegalOwner);
        }
        if !probe_account.is_writable {
            msg!("GeoProbe account must be writable");
            return Err(ProgramError::InvalidAccountData);
        }
        if !unique_probes.contains(probe_account.key) {
            msg!(
                "Probe account {} not referenced by user targets",
                probe_account.key
            );
            return Err(ProgramError::InvalidAccountData);
        }
    }

    try_acc_write(&user, user_account, payer_account, accounts)?;

    for probe_account in &remaining {
        let mut probe = GeoProbe::try_from(*probe_account)?;
        probe.target_update_count = probe.target_update_count.wrapping_add(1); // Probe uses change in this value to check for updates.
        try_acc_write(&probe, probe_account, payer_account, accounts)?;
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_validate_destination_ip_port() {
        assert!(validate_destination("185.199.108.1:9000").is_ok());
        assert!(validate_destination("8.8.8.8:443").is_ok());
    }

    #[test]
    fn test_validate_destination_domain_port() {
        assert!(validate_destination("results.example.com:9000").is_ok());
        assert!(validate_destination("a.b:1").is_ok());
    }

    #[test]
    fn test_validate_destination_missing_port() {
        assert!(validate_destination("no-port").is_err());
    }

    #[test]
    fn test_validate_destination_invalid_port() {
        assert!(validate_destination("example.com:99999").is_err());
        assert!(validate_destination("example.com:abc").is_err());
    }

    #[test]
    fn test_validate_destination_private_ip() {
        assert!(validate_destination("10.0.0.1:9000").is_err());
        assert!(validate_destination("192.168.1.1:9000").is_err());
    }

    #[test]
    fn test_validate_destination_single_label_domain() {
        assert!(validate_destination("localhost:9000").is_err());
    }

    #[test]
    fn test_validate_destination_hyphen_label() {
        assert!(validate_destination("-bad.example.com:9000").is_err());
        assert!(validate_destination("bad-.example.com:9000").is_err());
    }

    #[test]
    fn test_validate_destination_underscore_label() {
        assert!(validate_destination("bad_label.example.com:9000").is_err());
    }
}
