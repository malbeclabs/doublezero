use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_program_common::{
    serializer,
    types::{parse_utils::bandwidth_to_string, NetworkV4},
};
use doublezero_sdk::{
    commands::{
        contributor::list::ListContributorCommand,
        device::list::ListDeviceCommand,
        link::{get::GetLinkCommand, list::ListLinkCommand},
    },
    Link, LinkLinkType, LinkStatus,
};
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use tabled::{settings::Style, Table, Tabled};

#[derive(Args, Debug)]
pub struct LatencyCliCommand {
    /// List only WAN links.
    #[arg(long)]
    pub code: String,
    /// Output as pretty JSON.
    #[arg(long, default_value_t = false)]
    pub json: bool,
    /// Output as compact JSON.
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
}

#[derive(Tabled, Serialize)]
pub struct LinkDisplay {
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub code: String,
    #[tabled(rename = "contributor")]
    pub contributor_code: String,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    #[tabled(rename = "side_a")]
    #[tabled(skip)]
    pub side_a_pk: Pubkey,
    pub side_a_name: String,
    pub side_a_iface_name: String,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    #[tabled(rename = "side_z")]
    #[tabled(skip)]
    pub side_z_pk: Pubkey,
    pub side_z_name: String,
    pub side_z_iface_name: String,
    pub link_type: LinkLinkType,
    pub bandwidth: String,
    pub mtu: u32,
    #[tabled(display = "crate::util::display_as_ms", rename = "delay_ms")]
    pub delay_ns: u64,
    #[tabled(display = "crate::util::display_as_ms", rename = "jitter_ms")]
    pub jitter_ns: u64,
    pub tunnel_id: u16,
    pub tunnel_net: NetworkV4,
    pub status: LinkStatus,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey,
}

impl LatencyCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (_pk, _link) = client.get_link(GetLinkCommand {
            pubkey_or_code: self.code.clone(),
        })?;


        


        Ok(())
    }
}
