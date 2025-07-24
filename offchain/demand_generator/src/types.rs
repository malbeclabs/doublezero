use crate::constants::LAMPORTS_PER_SOL;
use serde::{Deserialize, Serialize};
use solana_sdk::pubkey::Pubkey;
use std::{collections::HashMap, net::IpAddr};

pub type ValidatorIpMap = HashMap<Pubkey, IpAddr>;

#[derive(Debug, Deserialize)]
pub struct IpInfoResp {
    pub ip: String,
    pub city: String,
    pub region: String,
    pub country: String,
    pub loc: String,
    pub org: String,
    pub postal: Option<String>,
    pub timezone: String,
}

#[derive(Debug)]
pub struct ValidatorDetail {
    pub identity_pubkey: Pubkey,
    pub ip_address: IpAddr,
    pub stake_lamports: u64,
}

#[derive(Debug, Serialize)]
pub struct EnrichedValidator {
    pub pubkey: String,
    pub country: String,
    pub city: String,
    pub ip_address: String,
    pub region: String,
    pub latitude: Option<f64>,
    pub longitude: Option<f64>,
    pub asn: Option<String>,
    pub org: Option<String>,
    pub stake_sol: f64,
}

impl EnrichedValidator {
    pub fn new(val_detail: &ValidatorDetail, ip_info_resp: &IpInfoResp) -> Self {
        let (lat, lon) = loc_to_lat_lon(&ip_info_resp.loc);
        let (asn, org) = org_to_asn_org(&ip_info_resp.org);
        let stake_sol = val_detail.stake_lamports as f64 / LAMPORTS_PER_SOL as f64;

        Self {
            pubkey: val_detail.identity_pubkey.to_string(),
            country: ip_info_resp.country.to_string(),
            city: ip_info_resp.city.to_string(),
            ip_address: val_detail.ip_address.to_string(),
            region: ip_info_resp.region.to_string(),
            latitude: lat,
            longitude: lon,
            asn,
            org,
            stake_sol,
        }
    }
}

fn loc_to_lat_lon(loc: &str) -> (Option<f64>, Option<f64>) {
    match loc.split_once(',') {
        None => (None, None),
        Some((lat_str, lon_str)) => match (lat_str.parse::<f64>(), lon_str.parse::<f64>()) {
            (Ok(lat), Ok(lon)) => (Some(lat), Some(lon)),
            _ => (None, None),
        },
    }
}

fn org_to_asn_org(org: &str) -> (Option<String>, Option<String>) {
    match org.split_once(char::is_whitespace) {
        None => (None, None),
        Some((asn, org)) => {
            let org = org.trim_start();
            (Some(asn.to_string()), Some(org.to_string()))
        }
    }
}
