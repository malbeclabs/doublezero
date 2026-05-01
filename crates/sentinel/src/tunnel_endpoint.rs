use std::net::Ipv4Addr;

use solana_sdk::pubkey::Pubkey;

/// Candidate tunnel endpoints for a device.
#[derive(Debug, Clone)]
pub struct DeviceEndpoints {
    pub public_ip: Ipv4Addr,
    /// IPs from device interfaces where `user_tunnel_endpoint == true`.
    pub user_tunnel_endpoints: Vec<Ipv4Addr>,
    /// Number of DzPrefixBlock resource extension accounts on this device — needed when
    /// building create-user instructions under onchain allocation, since every DzPrefixBlock
    /// must be supplied even for users that do not allocate from them.
    pub dz_prefix_count: u8,
}

impl Default for DeviceEndpoints {
    fn default() -> Self {
        Self {
            public_ip: Ipv4Addr::UNSPECIFIED,
            user_tunnel_endpoints: Vec::new(),
            dz_prefix_count: 0,
        }
    }
}

/// Select a tunnel endpoint for a user, preferring a `user_tunnel_endpoint`
/// interface IP and falling back to the device's `public_ip`. Any IP in
/// `exclude_ips` is skipped — these are the endpoints already used by other
/// user accounts at the same `client_ip`.
///
/// Returns `Ipv4Addr::UNSPECIFIED` when no endpoint is available; the activator
/// will then reject the create.
pub fn select_tunnel_endpoint(
    public_ip: Ipv4Addr,
    user_tunnel_endpoints: &[Ipv4Addr],
    exclude_ips: &[Ipv4Addr],
) -> Ipv4Addr {
    for ep in user_tunnel_endpoints {
        if !exclude_ips.contains(ep) {
            return *ep;
        }
    }

    if public_ip != Ipv4Addr::UNSPECIFIED && !exclude_ips.contains(&public_ip) {
        return public_ip;
    }

    Ipv4Addr::UNSPECIFIED
}

/// Collect the set of tunnel endpoints already in use by other user accounts
/// at the same `client_ip`.
///
/// A user with `tunnel_endpoint == UNSPECIFIED` is a legacy account from
/// before the field was populated onchain; the activator implicitly routes
/// its tunnel through the device's `public_ip`, so resolve it from
/// `device_endpoints` rather than dropping the user.
pub fn in_use_tunnel_endpoints<'a, I>(
    users: I,
    client_ip: Ipv4Addr,
    device_endpoints: &std::collections::HashMap<Pubkey, DeviceEndpoints>,
) -> Vec<Ipv4Addr>
where
    I: IntoIterator<Item = &'a crate::dz_ledger_reader::DzUser>,
{
    users
        .into_iter()
        .filter(|u| u.client_ip == client_ip)
        .map(|u| {
            if u.tunnel_endpoint != Ipv4Addr::UNSPECIFIED {
                u.tunnel_endpoint
            } else {
                device_endpoints
                    .get(&u.device_pk)
                    .map(|d| d.public_ip)
                    .unwrap_or(Ipv4Addr::UNSPECIFIED)
            }
        })
        .filter(|ip| *ip != Ipv4Addr::UNSPECIFIED)
        .collect()
}

/// Pick a tunnel endpoint for a user on `device_pk`, given all existing users
/// (to derive the exclude list) and the device endpoint map. Returns
/// `Ipv4Addr::UNSPECIFIED` when the device is unknown or has no free endpoint.
pub fn select_tunnel_endpoint_for_user(
    device_pk: &Pubkey,
    client_ip: Ipv4Addr,
    all_users: &[crate::dz_ledger_reader::DzUser],
    device_endpoints: &std::collections::HashMap<Pubkey, DeviceEndpoints>,
) -> Ipv4Addr {
    let Some(endpoints) = device_endpoints.get(device_pk) else {
        return Ipv4Addr::UNSPECIFIED;
    };
    let exclude = in_use_tunnel_endpoints(all_users, client_ip, device_endpoints);
    select_tunnel_endpoint(
        endpoints.public_ip,
        &endpoints.user_tunnel_endpoints,
        &exclude,
    )
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use doublezero_sdk::UserType;

    use super::*;
    use crate::dz_ledger_reader::DzUser;

    #[test]
    fn prefers_first_ute_not_excluded() {
        let ute1 = Ipv4Addr::new(10, 0, 0, 11);
        let ute2 = Ipv4Addr::new(10, 0, 0, 12);
        assert_eq!(
            select_tunnel_endpoint(Ipv4Addr::new(1, 1, 1, 1), &[ute1, ute2], &[]),
            ute1,
        );
    }

    #[test]
    fn skips_excluded_ute_and_picks_next() {
        let ute1 = Ipv4Addr::new(10, 0, 0, 11);
        let ute2 = Ipv4Addr::new(10, 0, 0, 12);
        assert_eq!(
            select_tunnel_endpoint(Ipv4Addr::new(1, 1, 1, 1), &[ute1, ute2], &[ute1]),
            ute2,
        );
    }

    #[test]
    fn falls_back_to_public_ip_when_all_utes_excluded() {
        let ute1 = Ipv4Addr::new(10, 0, 0, 11);
        let public_ip = Ipv4Addr::new(1, 1, 1, 1);
        assert_eq!(
            select_tunnel_endpoint(public_ip, &[ute1], &[ute1]),
            public_ip,
        );
    }

    #[test]
    fn uses_public_ip_when_no_utes() {
        let public_ip = Ipv4Addr::new(1, 1, 1, 1);
        assert_eq!(select_tunnel_endpoint(public_ip, &[], &[]), public_ip);
    }

    #[test]
    fn returns_unspecified_when_everything_excluded() {
        let ute1 = Ipv4Addr::new(10, 0, 0, 11);
        let public_ip = Ipv4Addr::new(1, 1, 1, 1);
        assert_eq!(
            select_tunnel_endpoint(public_ip, &[ute1], &[ute1, public_ip]),
            Ipv4Addr::UNSPECIFIED,
        );
    }

    #[test]
    fn returns_unspecified_when_no_endpoints_at_all() {
        assert_eq!(
            select_tunnel_endpoint(Ipv4Addr::UNSPECIFIED, &[], &[]),
            Ipv4Addr::UNSPECIFIED,
        );
    }

    fn make_user(ip: [u8; 4], device_pk: Pubkey, tunnel_endpoint: Ipv4Addr) -> DzUser {
        DzUser {
            owner: Pubkey::default(),
            client_ip: Ipv4Addr::from(ip),
            device_pk,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRL,
            publishers: vec![],
            tunnel_endpoint,
        }
    }

    #[test]
    fn in_use_tunnel_endpoints_filters_by_client_ip() {
        let ip_a = [10, 0, 0, 1];
        let ip_b = [10, 0, 0, 2];
        let ep1 = Ipv4Addr::new(1, 1, 1, 1);
        let ep2 = Ipv4Addr::new(2, 2, 2, 2);
        let users = vec![
            make_user(ip_a, Pubkey::new_unique(), ep1),
            make_user(ip_b, Pubkey::new_unique(), ep2),
        ];
        let in_use = in_use_tunnel_endpoints(&users, Ipv4Addr::from(ip_a), &HashMap::new());
        assert_eq!(in_use, vec![ep1]);
    }

    #[test]
    fn in_use_tunnel_endpoints_resolves_legacy_unspecified_via_device_public_ip() {
        // A legacy user predating the tunnel_endpoint field stores UNSPECIFIED
        // onchain; the activator implicitly routes its tunnel through the
        // device's public_ip, so we must add that to the in-use set.
        let ip_a = [10, 0, 0, 1];
        let device_a = Pubkey::new_unique();
        let public_ip_a = Ipv4Addr::new(1, 1, 1, 1);

        let mut device_endpoints = HashMap::new();
        device_endpoints.insert(
            device_a,
            DeviceEndpoints {
                public_ip: public_ip_a,
                user_tunnel_endpoints: vec![],
                dz_prefix_count: 0,
            },
        );

        let users = vec![make_user(ip_a, device_a, Ipv4Addr::UNSPECIFIED)];
        let in_use = in_use_tunnel_endpoints(&users, Ipv4Addr::from(ip_a), &device_endpoints);
        assert_eq!(in_use, vec![public_ip_a]);
    }

    #[test]
    fn in_use_tunnel_endpoints_drops_legacy_when_device_unknown() {
        // If the user's device isn't in the endpoint map (e.g., deactivated),
        // we have no way to resolve the implicit endpoint — drop the entry
        // rather than poison the exclude list with UNSPECIFIED.
        let ip_a = [10, 0, 0, 1];
        let users = vec![make_user(ip_a, Pubkey::new_unique(), Ipv4Addr::UNSPECIFIED)];
        let in_use = in_use_tunnel_endpoints(&users, Ipv4Addr::from(ip_a), &HashMap::new());
        assert!(in_use.is_empty());
    }

    #[test]
    fn select_for_user_uses_exclude_list() {
        let device_pk = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(10, 0, 0, 1);
        let ute1 = Ipv4Addr::new(192, 168, 1, 11);
        let ute2 = Ipv4Addr::new(192, 168, 1, 12);

        let mut device_endpoints = HashMap::new();
        device_endpoints.insert(
            device_pk,
            DeviceEndpoints {
                public_ip: Ipv4Addr::new(1, 1, 1, 1),
                user_tunnel_endpoints: vec![ute1, ute2],
                dz_prefix_count: 0,
            },
        );

        // One existing IBRL user at this client_ip already consumed ute1.
        let users = vec![make_user([10, 0, 0, 1], device_pk, ute1)];

        let ep = select_tunnel_endpoint_for_user(&device_pk, client_ip, &users, &device_endpoints);
        assert_eq!(ep, ute2);
    }

    #[test]
    fn select_for_user_unknown_device_returns_unspecified() {
        let device_pk = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(10, 0, 0, 1);
        let device_endpoints = HashMap::new();
        let users: Vec<DzUser> = vec![];
        assert_eq!(
            select_tunnel_endpoint_for_user(&device_pk, client_ip, &users, &device_endpoints),
            Ipv4Addr::UNSPECIFIED,
        );
    }
}
