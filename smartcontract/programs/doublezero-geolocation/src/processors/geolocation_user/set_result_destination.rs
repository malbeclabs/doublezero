use crate::{
    error::GeolocationError,
    serializer::try_acc_write,
    state::{geo_probe::GeoProbe, geolocation_user_view::GeolocationUserView},
    validation::validate_public_ip,
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::AccountInfo, entrypoint::ProgramResult, msg, program_error::ProgramError,
    pubkey::Pubkey,
};
use std::net::Ipv4Addr;

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
    if accounts.len() < 3 {
        msg!("Not enough accounts");
        return Err(ProgramError::NotEnoughAccountKeys);
    }
    // Account layout: [user, probe_0..probe_N, payer, system_program]
    // Payer and system_program are always the last two accounts (appended by
    // execute_transaction in the SDK), with variable-length probe accounts
    // between the user and the payer.
    let user_account = &accounts[0];
    let payer_account = &accounts[accounts.len() - 2];
    let probe_accounts = &accounts[1..accounts.len() - 2];

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

    let mut view = GeolocationUserView::try_from_account(user_account)?;

    if view.owner != *payer_account.key {
        msg!("Signer is not the account owner");
        return Err(GeolocationError::Unauthorized.into());
    }

    let new_destination = if args.destination.is_empty() {
        String::new()
    } else {
        validate_destination(&args.destination)?;
        args.destination.clone()
    };

    // Phase 0: pre-flight each provided probe account (owner / writable /
    // dedup), and assemble a small sorted Vec<Pubkey> of provided keys that
    // we can binary-search per target during the cursor scan. Bounded by
    // the tx accounts list size (~30), so heap usage stays small.
    let mut provided: Vec<Pubkey> = Vec::with_capacity(probe_accounts.len());
    for probe_account in probe_accounts {
        if probe_account.owner != program_id {
            msg!("Invalid GeoProbe account owner");
            return Err(ProgramError::IllegalOwner);
        }
        if !probe_account.is_writable {
            msg!("GeoProbe account must be writable");
            return Err(ProgramError::InvalidAccountData);
        }
        let key = *probe_account.key;
        match provided.binary_search(&key) {
            Ok(_) => {
                msg!("Duplicate probe account: {}", key);
                return Err(ProgramError::InvalidAccountData);
            }
            Err(insert_at) => provided.insert(insert_at, key),
        }
    }

    // Phase 1: build a sorted set of unique probes referenced by the
    // targets, via a cursor scan. We only need to know whether
    // |unique-from-targets| equals |provided|, so we cap the set at
    // |provided| + 1: anything beyond that is already a count mismatch.
    // Heap bound: (provided.len() + 1) * 32 bytes (≤ ~1 KB at realistic
    // tx-size limits).
    let unique_cap = provided.len().saturating_add(1);
    let mut unique_from_targets: Vec<Pubkey> = Vec::with_capacity(unique_cap);
    {
        let data = user_account.try_borrow_data()?;
        let cursor = view.cursor(&data)?;
        for entry in cursor.iter() {
            let target = entry?;
            if let Err(insert_at) = unique_from_targets.binary_search(&target.geoprobe_pk) {
                if unique_from_targets.len() >= unique_cap {
                    msg!(
                        "Targets reference more than {} unique probes",
                        provided.len()
                    );
                    return Err(GeolocationError::ProbeAccountCountMismatch.into());
                }
                unique_from_targets.insert(insert_at, target.geoprobe_pk);
            }
        }
    }

    // Phase 2: cardinality check.
    if provided.len() != unique_from_targets.len() {
        msg!(
            "Expected {} probe accounts, got {}",
            unique_from_targets.len(),
            provided.len()
        );
        return Err(GeolocationError::ProbeAccountCountMismatch.into());
    }

    // Phase 3: membership — every provided probe must appear among the
    // unique-from-targets set. (Cardinalities are equal, so this also
    // implies the reverse direction.)
    for p in &provided {
        if unique_from_targets.binary_search(p).is_err() {
            msg!("Probe account {} not referenced by user targets", p);
            return Err(ProgramError::InvalidAccountData);
        }
    }

    view.write_result_destination(user_account, payer_account, accounts, new_destination)?;

    for probe_account in probe_accounts {
        let mut probe = GeoProbe::try_from(probe_account)?;
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
