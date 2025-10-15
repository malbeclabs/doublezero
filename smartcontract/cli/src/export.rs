use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_program_common::types::parse_utils::bandwidth_to_string;
use doublezero_sdk::commands::{
    contributor, device::list::ListDeviceCommand, exchange::list::ListExchangeCommand,
    link::list::ListLinkCommand, location::list::ListLocationCommand, user::list::ListUserCommand,
};
use serde::{Deserialize, Serialize};
use std::{fs, io::Write};

#[derive(Args, Debug)]
pub struct ExportCliCommand {
    /// Path to export the YAML files
    #[arg(long)]
    pub path: String,
}

#[derive(Serialize, Deserialize, Debug)]
struct Data {
    device: DeviceData,
}

#[derive(Serialize, Deserialize, Debug)]
struct DeviceData {
    name: String,
    pubkey: String,
    contributor: String,
    public_ip: String,
    location: LocationData,
    exchange: ExchangeData,
    tunnels: Vec<LinkData>,
    users: Vec<UserData>,
    owner: String,
}

#[derive(Serialize, Deserialize, Debug)]
struct LocationData {
    code: String,
    name: String,
    pubkey: String,
    country: String,
    lat: f64,
    lng: f64,
    owner: String,
}

#[derive(Serialize, Deserialize, Debug)]
struct ExchangeData {
    code: String,
    name: String,
    pubkey: String,
    lat: f64,
    lng: f64,
    owner: String,
}

#[derive(Serialize, Deserialize, Debug)]
struct LinkData {
    pub pubkey: String,
    pub code: String,
    pub iface_name: String,
    pub side: LinkSideData,
    pub tunnel_net: String,
    pub link_type: String,
    pub bandwidth: String,
    pub mtu: u32,
    pub delay_ms: f32,
    pub jitter_ms: f32,
    pub owner: String,
}

#[derive(Serialize, Deserialize, Debug)]
struct LinkSideData {
    pub name: String,
    pub pubkey: String,
    pub tunnel_id: u16,
    pub tunnel_net: String,
    pub public_ip: String,
    pub iface_name: String,
}

#[derive(Serialize, Deserialize, Debug)]
struct UserData {
    pub pubkey: String,
    pub user_type: String,
    pub cyoa_type: String,
    pub client_ip: String,
    pub tunnel_id: u16,
    pub tunnel_net: String,
    pub dz_ip: String,
    pub status: String,
    pub owner: String,
    pub validator_pubkey: String,
}

impl ExportCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let locations = client.list_location(ListLocationCommand)?;
        let exchanges = client.list_exchange(ListExchangeCommand)?;

        let contributors = client.list_contributor(contributor::list::ListContributorCommand {})?;
        let devices = client.list_device(ListDeviceCommand)?;
        let tunnels = client.list_link(ListLinkCommand)?;
        let users = client.list_user(ListUserCommand)?;

        for (pubkey, data) in devices.clone() {
            let name = format!("{}/{}.yml", self.path, data.code);

            let location = locations
                .get(&data.location_pk)
                .ok_or(eyre::eyre!("Unable to retrieve Location"))?;
            let exchange = exchanges
                .get(&data.exchange_pk)
                .ok_or(eyre::eyre!("Unable to retrieve Exchange"))?;

            let contributor = contributors
                .get(&data.contributor_pk)
                .ok_or(eyre::eyre!("Unable to retrieve Contributor"))?;

            writeln!(out, "{name}")?;

            let config = Data {
                device: DeviceData {
                    name: data.code,
                    pubkey: pubkey.to_string(),
                    contributor: contributor.code.clone(),
                    location: LocationData {
                        code: location.code.clone(),
                        name: location.name.clone(),
                        country: location.country.clone(),
                        pubkey: data.location_pk.to_string(),
                        lat: location.lat,
                        lng: location.lng,
                        owner: location.owner.to_string(),
                    },
                    exchange: ExchangeData {
                        code: exchange.code.clone(),
                        name: exchange.name.clone(),
                        pubkey: data.exchange_pk.to_string(),
                        lat: exchange.lat,
                        lng: exchange.lng,
                        owner: exchange.owner.to_string(),
                    },
                    public_ip: data.public_ip.to_string(),
                    tunnels: tunnels
                        .clone()
                        .into_iter()
                        .filter(|(_, tunnel)| {
                            tunnel.side_a_pk == pubkey || tunnel.side_z_pk == pubkey
                        })
                        .filter_map(|(key, link)| {
                            let iface_name = if link.side_a_pk == pubkey {
                                link.side_a_iface_name.clone()
                            } else {
                                link.side_z_iface_name.clone()
                            };

                            let side_pubkey = if link.side_a_pk == pubkey {
                                link.side_z_pk
                            } else {
                                link.side_a_pk
                            };

                            let iface_name2 = if link.side_a_pk == pubkey {
                                link.side_z_iface_name.clone()
                            } else {
                                link.side_a_iface_name.clone()
                            };

                            let side_device = devices.get(&side_pubkey)?;

                            Some(LinkData {
                                pubkey: key.to_string(),
                                code: link.code.clone(),
                                tunnel_net: link.tunnel_net.to_string(),
                                iface_name,
                                side: LinkSideData {
                                    name: side_device.code.clone(),
                                    pubkey: side_pubkey.to_string(),
                                    iface_name: iface_name2,
                                    public_ip: side_device.public_ip.to_string(),
                                    tunnel_id: link.tunnel_id,
                                    tunnel_net: link.tunnel_net.to_string(),
                                },
                                link_type: link.link_type.to_string(),
                                bandwidth: bandwidth_to_string(&link.bandwidth),
                                mtu: link.mtu,
                                delay_ms: link.delay_ns as f32 / 1000000.0,
                                jitter_ms: link.jitter_ns as f32 / 1000000.0,
                                owner: link.owner.to_string(),
                            })
                        })
                        .collect(),
                    users: users
                        .iter()
                        .filter(|(_, user)| user.device_pk == pubkey)
                        .map(|(key, user)| UserData {
                            pubkey: key.to_string(),
                            user_type: user.user_type.to_string(),
                            client_ip: user.client_ip.to_string(),
                            cyoa_type: user.cyoa_type.to_string(),
                            tunnel_id: user.tunnel_id,
                            tunnel_net: user.tunnel_net.to_string(),
                            dz_ip: user.dz_ip.to_string(),
                            status: user.status.to_string(),
                            owner: user.owner.to_string(),
                            validator_pubkey: user.validator_pubkey.to_string(),
                        })
                        .collect(),
                    owner: data.owner.to_string(),
                },
            };

            let content = serde_yaml::to_string(&config)?;
            fs::write(name, content)?;
        }

        Ok(())
    }
}
