use std::{io::Write, net::Ipv4Addr, sync::Arc};

use clap::Args;
use doublezero_cli_core::{CliContext, OutputFormat};
use doublezero_ledger_sentinel::{
    client::solana::SolRpcClient, constants::ENV_PREVIOUS_LEADER_EPOCHS,
};
use doublezero_sdk::get_doublezero_pubkey;
use doublezero_solana_client_tools::rpc::SolanaConnection;
use serde::Serialize;
use solana_client::rpc_response::RpcContactInfo;
use solana_sdk::{pubkey::Pubkey, signature::Keypair, signer::Signer};
use url::Url;

use crate::{
    error::{PassportCliError, Result},
    output::{emit_json, is_json},
    util::{find_node_by_ip, find_node_by_node_id, identify_cluster, try_get_public_ipv4},
};

#[derive(Debug, Args)]
pub struct FindValidatorArgs {
    #[arg(long, value_name = "PUBKEY")]
    pub validator_id: Option<Pubkey>,

    #[arg(long, value_name = "IP_ADDRESS")]
    pub gossip_ip: Option<String>,
}

impl FindValidatorArgs {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        let format = ctx.output_format;
        if is_json(format) {
            self.run_json(ctx, out, format).await
        } else {
            self.run_human(ctx, out).await
        }
    }

    /// Human-readable output. Reproduces the exact pre-RFC-20 behavior, including
    /// branch-specific warnings and the print-and-return handling of parse / IP
    /// detection failures.
    async fn run_human(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        tracing::debug!(env = %ctx.env, "passport find-validator");

        writeln!(out, "DoubleZero Passport - Find Validator")?;

        let connection = SolanaConnection::new(ctx.solana_l1_rpc_url.clone());
        let sol_client =
            SolRpcClient::new(Url::parse(&connection.url())?, Arc::new(Keypair::new()));

        let cluster = identify_cluster(&connection).await?;
        writeln!(out, "Connected to Solana: {cluster}\n")?;

        if let Ok(kp) = get_doublezero_pubkey() {
            writeln!(out, "DoubleZero ID: {}", kp.pubkey())?;
        }

        let nodes = connection.get_cluster_nodes().await?;
        if nodes.is_empty() {
            return Err(PassportCliError::ClusterNodesUnavailable);
        }

        if let Some(node_id) = self.validator_id {
            render_node_id_node(&nodes, &node_id, &sol_client, out).await?;
        } else if let Some(ip_str) = self.gossip_ip {
            let server_ip: Ipv4Addr = match ip_str.parse() {
                Ok(addr) => addr,
                Err(e) => {
                    writeln!(out, "Failed to parse server IP: {e}")?;
                    return Ok(());
                }
            };
            render_ip_node(&nodes, server_ip, &sol_client, out).await?;
        } else {
            match try_get_public_ipv4() {
                Ok(ip) => {
                    writeln!(out, "Detected public IP: {ip}")?;
                    let server_ip: Ipv4Addr = match ip.parse() {
                        Ok(addr) => addr,
                        Err(e) => {
                            writeln!(out, "Failed to parse detected public IP: {e}")?;
                            return Ok(());
                        }
                    };
                    render_ip_node(&nodes, server_ip, &sol_client, out).await?;
                }
                Err(e) => writeln!(out, "Failed to get public IP: {e}")?,
            }
        }

        Ok(())
    }

    /// Additive JSON output for the read verb. Collects the same lookup into a
    /// serializable view.
    async fn run_json(
        self,
        ctx: &CliContext,
        out: &mut impl Write,
        format: OutputFormat,
    ) -> Result<()> {
        tracing::debug!(env = %ctx.env, "passport find-validator (json)");

        let connection = SolanaConnection::new(ctx.solana_l1_rpc_url.clone());
        let sol_client =
            SolRpcClient::new(Url::parse(&connection.url())?, Arc::new(Keypair::new()));

        let mut view = ValidatorLookupView {
            cluster: identify_cluster(&connection).await?.to_string(),
            doublezero_id: get_doublezero_pubkey()
                .ok()
                .map(|kp| kp.pubkey().to_string()),
            ..Default::default()
        };

        let nodes = connection.get_cluster_nodes().await?;
        if nodes.is_empty() {
            return Err(PassportCliError::ClusterNodesUnavailable);
        }

        let node: Option<&RpcContactInfo> = if let Some(node_id) = self.validator_id {
            find_node_by_node_id(&nodes, &node_id)
        } else if let Some(ip_str) = self.gossip_ip {
            let server_ip: Ipv4Addr = ip_str.parse()?;
            find_node_by_ip(&nodes, server_ip)
        } else {
            let ip = try_get_public_ipv4()?;
            view.detected_public_ip = Some(ip.clone());
            let server_ip: Ipv4Addr = ip.parse()?;
            find_node_by_ip(&nodes, server_ip)
        };

        match node {
            Some(node) => {
                let in_leader_schedule = leader_status(&sol_client, node).await?;
                view.validator_id = Some(node.pubkey.clone());
                view.gossip_ip = Some(
                    node.gossip
                        .as_ref()
                        .map(|g| g.ip().to_string())
                        .unwrap_or_else(|| "<unknown>".to_string()),
                );
                view.in_leader_schedule = Some(in_leader_schedule);
                view.role = Some(
                    if in_leader_schedule {
                        "primary"
                    } else {
                        "backup"
                    }
                    .to_string(),
                );
                view.visible_in_gossip = true;
            }
            None => {
                view.visible_in_gossip = false;
                view.warning = Some(NOT_IN_GOSSIP_WARNING.to_string());
            }
        }

        emit_json(out, &view, format)
    }
}

/// Resolve whether `node` is a scheduled leader. Shared by the human and JSON
/// paths so the pubkey parse and `is_scheduled_leader` lookup live in one place.
async fn leader_status(sol_client: &SolRpcClient, node: &RpcContactInfo) -> Result<bool> {
    let pubkey = node.pubkey.parse::<Pubkey>()?;
    Ok(sol_client
        .is_scheduled_leader(&pubkey, ENV_PREVIOUS_LEADER_EPOCHS)
        .await?)
}

/// Look up a node by node ID and render it (human path).
async fn render_node_id_node<W: Write>(
    nodes: &[RpcContactInfo],
    node_id: &Pubkey,
    sol_client: &SolRpcClient,
    out: &mut W,
) -> Result<()> {
    if let Some(node) = find_node_by_node_id(nodes, node_id) {
        print_node_info(node, sol_client, out).await
    } else {
        writeln!(
            out,
            "⚠️  Warning: Your node ID is not appearing in gossip. Your validator must be visible in gossip in order to connect to DoubleZero."
        )?;
        Ok(())
    }
}

/// Look up a node by gossip IP and render it (human path). Shared by the
/// `--gossip-ip` and detected-public-IP branches.
async fn render_ip_node<W: Write>(
    nodes: &[RpcContactInfo],
    server_ip: Ipv4Addr,
    sol_client: &SolRpcClient,
    out: &mut W,
) -> Result<()> {
    if let Some(node) = find_node_by_ip(nodes, server_ip) {
        print_node_info(node, sol_client, out).await
    } else {
        writeln!(
            out,
            "⚠️  Warning: Your IP is not appearing in gossip. Your validator must be visible in gossip in order to connect to DoubleZero."
        )?;
        Ok(())
    }
}

#[derive(Debug, Default, Serialize)]
struct ValidatorLookupView {
    cluster: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    doublezero_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    detected_public_ip: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    validator_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    gossip_ip: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    in_leader_schedule: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    role: Option<String>,
    visible_in_gossip: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    warning: Option<String>,
}

const NOT_IN_GOSSIP_WARNING: &str =
    "Your validator must be visible in gossip in order to connect to DoubleZero.";

async fn print_node_info<W: Write>(
    node: &RpcContactInfo,
    sol_client: &SolRpcClient,
    out: &mut W,
) -> Result<()> {
    writeln!(out, "Validator ID: {}", node.pubkey)?;
    match &node.gossip {
        Some(gossip) => writeln!(out, "Gossip IP: {}", gossip.ip())?,
        None => writeln!(out, "Gossip IP: <unknown>")?,
    }

    if leader_status(sol_client, node).await? {
        writeln!(out, "In Leader scheduler")?;
        writeln!(
            out,
            "✅ This validator can connect as a primary in DoubleZero 🖥️  💎. It is a leader scheduled validator."
        )?;
    } else {
        writeln!(
            out,
            "✅ This validator can only connect as a backup in DoubleZero 🖥️  🛟. It is not leader scheduled and cannot act as a primary validator."
        )?;
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use std::net::Ipv4Addr;

    use super::*;

    fn dummy_client() -> SolRpcClient {
        // No network calls happen on the not-in-gossip paths (the node is never
        // found), so any well-formed URL works.
        SolRpcClient::new(
            Url::parse("http://127.0.0.1:8899").unwrap(),
            Arc::new(Keypair::new()),
        )
    }

    #[tokio::test]
    async fn node_id_not_in_gossip_emits_node_id_warning() {
        let sol_client = dummy_client();
        let mut out = Vec::new();

        render_node_id_node(&[], &Pubkey::new_unique(), &sol_client, &mut out)
            .await
            .unwrap();

        let rendered = String::from_utf8(out).unwrap();
        assert_eq!(
            rendered,
            "⚠️  Warning: Your node ID is not appearing in gossip. Your validator must be visible in gossip in order to connect to DoubleZero.\n"
        );
    }

    #[tokio::test]
    async fn ip_not_in_gossip_emits_ip_warning() {
        let sol_client = dummy_client();
        let mut out = Vec::new();

        render_ip_node(&[], Ipv4Addr::LOCALHOST, &sol_client, &mut out)
            .await
            .unwrap();

        let rendered = String::from_utf8(out).unwrap();
        assert_eq!(
            rendered,
            "⚠️  Warning: Your IP is not appearing in gossip. Your validator must be visible in gossip in order to connect to DoubleZero.\n"
        );
    }
}
