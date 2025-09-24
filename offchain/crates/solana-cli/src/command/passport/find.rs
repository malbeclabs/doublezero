use std::net::Ipv4Addr;

use anyhow::Result;
use clap::Args;
use doublezero_solana_client_tools::rpc::{SolanaConnection, SolanaConnectionOptions};
use solana_client::rpc_response::RpcContactInfo;
use solana_sdk::pubkey::Pubkey;

use crate::helpers::get_public_ipv4;

#[derive(Debug, Args)]
pub struct FindCommand {
    #[arg(long, value_name = "PUBKEY")]
    node_id: Option<Pubkey>,

    #[arg(long, value_name = "IP_ADDRESS")]
    server_ip: Option<String>,

    #[command(flatten)]
    solana_connection_options: SolanaConnectionOptions,
}

impl FindCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let FindCommand {
            node_id,
            server_ip,
            solana_connection_options,
        } = self;

        println!("DoubleZero Passport - Find");

        // Establish a connection to the Solana cluster
        let connection = SolanaConnection::try_from(solana_connection_options)?;

        // Fetch the cluster nodes
        let nodes = connection.get_cluster_nodes().await?;

        // Check if either node_id or server_ip is provided
        if let Some(node_id) = node_id {
            // Search by node_id
            if let Some(node) = find_node_by_node_id(&nodes, &node_id) {
                print_node_info(node);
            } else {
                println!(
                    "⚠️  Warning: Your node ID is not appearing in gossip. Your validator must be visible in gossip in order to connect to DoubleZero."
                );
            }
        } else if let Some(ip_str) = server_ip {
            // Search by server_ip
            let server_ip: Ipv4Addr = match ip_str.parse() {
                Ok(addr) => addr,
                Err(e) => {
                    println!("Failed to parse server IP: {e}");
                    return Ok(());
                }
            };
            if let Some(node) = find_node_by_ip(&nodes, server_ip) {
                print_node_info(node);
            } else {
                println!(
                    "⚠️  Warning: Your IP is not appearing in gossip. Your validator must be visible in gossip in order to connect to DoubleZero."
                );
            }
        } else {
            // Neither node_id nor server_ip provided, attempt to detect public IP
            match get_public_ipv4() {
                Ok(ip) => {
                    println!("Detected public IP: {ip}");
                    let server_ip: Ipv4Addr = match ip.parse() {
                        Ok(addr) => addr,
                        Err(e) => {
                            println!("Failed to parse detected public IP: {e}");
                            return Ok(());
                        }
                    };
                    if let Some(node) = find_node_by_ip(&nodes, server_ip) {
                        print_node_info(node);
                    } else {
                        println!(
                            "⚠️  Warning: Your IP is not appearing in gossip. Your validator must be visible in gossip in order to connect to DoubleZero."
                        );
                    }
                }
                Err(e) => println!("Failed to get public IP: {e}"),
            }
        }

        Ok(())
    }
}

fn find_node_by_node_id<'a>(
    nodes: &'a [RpcContactInfo],
    node_id: &Pubkey,
) -> Option<&'a RpcContactInfo> {
    let node_id_str = node_id.to_string();
    nodes.iter().find(|n| n.pubkey == node_id_str)
}

fn find_node_by_ip(nodes: &[RpcContactInfo], ip: Ipv4Addr) -> Option<&RpcContactInfo> {
    nodes
        .iter()
        .find(|n| n.gossip.as_ref().is_some_and(|gossip| gossip.ip() == ip))
}

fn print_node_info(node: &RpcContactInfo) {
    println!("Node-Id: {}", node.pubkey);
    match &node.gossip {
        Some(gossip) => println!("Server IP: {}", gossip.ip()),
        None => println!("Server IP: <unknown>"),
    }
}
