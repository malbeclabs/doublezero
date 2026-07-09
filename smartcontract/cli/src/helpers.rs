use crate::doublezerocommand::CliCommand;
use chrono::{TimeZone, Utc};
use doublezero_sdk::commands::{
    contributor::get::GetContributorCommand, exchange::get::GetExchangeCommand,
    location::get::GetLocationCommand, multicastgroup::get::GetMulticastGroupCommand,
    tenant::get::GetTenantCommand,
};
use eyre::WrapErr;
use std::{
    io::{Read, Write},
    str,
};

use indicatif::{ProgressBar, ProgressStyle};
use solana_sdk::pubkey::Pubkey;
use std::{
    net::{Ipv4Addr, TcpStream, ToSocketAddrs},
    str::FromStr,
    time::Duration,
};

pub fn slot_to_datetime<C: CliCommand>(client: &C, slot: u64) -> String {
    if slot == 0 {
        return "never".to_string();
    }
    match client.get_block_time(slot) {
        Ok(Some(ts)) => Utc
            .timestamp_opt(ts, 0)
            .single()
            .map(|dt| dt.format("%Y-%m-%d %H:%M:%S UTC").to_string())
            .unwrap_or_else(|| slot.to_string()),
        _ => slot.to_string(),
    }
}

pub fn parse_pubkey(input: &str) -> Option<Pubkey> {
    if input.len() < 43 || input.len() > 44 {
        return None;
    }

    Pubkey::from_str(input).ok()
}

/// Resolve a `--pubkey`/`--code` argument to the location's on-chain pubkey.
///
/// Update and delete verbs accept either a base58 pubkey or a location code.
/// In both cases the backend is queried for the account so the verb can
/// reuse the returned pubkey on the mutating call without having to parse
/// the input itself.
pub fn resolve_location_pk<C: CliCommand>(
    client: &C,
    pubkey_or_code: &str,
) -> eyre::Result<Pubkey> {
    let (pubkey, _) = client.get_location(GetLocationCommand {
        pubkey_or_code: pubkey_or_code.to_string(),
    })?;
    Ok(pubkey)
}

/// Resolve a `--pubkey`/`--code` argument to the exchange's on-chain pubkey.
pub fn resolve_exchange_pk<C: CliCommand>(
    client: &C,
    pubkey_or_code: &str,
) -> eyre::Result<Pubkey> {
    let (pubkey, _) = client.get_exchange(GetExchangeCommand {
        pubkey_or_code: pubkey_or_code.to_string(),
    })?;
    Ok(pubkey)
}

/// Resolve an exchange argument that is either a base58 pubkey or an exchange code.
///
/// Unlike [`resolve_exchange_pk`], a pubkey input is used as-is with no onchain
/// lookup or validation; only a code queries the backend for the account.
/// Classification uses a full base58 decode rather than [`parse_pubkey`]'s
/// 43-44 char window so pubkeys with leading zero bytes (shorter encodings)
/// still pass through.
pub fn resolve_exchange_arg<C: CliCommand>(client: &C, input: &str) -> eyre::Result<Pubkey> {
    match Pubkey::from_str(input).ok() {
        Some(pk) => Ok(pk),
        None => client
            .get_exchange(GetExchangeCommand {
                pubkey_or_code: input.to_string(),
            })
            .map(|(pubkey, _)| pubkey)
            .wrap_err_with(|| format!("Exchange not found: {input}")),
    }
}

/// Resolve a multicast group argument that is either a base58 pubkey or a group code.
///
/// A pubkey input is used as-is with no onchain lookup or validation; only a
/// code queries the backend for the account. Classification uses a full base58
/// decode rather than [`parse_pubkey`]'s 43-44 char window so pubkeys with
/// leading zero bytes (shorter encodings) still pass through.
pub fn resolve_multicastgroup_arg<C: CliCommand>(client: &C, input: &str) -> eyre::Result<Pubkey> {
    match Pubkey::from_str(input).ok() {
        Some(pk) => Ok(pk),
        None => client
            .get_multicastgroup(GetMulticastGroupCommand {
                pubkey_or_code: input.to_string(),
            })
            .map(|(pubkey, _)| pubkey)
            .wrap_err_with(|| format!("Multicast group not found: {input}")),
    }
}

/// Resolve a `--pubkey`/`--code` argument to the contributor's on-chain pubkey.
pub fn resolve_contributor_pk<C: CliCommand>(
    client: &C,
    pubkey_or_code: &str,
) -> eyre::Result<Pubkey> {
    let (pubkey, _) = client.get_contributor(GetContributorCommand {
        pubkey_or_code: pubkey_or_code.to_string(),
    })?;
    Ok(pubkey)
}

/// Resolve a `--pubkey`/`--code` argument to the tenant's on-chain pubkey.
pub fn resolve_tenant_pk<C: CliCommand>(client: &C, pubkey_or_code: &str) -> eyre::Result<Pubkey> {
    let (pubkey, _) = client.get_tenant(GetTenantCommand {
        pubkey_or_code: pubkey_or_code.to_string(),
    })?;
    Ok(pubkey)
}

pub fn print_error(e: eyre::Report) {
    tracing::error!("{e:?}");
}

pub fn get_public_ipv4() -> Result<String, Box<dyn std::error::Error>> {
    // Resolve the host `ifconfig.me` to IPv4 addresses
    let addrs = "ifconfig.me:80"
        .to_socket_addrs()?
        .filter_map(|addr| match addr {
            std::net::SocketAddr::V4(ipv4) => Some(ipv4),
            _ => None,
        })
        .next()
        .ok_or("Failed to resolve an IPv4 address")?;

    // Establish a connection to the IPv4 address
    let mut stream = TcpStream::connect(addrs)?;

    // Send an HTTP GET request to retrieve only IPv4
    let request = "GET /ip HTTP/1.1\r\nHost: ifconfig.me\r\nConnection: close\r\n\r\n";
    stream.write_all(request.as_bytes())?;

    // Read the response from the server
    let mut response = Vec::new();
    stream.read_to_end(&mut response)?;

    // Convert the response to text and find the body of the response
    let response_text = str::from_utf8(&response)?;

    // The IP will be in the body after the HTTP headers
    if let Some(body_start) = response_text.find("\r\n\r\n") {
        let ip = response_text[body_start + 4..].trim();
        ip.parse::<Ipv4Addr>()
            .map(|_| ip.to_string())
            .map_err(|_| "Failed to parse IPv4 address from response".into())
    } else {
        Err("Failed to extract the IP from the response".into())
    }
}

pub fn init_command(len: u64) -> ProgressBar {
    let spinner = ProgressBar::new(len);

    spinner.set_style(
        ProgressStyle::default_spinner()
            .template("{spinner:.green} [{elapsed_precise}] [{bar:40.cyan/blue}] {pos}/{len} {msg}")
            .expect("Failed to set template")
            .progress_chars("#>-")
            .tick_strings(&["-", "\\", "|", "/"]),
    );
    spinner.enable_steady_tick(Duration::from_millis(100));

    spinner.println("DoubleZero Network");

    spinner
}
