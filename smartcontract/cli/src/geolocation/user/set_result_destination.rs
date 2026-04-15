use crate::{geoclicommand::GeoCliCommand, validators::validate_code};
use clap::Args;
use doublezero_geolocation::validation::validate_public_ip;
use doublezero_sdk::geolocation::geolocation_user::{
    get::GetGeolocationUserCommand, set_result_destination::SetResultDestinationCommand,
};
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, net::Ipv4Addr};

#[derive(Args, Debug)]
pub struct SetResultDestinationCliCommand {
    /// User code
    #[arg(long, value_parser = validate_code)]
    pub user: String,
    /// Destination as host:port (e.g., "185.199.108.1:9000" or "results.example.com:9000")
    #[arg(long, conflicts_with = "clear")]
    pub destination: Option<String>,
    /// Clear the result destination
    #[arg(long)]
    pub clear: bool,
}

// RFC 1035 §2.3.4
const MAX_DOMAIN_LENGTH: usize = 253;
const MAX_LABEL_LENGTH: usize = 63;

fn validate_domain(host: &str) -> eyre::Result<()> {
    if host.len() > MAX_DOMAIN_LENGTH {
        return Err(eyre::eyre!(
            "domain too long ({} chars, max {MAX_DOMAIN_LENGTH})",
            host.len()
        ));
    }
    let labels: Vec<&str> = host.split('.').collect();
    if labels.len() < 2 {
        return Err(eyre::eyre!(
            "domain must have at least two labels (e.g., \"example.com\")"
        ));
    }
    for label in &labels {
        if label.is_empty() || label.len() > MAX_LABEL_LENGTH {
            return Err(eyre::eyre!("invalid domain label length: {}", label.len()));
        }
        if label.starts_with('-') || label.ends_with('-') {
            return Err(eyre::eyre!(
                "domain label \"{}\" cannot start or end with a hyphen",
                label
            ));
        }
        if !label.chars().all(|c| c.is_ascii_alphanumeric() || c == '-') {
            return Err(eyre::eyre!(
                "domain label \"{}\" contains invalid characters",
                label
            ));
        }
    }
    Ok(())
}

fn validate_destination(destination: &str) -> eyre::Result<()> {
    let colon_pos = destination.rfind(':').ok_or_else(|| {
        eyre::eyre!("invalid destination \"{destination}\": expected host:port format")
    })?;
    let host = &destination[..colon_pos];
    let port_str = &destination[colon_pos + 1..];

    port_str
        .parse::<u16>()
        .map_err(|_| eyre::eyre!("invalid port \"{port_str}\": must be a number 0-65535"))?;

    if let Ok(ip) = host.parse::<Ipv4Addr>() {
        validate_public_ip(&ip).map_err(|e| eyre::eyre!("invalid IP address {host}: {e}"))?;
        return Ok(());
    }

    validate_domain(host)?;
    Ok(())
}

impl SetResultDestinationCliCommand {
    pub fn execute<C: GeoCliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let destination = if self.clear {
            String::new()
        } else {
            let dest = self
                .destination
                .ok_or_else(|| eyre::eyre!("--destination is required (or use --clear)"))?;
            validate_destination(&dest)?;
            dest
        };

        let (_, user) = client.get_geolocation_user(GetGeolocationUserCommand {
            pubkey_or_code: self.user.clone(),
        })?;

        let mut probe_pks: Vec<Pubkey> = Vec::new();
        for target in &user.targets {
            if !probe_pks.contains(&target.geoprobe_pk) {
                probe_pks.push(target.geoprobe_pk);
            }
        }

        let sig = client.set_result_destination(SetResultDestinationCommand {
            code: self.user,
            destination,
            probe_pks,
        })?;

        writeln!(out, "Signature: {sig}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::geoclicommand::MockGeoCliCommand;
    use doublezero_geolocation::state::{
        accounttype::AccountType,
        geolocation_user::{
            FlatPerEpochConfig, GeoLocationTargetType, GeolocationBillingConfig,
            GeolocationPaymentStatus, GeolocationTarget, GeolocationUser, GeolocationUserStatus,
        },
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::net::Ipv4Addr;

    fn make_user(targets: Vec<GeolocationTarget>) -> GeolocationUser {
        GeolocationUser {
            account_type: AccountType::GeolocationUser,
            owner: Pubkey::new_unique(),
            code: "geo-user-01".to_string(),
            token_account: Pubkey::new_unique(),
            payment_status: GeolocationPaymentStatus::Paid,
            billing: GeolocationBillingConfig::FlatPerEpoch(FlatPerEpochConfig {
                rate: 1000,
                last_deduction_dz_epoch: 42,
            }),
            status: GeolocationUserStatus::Activated,
            targets,
            result_destination: String::new(),
        }
    }

    #[test]
    fn test_cli_set_result_destination() {
        let mut client = MockGeoCliCommand::new();

        let user_pk = Pubkey::from_str_const("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB");
        let probe_pk1 = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let probe_pk2 = Pubkey::from_str_const("GQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc");
        let signature = Signature::new_unique();

        let user = make_user(vec![
            GeolocationTarget {
                target_type: GeoLocationTargetType::Outbound,
                ip_address: Ipv4Addr::new(8, 8, 8, 8),
                location_offset_port: 8923,
                target_pk: Pubkey::default(),
                geoprobe_pk: probe_pk1,
            },
            GeolocationTarget {
                target_type: GeoLocationTargetType::Outbound,
                ip_address: Ipv4Addr::new(1, 1, 1, 1),
                location_offset_port: 8923,
                target_pk: Pubkey::default(),
                geoprobe_pk: probe_pk2,
            },
            GeolocationTarget {
                target_type: GeoLocationTargetType::Outbound,
                ip_address: Ipv4Addr::new(9, 9, 9, 9),
                location_offset_port: 8923,
                target_pk: Pubkey::default(),
                geoprobe_pk: probe_pk1,
            },
        ]);

        client
            .expect_get_geolocation_user()
            .with(predicate::eq(GetGeolocationUserCommand {
                pubkey_or_code: "geo-user-01".to_string(),
            }))
            .returning(move |_| Ok((user_pk, user.clone())));

        client
            .expect_set_result_destination()
            .with(predicate::eq(SetResultDestinationCommand {
                code: "geo-user-01".to_string(),
                destination: "185.199.108.1:9000".to_string(),
                probe_pks: vec![probe_pk1, probe_pk2],
            }))
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = SetResultDestinationCliCommand {
            user: "geo-user-01".to_string(),
            destination: Some("185.199.108.1:9000".to_string()),
            clear: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("Signature:"));
    }

    #[test]
    fn test_cli_set_result_destination_clear() {
        let mut client = MockGeoCliCommand::new();

        let user_pk = Pubkey::from_str_const("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB");
        let signature = Signature::new_unique();

        let user = make_user(vec![]);

        client
            .expect_get_geolocation_user()
            .with(predicate::eq(GetGeolocationUserCommand {
                pubkey_or_code: "geo-user-01".to_string(),
            }))
            .returning(move |_| Ok((user_pk, user.clone())));

        client
            .expect_set_result_destination()
            .with(predicate::eq(SetResultDestinationCommand {
                code: "geo-user-01".to_string(),
                destination: String::new(),
                probe_pks: vec![],
            }))
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = SetResultDestinationCliCommand {
            user: "geo-user-01".to_string(),
            destination: None,
            clear: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("Signature:"));
    }

    #[test]
    fn test_cli_set_result_destination_missing_destination() {
        let client = MockGeoCliCommand::new();

        let mut output = Vec::new();
        let res = SetResultDestinationCliCommand {
            user: "geo-user-01".to_string(),
            destination: None,
            clear: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_err());
        assert!(res.unwrap_err().to_string().contains("--destination"));
    }

    #[test]
    fn test_cli_set_result_destination_invalid_destinations() {
        let cases = vec![
            ("no-port", "expected host:port"),
            ("10.0.0.1:9000", "invalid IP"),
            ("192.168.1.1:9000", "invalid IP"),
            ("example.com:99999", "invalid port"),
            ("example.com:abc", "invalid port"),
            ("bad..domain:80", "invalid domain label length"),
            ("-bad.example.com:80", "cannot start or end with a hyphen"),
            ("localhost:9000", "at least two labels"),
            ("bad_label.example.com:80", "invalid characters"),
        ];
        for (dest, expected_msg) in cases {
            let client = MockGeoCliCommand::new();
            let mut output = Vec::new();
            let res = SetResultDestinationCliCommand {
                user: "geo-user-01".to_string(),
                destination: Some(dest.to_string()),
                clear: false,
            }
            .execute(&client, &mut output);
            assert!(res.is_err(), "expected error for destination \"{dest}\"");
            let err = res.unwrap_err().to_string();
            assert!(
                err.contains(expected_msg),
                "destination \"{dest}\": expected error containing \"{expected_msg}\", got \"{err}\""
            );
        }
    }
}
