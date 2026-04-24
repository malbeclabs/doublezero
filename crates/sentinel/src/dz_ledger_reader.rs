use std::{collections::HashMap, net::Ipv4Addr};

use anyhow::{bail, Context, Result};
use doublezero_sdk::{
    AccountData, AccountType, DeviceStatus, LocationStatus, MulticastGroupStatus, UserStatus,
    UserType,
};
use doublezero_telemetry::state::device_latency_samples::DeviceLatencySamples;
use solana_account_decoder::UiAccountEncoding;
use solana_client::{
    nonblocking::rpc_client::RpcClient as NonblockingRpcClient,
    rpc_client::RpcClient,
    rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig},
    rpc_filter::{Memcmp, RpcFilterType},
};
use solana_sdk::pubkey::Pubkey;

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

/// DZ Ledger user info.
#[derive(Debug, Clone)]
pub struct DzUser {
    pub owner: Pubkey,
    pub client_ip: Ipv4Addr,
    pub device_pk: Pubkey,
    pub tenant_pk: Pubkey,
    pub user_type: UserType,
    pub publishers: Vec<Pubkey>,
    pub tunnel_endpoint: Ipv4Addr,
}

/// Maps device pubkey → device code.
pub struct DzLedgerCodes {
    pub device_codes: HashMap<Pubkey, String>,
}

/// Device info used for nearest-device calculations.
#[derive(Debug, Clone)]
pub struct DzDeviceInfo {
    pub pk: Pubkey,
    pub code: String,
    pub lat: f64,
    pub lng: f64,
    pub users_count: u16,
    pub max_users: u16,
    pub reserved_seats: u16,
    pub multicast_publishers_count: u16,
    pub max_multicast_publishers: u16,
    pub public_ip: Ipv4Addr,
    /// IPs from device interfaces where `user_tunnel_endpoint == true`.
    pub user_tunnel_endpoints: Vec<Ipv4Addr>,
}

// ---------------------------------------------------------------------------
// Trait
// ---------------------------------------------------------------------------

/// Client for querying DZ Ledger onchain state.
#[cfg_attr(test, mockall::automock)]
#[async_trait::async_trait]
pub trait DzLedgerReader: Send + Sync {
    /// Fetch all activated users from the DZ Ledger.
    async fn fetch_all_dz_users(&self) -> Result<Vec<DzUser>>;

    /// Resolve a multicast group code to its onchain pubkey.
    async fn resolve_multicast_group_code(&self, code: &str) -> Result<Option<Pubkey>>;
}

// ---------------------------------------------------------------------------
// RPC implementation
// ---------------------------------------------------------------------------

pub struct RpcDzLedgerReader {
    client: NonblockingRpcClient,
    program_id: Pubkey,
}

impl RpcDzLedgerReader {
    pub fn new(client: NonblockingRpcClient, program_id: Pubkey) -> Self {
        Self { client, program_id }
    }
}

#[async_trait::async_trait]
impl DzLedgerReader for RpcDzLedgerReader {
    async fn fetch_all_dz_users(&self) -> Result<Vec<DzUser>> {
        let user_type_byte = AccountType::User as u8;
        let accounts = self
            .client
            .get_program_accounts_with_config(
                &self.program_id,
                RpcProgramAccountsConfig {
                    filters: Some(vec![RpcFilterType::Memcmp(Memcmp::new_raw_bytes(
                        0,
                        vec![user_type_byte],
                    ))]),
                    account_config: RpcAccountInfoConfig {
                        encoding: Some(UiAccountEncoding::Base64),
                        ..Default::default()
                    },
                    ..Default::default()
                },
            )
            .await
            .context("failed to fetch User accounts from DZ Ledger")?;

        let mut users = Vec::new();
        for (_pk, account) in accounts {
            let Ok(ad) = AccountData::try_from(account.data.as_slice()) else {
                continue;
            };
            let Ok(user) = ad.get_user() else {
                continue;
            };
            if user.status != UserStatus::Activated {
                continue;
            }
            users.push(DzUser {
                owner: user.owner,
                client_ip: user.client_ip,
                device_pk: user.device_pk,
                tenant_pk: user.tenant_pk,
                user_type: user.user_type,
                publishers: user.publishers.clone(),
                tunnel_endpoint: user.tunnel_endpoint,
            });
        }

        Ok(users)
    }

    async fn resolve_multicast_group_code(&self, code: &str) -> Result<Option<Pubkey>> {
        let mgroup_type_byte = AccountType::MulticastGroup as u8;
        let accounts = self
            .client
            .get_program_accounts_with_config(
                &self.program_id,
                RpcProgramAccountsConfig {
                    filters: Some(vec![RpcFilterType::Memcmp(Memcmp::new_raw_bytes(
                        0,
                        vec![mgroup_type_byte],
                    ))]),
                    account_config: RpcAccountInfoConfig {
                        encoding: Some(UiAccountEncoding::Base64),
                        ..Default::default()
                    },
                    ..Default::default()
                },
            )
            .await
            .context("failed to fetch MulticastGroup accounts from DZ Ledger")?;

        for (pk, account) in accounts {
            let Ok(ad) = AccountData::try_from(account.data.as_slice()) else {
                continue;
            };
            let Ok(group) = ad.get_multicastgroup() else {
                continue;
            };
            if group.code == code && group.status == MulticastGroupStatus::Activated {
                return Ok(Some(pk));
            }
        }

        Ok(None)
    }
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/// Fetch device codes from the serviceability program.
pub fn fetch_device_codes(client: &RpcClient, program_id: &Pubkey) -> Result<DzLedgerCodes> {
    let device_accounts = client
        .get_program_accounts_with_config(
            program_id,
            RpcProgramAccountsConfig {
                filters: Some(vec![RpcFilterType::Memcmp(Memcmp::new_raw_bytes(
                    0,
                    vec![AccountType::Device as u8],
                ))]),
                account_config: RpcAccountInfoConfig {
                    encoding: Some(UiAccountEncoding::Base64),
                    ..Default::default()
                },
                ..Default::default()
            },
        )
        .context("failed to fetch Device accounts from DZ Ledger")?;

    let mut device_codes = HashMap::new();
    for (pk, account) in device_accounts {
        let Ok(ad) = AccountData::try_from(account.data.as_slice()) else {
            continue;
        };
        let Ok(device) = ad.get_device() else {
            continue;
        };
        if device.status == DeviceStatus::Activated {
            device_codes.insert(pk, device.code.clone());
        }
    }

    Ok(DzLedgerCodes { device_codes })
}

/// Fetch activated devices with their location coordinates.
///
/// Makes two RPC calls: one for all Device accounts, one for all Location accounts.
pub fn fetch_device_infos(
    client: &RpcClient,
    program_id: &Pubkey,
) -> Result<HashMap<Pubkey, DzDeviceInfo>> {
    // Fetch all Device accounts.
    let device_accounts = client
        .get_program_accounts_with_config(
            program_id,
            RpcProgramAccountsConfig {
                filters: Some(vec![RpcFilterType::Memcmp(Memcmp::new_raw_bytes(
                    0,
                    vec![AccountType::Device as u8],
                ))]),
                account_config: RpcAccountInfoConfig {
                    encoding: Some(UiAccountEncoding::Base64),
                    ..Default::default()
                },
                ..Default::default()
            },
        )
        .context("failed to fetch Device accounts from DZ Ledger")?;

    // Collect activated devices and their location pubkeys.
    let mut devices: Vec<(Pubkey, doublezero_sdk::Device)> = Vec::new();
    let mut location_pks: Vec<Pubkey> = Vec::new();
    for (pk, account) in &device_accounts {
        let Ok(ad) = AccountData::try_from(account.data.as_slice()) else {
            continue;
        };
        let Ok(device) = ad.get_device() else {
            continue;
        };
        if device.status == DeviceStatus::Activated {
            location_pks.push(device.location_pk);
            devices.push((*pk, device));
        }
    }

    if devices.is_empty() {
        return Ok(HashMap::new());
    }

    // Fetch all Location accounts in one batch call.
    let location_accounts = client
        .get_multiple_accounts(&location_pks)
        .context("failed to fetch Location accounts from DZ Ledger")?;

    // Build location_pk → (lat, lng) map.
    let mut location_coords: HashMap<Pubkey, (f64, f64)> = HashMap::new();
    for (pk, maybe_account) in location_pks.iter().zip(location_accounts.iter()) {
        let Some(account) = maybe_account else {
            continue;
        };
        let Ok(ad) = AccountData::try_from(account.data.as_slice()) else {
            continue;
        };
        let Ok(loc) = ad.get_location() else {
            continue;
        };
        if loc.status == LocationStatus::Activated {
            location_coords.insert(*pk, (loc.lat, loc.lng));
        }
    }

    // Join devices with their location coordinates.
    let mut infos = HashMap::new();
    for (pk, device) in devices {
        let (lat, lng) = location_coords
            .get(&device.location_pk)
            .copied()
            .unwrap_or((0.0, 0.0));
        let user_tunnel_endpoints = device
            .interfaces
            .iter()
            .filter_map(|iface| {
                let iface = iface.into_current_version();
                if iface.user_tunnel_endpoint && iface.ip_net != Default::default() {
                    Some(iface.ip_net.ip())
                } else {
                    None
                }
            })
            .collect();
        infos.insert(
            pk,
            DzDeviceInfo {
                pk,
                code: device.code.clone(),
                lat,
                lng,
                users_count: device.users_count,
                max_users: device.max_users,
                reserved_seats: device.reserved_seats,
                multicast_publishers_count: device.multicast_publishers_count,
                max_multicast_publishers: device.max_multicast_publishers,
                public_ip: device.public_ip,
                user_tunnel_endpoints,
            },
        );
    }

    Ok(infos)
}

/// Fetch a map of min-latency (in microseconds) between device pairs for the current epoch.
///
/// Returns `HashMap<(origin_device_pk, target_device_pk), min_latency_us>`.
/// The map is derived from `DeviceLatencySamples` onchain telemetry accounts.
pub fn fetch_device_latency_map(
    client: &RpcClient,
    telemetry_program_id: &Pubkey,
) -> Result<HashMap<(Pubkey, Pubkey), f64>> {
    const DEVICE_LATENCY_SAMPLES_ACCOUNT_TYPE: u8 = 3;

    let epoch_info = client
        .get_epoch_info()
        .context("failed to fetch epoch info")?;
    let epoch = epoch_info.epoch;

    let accounts = client
        .get_program_accounts_with_config(
            telemetry_program_id,
            RpcProgramAccountsConfig {
                filters: Some(vec![
                    RpcFilterType::Memcmp(Memcmp::new_raw_bytes(
                        0,
                        vec![DEVICE_LATENCY_SAMPLES_ACCOUNT_TYPE],
                    )),
                    RpcFilterType::Memcmp(Memcmp::new_raw_bytes(1, epoch.to_le_bytes().to_vec())),
                ]),
                account_config: RpcAccountInfoConfig {
                    encoding: Some(UiAccountEncoding::Base64),
                    ..Default::default()
                },
                ..Default::default()
            },
        )
        .context("failed to fetch DeviceLatencySamples accounts")?;

    let mut map: HashMap<(Pubkey, Pubkey), f64> = HashMap::new();

    for (_pk, account) in accounts {
        let Ok(samples) = DeviceLatencySamples::try_from(account.data.as_slice()) else {
            continue;
        };
        if samples.samples.is_empty() {
            continue;
        }
        let origin = samples.header.origin_device_pk;
        let target = samples.header.target_device_pk;
        // Zero RTT indicates packet loss — exclude from min calculation.
        // If all samples are zero (total loss), skip this account entirely.
        let Some(min_us) = samples
            .samples
            .iter()
            .copied()
            .filter(|&s| s > 0)
            .min()
            .map(|v| v as f64)
        else {
            continue;
        };
        map.entry((origin, target))
            .and_modify(|e| {
                if min_us < *e {
                    *e = min_us;
                }
            })
            .or_insert(min_us);
    }

    Ok(map)
}

/// Resolve a multicast group key-or-code to a pubkey.
pub async fn resolve_multicast_group(
    key_or_code: &str,
    dz_client: &dyn DzLedgerReader,
) -> Result<Pubkey> {
    if let Ok(pk) = key_or_code.parse::<Pubkey>() {
        return Ok(pk);
    }

    match dz_client.resolve_multicast_group_code(key_or_code).await? {
        Some(pk) => {
            eprintln!("Resolved multicast group '{key_or_code}' -> {pk}");
            Ok(pk)
        }
        None => {
            bail!(
                "Multicast group not found: {key_or_code} \
                 (not a valid pubkey or known group code)"
            );
        }
    }
}
