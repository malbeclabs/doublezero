use std::{
    fmt,
    io::{Read, Write},
    net::{Ipv4Addr, SocketAddr, TcpStream, ToSocketAddrs},
    time::Duration,
};

use anyhow::{Context, Result, bail};
use solana_client::{nonblocking::rpc_client::RpcClient, rpc_response::RpcContactInfo};
use solana_sdk::pubkey::Pubkey;

pub fn try_get_public_ipv4() -> Result<String> {
    // Resolve the host `ifconfig.me` to IPv4 addresses
    let socket_addr = "ifconfig.me:80"
        .to_socket_addrs()?
        .find(|addr| matches!(addr, SocketAddr::V4(_)))
        .context("Failed to resolve an IPv4 address")?;

    // Establish a connection to the IPv4 address with a short timeout to avoid hanging CLI calls.
    let mut stream = TcpStream::connect_timeout(&socket_addr, Duration::from_secs(5))?;
    stream.set_read_timeout(Some(Duration::from_secs(5)))?;

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
        let ip = &response_text[body_start + 4..].trim();
        return Ok(ip.to_string());
    }

    bail!("Failed to extract the IP from the response")
}

#[derive(Debug, PartialEq)]
pub enum Cluster {
    MainnetBeta,
    Testnet,
    Devnet,
    Unknown,
}

impl fmt::Display for Cluster {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Cluster::MainnetBeta => write!(f, "mainnet-beta"),
            Cluster::Testnet => write!(f, "testnet"),
            Cluster::Devnet => write!(f, "devnet"),
            Cluster::Unknown => write!(f, "unknown"),
        }
    }
}

pub async fn identify_cluster(client: &RpcClient) -> Cluster {
    let genesis_hash = client
        .get_genesis_hash()
        .await
        .expect("Failed to fetch genesis hash");

    match genesis_hash.to_string().as_str() {
        "5eykt4UsFv8P8NJdTREpY1vzqKqZKvdpKuc147dw2N9d" => Cluster::MainnetBeta,
        "4uhcVJyU9pJkvQyS88uRDiswHXSCkY3zQawwpjk2NsNY" => Cluster::Testnet,
        "EtWTRABZaYq6iMfeYKouRu166VU2xqa1wcaWoxPkrZBG" => Cluster::Devnet,
        _ => Cluster::Unknown,
    }
}

pub fn find_node_by_node_id<'a>(
    nodes: &'a [RpcContactInfo],
    node_id: &Pubkey,
) -> Option<&'a RpcContactInfo> {
    // Convert the Pubkey to string for comparison
    let node_id_str = node_id.to_string();
    // Search for the node in the list
    nodes.iter().find(|n| n.pubkey == node_id_str)
}

pub fn find_node_by_ip(nodes: &[RpcContactInfo], ip: Ipv4Addr) -> Option<&RpcContactInfo> {
    nodes
        .iter()
        .find(|n| n.gossip.as_ref().is_some_and(|gossip| gossip.ip() == ip))
}

pub fn parse_sol_amount_to_lamports(sol_amount_str: String) -> Result<u64> {
    let sol_amount_str = sol_amount_str.trim();

    if sol_amount_str.is_empty() {
        bail!("SOL amount cannot be empty");
    }

    let sol_amount = sol_amount_str
        .parse::<f64>()
        .map_err(|_| anyhow::anyhow!("Invalid SOL amount: '{sol_amount_str}'"))?;

    if sol_amount <= 0.0 {
        bail!("SOL amount must be a positive value");
    }

    if sol_amount > (u64::MAX as f64 / 1e9) {
        bail!("SOL amount too large");
    }

    // Check that value is at most 9 decimal places.
    if let Some(decimal_index) = sol_amount_str.find('.') {
        let decimal_places = sol_amount_str.len() - decimal_index - 1;
        if decimal_places > 9 {
            bail!("SOL amount cannot have more than 9 decimal places");
        }
    }

    Ok((sol_amount * 1e9).round() as u64)
}
