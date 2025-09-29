use std::{net::Ipv4Addr, sync::Arc};

use anyhow::Result;
use clap::Args;
use doublezero_ledger_sentinel::{
    client::solana::SolRpcClient, constants::ENV_PREVIOUS_LEADER_EPOCHS,
};
use doublezero_sdk::get_doublezero_pubkey;
use doublezero_solana_client_tools::rpc::{SolanaConnection, SolanaConnectionOptions};
use solana_client::rpc_response::RpcContactInfo;
use solana_sdk::{pubkey::Pubkey, signature::Keypair, signer::Signer};
use url::Url;

use crate::helpers::{find_node_by_ip, find_node_by_node_id, get_public_ipv4, identify_cluster};

#[derive(Debug, Args)]
pub struct FindValidatorCommand {
    #[arg(long, value_name = "PUBKEY")]
    validator_id: Option<Pubkey>,

    #[arg(long, value_name = "IP_ADDRESS")]
    gossip_ip: Option<String>,

    #[command(flatten)]
    solana_connection_options: SolanaConnectionOptions,
}

impl FindValidatorCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let FindValidatorCommand {
            validator_id,
            gossip_ip,
            solana_connection_options,
        } = self;

        println!("DoubleZero Passport - Find Validator");

        // Establish a connection to the Solana cluster
        let connection = SolanaConnection::try_from(solana_connection_options)?;
        let sol_client = SolRpcClient::new(
            Url::parse(&connection.rpc_client.url()).expect("Invalid RPC URL"),
            Arc::new(Keypair::new()),
        );

        // Identify the cluster
        let cluster = identify_cluster(&connection).await;
        println!("Connected to Solana: {:}\n", cluster);

        if let Ok(kp) = get_doublezero_pubkey() {
            println!("DoubleZero ID: {}", kp.pubkey())
        }

        // Fetch the cluster nodes
        let nodes = connection.get_cluster_nodes().await?;
        if nodes.is_empty() {
            anyhow::bail!("Unable to fetch cluster nodes. Is your RPC endpoint correct?");
        }

        // Check if either node_id or server_ip is provided
        if let Some(node_id) = validator_id {
            // Search by node_id
            if let Some(node) = find_node_by_node_id(&nodes, &node_id) {
                print_node_info(node, &sol_client).await;
            } else {
                println!(
                    "⚠️  Warning: Your node ID is not appearing in gossip. Your validator must be visible in gossip in order to connect to DoubleZero."
                );
            }
        } else if let Some(ip_str) = gossip_ip {
            // Search by server_ip
            let server_ip: Ipv4Addr = match ip_str.parse() {
                Ok(addr) => addr,
                Err(e) => {
                    println!("Failed to parse server IP: {e}");
                    return Ok(());
                }
            };
            if let Some(node) = find_node_by_ip(&nodes, server_ip) {
                print_node_info(node, &sol_client).await;
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
                        print_node_info(node, &sol_client).await;
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

async fn print_node_info(node: &RpcContactInfo, sol_client: &SolRpcClient) {
    println!("Validator ID: {}", node.pubkey);
    match &node.gossip {
        Some(gossip) => println!("Gossip IP: {}", gossip.ip()),
        None => println!("Gossip IP: <unknown>"),
    }

    let pubkey = node.pubkey.parse::<Pubkey>().expect("Invalid pubkey");

    if sol_client
        .check_leader_schedule(&pubkey, ENV_PREVIOUS_LEADER_EPOCHS)
        .await
        .is_ok()
    {
        println!("In Leader scheduler");
        println!(
            "✅ This validator can connect as a primary in DoubleZero 🖥️  💎. It is a leader scheduled validator."
        );
    } else {
        println!(
            "✅ This validator can only connect as a backup in DoubleZero 🖥️  🛟. It is not leader scheduled and cannot act as a primary validator."
        );
    }
}
