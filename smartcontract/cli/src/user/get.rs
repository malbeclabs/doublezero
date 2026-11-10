use crate::{
    doublezerocommand::CliCommand,
    feed::resolve::resolve_feed_labels,
    helpers::slot_to_datetime,
    user::list::{format_accesspasses, user_accesspasses},
    validators::validate_pubkey,
};
use clap::Args;
use doublezero_cli_core::{label_addresses, CliContext};
use doublezero_program_common::serializer;
use doublezero_sdk::commands::{
    accesspass::get::GetAccessPassCommand, device::list::ListDeviceCommand,
    multicastgroup::list::ListMulticastGroupCommand, tenant::list::ListTenantCommand,
    user::get::GetUserCommand,
};
use doublezero_serviceability::state::accountdata::AccountData;
use eyre::WrapErr;
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::{collections::HashMap, io::Write, net::Ipv4Addr, str::FromStr};
use tabled::Tabled;

#[derive(Args, Debug)]
pub struct GetUserCliCommand {
    /// User Pubkey to retrieve
    #[arg(long, value_parser = validate_pubkey)]
    pub pubkey: String,
    /// Output as JSON
    #[arg(long)]
    pub json: bool,
}

#[derive(Tabled, Serialize)]
struct UserDisplay {
    pub account: String,
    pub user_type: String,
    pub tenant: String,
    #[tabled(skip)]
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub device_pk: Pubkey,
    pub device: String,
    pub cyoa_type: String,
    pub client_ip: String,
    pub tunnel_id: u16,
    pub tunnel_net: String,
    pub dz_ip: String,
    pub tunnel_endpoint: String,
    pub validator_pubkey: String,
    pub accesspass: String,
    /// The address of the pass the user records, or of both candidate passes of a legacy user.
    #[tabled(skip)]
    #[serde(serialize_with = "serializer::serialize_pubkeylist_as_string")]
    pub accesspass_pks: Vec<Pubkey>,
    pub feeds: String,
    pub publishers: String,
    pub subscribers: String,
    pub status: String,
    pub bgp_status: String,
    #[tabled(skip)]
    pub last_bgp_reported_at: u64,
    #[tabled(rename = "last_bgp_reported_at")]
    pub last_bgp_reported_at_str: String,
    #[tabled(skip)]
    pub last_bgp_up_at: u64,
    #[tabled(rename = "last_bgp_up_at")]
    pub last_bgp_up_at_str: String,
    /// Raw BGP RTT in nanoseconds (kept in JSON output for tooling).
    #[tabled(skip)]
    pub bgp_rtt_ns: u64,
    /// Pretty-printed BGP RTT (e.g. "5.50 ms" or "-" when no sample yet).
    #[tabled(rename = "bgp_rtt")]
    pub bgp_rtt_str: String,
    pub owner: String,
}

/// Formats a BGP RTT value in nanoseconds as a human-readable millisecond
/// string. Returns "-" for 0 (the sentinel for "no sample observed").
fn format_bgp_rtt_ns(ns: u64) -> String {
    if ns == 0 {
        return "-".to_string();
    }
    format!("{:.2} ms", ns as f64 / 1_000_000.0)
}

impl GetUserCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        let pubkey = Pubkey::from_str(&self.pubkey)?;
        let (pubkey, user) = client.get_user(GetUserCommand { pubkey })?;

        // Read only the passes the user can be on, then pick among them as `user list` does.
        let mut candidates = HashMap::new();
        if user.accesspass_pk != Pubkey::default() {
            // `get_multiple_accounts` reports a closed pass as `None` and a failed read as `Err`,
            // as `GetAccessPassCommand` does. A failed RPC read or decode then fails the command
            // instead of printing `none`.
            let account = client
                .get_multiple_accounts(vec![user.accesspass_pk])?
                .into_iter()
                .next()
                .flatten()
                .filter(|account| account.owner == client.get_program_id());
            if let Some(account) = account {
                if let AccountData::AccessPass(accesspass) =
                    AccountData::try_from(&account.data[..]).wrap_err_with(|| {
                        format!("decoding the AccessPass at {}", user.accesspass_pk)
                    })?
                {
                    candidates.insert(user.accesspass_pk, accesspass);
                }
            }
        } else {
            for client_ip in [user.client_ip, Ipv4Addr::UNSPECIFIED] {
                if let Some((pk, accesspass)) = client.get_accesspass(GetAccessPassCommand {
                    client_ip,
                    user_payer: user.owner,
                })? {
                    candidates.insert(pk, accesspass);
                }
            }
        }
        let accesspasses = user_accesspasses(&client.get_program_id(), &user, &candidates);
        if accesspasses.is_empty() {
            if user.accesspass_pk != Pubkey::default() {
                tracing::warn!(
                    accesspass = %user.accesspass_pk,
                    "the access pass the user records is closed"
                );
            } else {
                tracing::warn!(
                    client_ip = %user.client_ip,
                    payer = %user.owner,
                    "no access pass found for user"
                );
            }
        }
        let multicast_groups = client.list_multicastgroup(ListMulticastGroupCommand {})?;
        let tenants = client.list_tenant(ListTenantCommand {})?;
        let devices = client.list_device(ListDeviceCommand {})?;

        let feeds = resolve_feed_labels(client, &user.feed_pks)?;

        let tenant_str = if user.tenant_pk == Pubkey::default() {
            String::new()
        } else {
            tenants
                .get(&user.tenant_pk)
                .map_or(user.tenant_pk.to_string(), |t| t.code.clone())
        };
        let display = UserDisplay {
            account: pubkey.to_string(),
            user_type: user.user_type.to_string(),
            tenant: tenant_str,
            device_pk: user.device_pk,
            device: devices
                .get(&user.device_pk)
                .map_or(user.device_pk.to_string(), |d| d.code.clone()),
            cyoa_type: user.cyoa_type.to_string(),
            client_ip: user.client_ip.to_string(),
            tunnel_id: user.tunnel_id,
            tunnel_net: user.tunnel_net.to_string(),
            dz_ip: user.dz_ip.to_string(),
            tunnel_endpoint: user.tunnel_endpoint.to_string(),
            validator_pubkey: user.validator_pubkey.to_string(),
            accesspass: format_accesspasses(&accesspasses),
            accesspass_pks: accesspasses.iter().map(|(pk, _)| *pk).collect(),
            // `code:metro`, as `access-pass get` names the same feeds: a feed is keyed by
            // (code, exchange), so the code on its own does not say which feed is held.
            feeds: user
                .feed_pks
                .iter()
                .map(|pk| feeds.label(pk))
                .collect::<Vec<_>>()
                .join(", "),
            publishers: user
                .publishers
                .iter()
                .map(|pk| {
                    multicast_groups
                        .get(pk)
                        .map_or(pk.to_string(), |mg| mg.code.clone())
                })
                .collect::<Vec<_>>()
                .join(", "),
            subscribers: user
                .subscribers
                .iter()
                .map(|pk| {
                    multicast_groups
                        .get(pk)
                        .map_or(pk.to_string(), |mg| mg.code.clone())
                })
                .collect::<Vec<_>>()
                .join(", "),
            status: user.status.to_string(),
            bgp_status: user.bgp_status.to_string(),
            last_bgp_reported_at: user.last_bgp_reported_at,
            last_bgp_reported_at_str: slot_to_datetime(client, user.last_bgp_reported_at),
            last_bgp_up_at: user.last_bgp_up_at,
            last_bgp_up_at_str: slot_to_datetime(client, user.last_bgp_up_at),
            bgp_rtt_ns: user.bgp_rtt_ns,
            bgp_rtt_str: format_bgp_rtt_ns(user.bgp_rtt_ns),
            owner: user.owner.to_string(),
        };

        if self.json {
            let json = serde_json::to_string_pretty(&display)?;
            writeln!(out, "{json}")?;
        } else {
            let headers = UserDisplay::headers();
            let fields = display.fields();
            let max_len = headers.iter().map(|h| h.len()).max().unwrap_or(0);
            for (header, value) in headers.iter().zip(fields.iter()) {
                let value = label_addresses(value, &ctx.address_labels);
                writeln!(out, " {header:<max_len$} | {value}")?;
            }
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};

    use crate::{
        doublezerocommand::CliCommand, tests::utils::create_test_client,
        user::get::GetUserCliCommand,
    };
    use doublezero_sdk::{
        commands::{
            accesspass,
            device::list::ListDeviceCommand,
            multicastgroup::list::ListMulticastGroupCommand,
            tenant::list::ListTenantCommand,
            user::{delete::DeleteUserCommand, get::GetUserCommand},
        },
        AccountType, Device, Exchange, ExchangeStatus, Feed, MulticastGroup, User, UserCYOA,
        UserStatus, UserType,
    };
    use doublezero_serviceability::{
        pda::{get_accesspass_pda, get_user_old_pda},
        state::{
            accesspass::{AccessPass, AccessPassKind, AccessPassStatus, AccessPassType},
            device::{DeviceDesiredStatus, DeviceHealth, DeviceStatus, DeviceType},
            tenant::{Tenant, TenantBillingConfig, TenantPaymentStatus},
        },
    };
    use mockall::predicate;
    use solana_sdk::{account::Account, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_user_get() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_user_old_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let tenant_pubkey = Pubkey::new_unique();
        let tenant = Tenant {
            account_type: AccountType::Tenant,
            owner: client.get_payer(),
            bump_seed: 0,
            code: "test-tenant".to_string(),
            vrf_id: 100,
            reference_count: 0,
            administrators: vec![],
            token_account: Pubkey::default(),
            payment_status: TenantPaymentStatus::Paid,
            metro_routing: false,
            route_liveness: false,
            billing: TenantBillingConfig::default(),
            include_topologies: vec![],
        };

        let device_pubkey = Pubkey::new_unique();
        let device = Device {
            account_type: AccountType::Device,
            owner: client.get_payer(),
            index: 1,
            bump_seed: 0,
            location_pk: Pubkey::default(),
            exchange_pk: Pubkey::default(),
            device_type: DeviceType::Hybrid,
            public_ip: "192.168.1.1".parse().unwrap(),
            status: DeviceStatus::Activated,
            code: "test-device".to_string(),
            dz_prefixes: vec![].into(),
            metrics_publisher_pk: Pubkey::default(),
            contributor_pk: Pubkey::default(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            reference_count: 0,
            users_count: 0,
            max_users: 1000,
            device_health: DeviceHealth::ReadyForUsers,
            desired_status: DeviceDesiredStatus::Activated,
            unicast_users_count: 0,
            multicast_subscribers_count: 0,
            max_unicast_users: 1000,
            max_multicast_subscribers: 1000,
            reserved_seats: 0,
            multicast_publishers_count: 0,
            max_multicast_publishers: 0,
            ..Default::default()
        };

        let mgroup_pubkey = Pubkey::new_unique();
        let mgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            owner: client.get_payer(),
            bump_seed: 0,
            index: 1,
            code: "test".to_string(),
            max_bandwidth: 1000,
            status: doublezero_sdk::MulticastGroupStatus::Activated,
            tenant_pk: Pubkey::default(),
            multicast_ip: "100.0.0.1".parse().unwrap(),
            publisher_count: 0,
            subscriber_count: 1,
            bond_ref: Pubkey::default(),
            private: false,
            max_publishers: 0,
            max_subscribers: 0,
        };

        let user = User {
            account_type: AccountType::User,
            index: 1,
            bump_seed: 255,
            user_type: UserType::IBRL,
            tenant_pk: tenant_pubkey,
            cyoa_type: UserCYOA::GREOverDIA,
            device_pk: device_pubkey,
            client_ip: [10, 0, 0, 1].into(),
            dz_ip: [10, 0, 0, 2].into(),
            tunnel_id: 0,
            tunnel_net: "10.2.3.4/24".parse().unwrap(),
            status: UserStatus::Activated,
            owner: pda_pubkey,
            publishers: vec![],
            subscribers: vec![mgroup_pubkey],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            ..Default::default()
        };

        let (accesspass_pubkey, _) =
            get_accesspass_pda(&client.get_program_id(), &user.client_ip, &user.owner);
        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 255,
            accesspass_type: AccessPassType::Prepaid,
            client_ip: user.client_ip,
            user_payer: user.owner,
            last_access_epoch: 10,
            connection_count: 0,
            status: AccessPassStatus::Connected,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            owner: client.get_payer(),
            flags: 0,
            unicast_user_count: 0,
            max_unicast_users: 1,
            public_multicast_user_count: 0,
            max_multicast_users: 1,
        };

        client
            .expect_list_multicastgroup()
            .with(predicate::eq(ListMulticastGroupCommand {}))
            .returning(move |_| {
                let mut map = std::collections::HashMap::new();
                map.insert(mgroup_pubkey, mgroup.clone());
                Ok(map)
            });
        client
            .expect_list_tenant()
            .with(predicate::eq(ListTenantCommand {}))
            .returning(move |_| {
                let mut map = std::collections::HashMap::new();
                map.insert(tenant_pubkey, tenant.clone());
                Ok(map)
            });
        client
            .expect_list_device()
            .with(predicate::eq(ListDeviceCommand {}))
            .returning(move |_| {
                let mut map = std::collections::HashMap::new();
                map.insert(device_pubkey, device.clone());
                Ok(map)
            });
        // A legacy user: the pass at its client IP exists, the dynamic one does not.
        let user_client_ip = user.client_ip;
        client.expect_get_accesspass().returning(
            move |cmd: accesspass::get::GetAccessPassCommand| {
                Ok((cmd.client_ip == user_client_ip)
                    .then(|| (accesspass_pubkey, accesspass.clone())))
            },
        );

        client
            .expect_get_user()
            .with(predicate::eq(GetUserCommand { pubkey: pda_pubkey }))
            .returning(move |_| Ok((pda_pubkey, user.clone())));

        client
            .expect_delete_user()
            .with(predicate::eq(DeleteUserCommand::new(
                pda_pubkey,
                Some(AccessPassKind::Prepaid),
            )))
            .returning(move |_| Ok(signature));

        // Expected success (table)
        let mut output = Vec::new();
        let ctx = cli_context_default_for_tests();
        let res = block_on(
            GetUserCliCommand {
                pubkey: pda_pubkey.to_string(),
                json: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok(), "I should find a item by pubkey");
        let output_str = String::from_utf8(output).unwrap();
        let has_row = |header: &str, value: &str| {
            output_str
                .lines()
                .any(|l| l.contains(header) && l.contains(value))
        };
        assert!(
            has_row("account", &pda_pubkey.to_string()),
            "account row should contain pubkey"
        );
        assert!(
            has_row("user_type", "IBRL"),
            "user_type row should contain value"
        );
        assert!(
            has_row("tenant", "test-tenant"),
            "tenant row should contain value"
        );
        assert!(
            has_row("device", "test-device"),
            "device row should contain value"
        );
        assert!(
            has_row("status", "activated"),
            "status row should contain value"
        );
        assert!(
            has_row("subscribers", "test"),
            "subscribers row should contain group name"
        );
        assert!(
            has_row("accesspass", "prepaid"),
            "accesspass row should name the pass type"
        );
    }

    #[test]
    fn test_cli_user_get_renders_held_feeds() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_user_old_pda(&client.get_program_id(), 1);
        let feed_key = Pubkey::new_unique();
        let exchange_key = Pubkey::new_unique();
        let group_pubkey = Pubkey::new_unique();

        let user = User {
            account_type: AccountType::User,
            index: 1,
            bump_seed: 255,
            user_type: UserType::Multicast,
            tenant_pk: Pubkey::default(),
            cyoa_type: UserCYOA::GREOverDIA,
            device_pk: Pubkey::new_unique(),
            client_ip: [10, 0, 0, 1].into(),
            dz_ip: [10, 0, 0, 2].into(),
            tunnel_id: 0,
            tunnel_net: "10.2.3.4/24".parse().unwrap(),
            status: UserStatus::Activated,
            owner: pda_pubkey,
            publishers: vec![],
            subscribers: vec![group_pubkey],
            feed_pks: vec![feed_key],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            accesspass_pk: Pubkey::new_unique(),
            holds_pass_seat: false,
        };

        let mgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 1,
            bump_seed: 1,
            owner: Pubkey::new_unique(),
            tenant_pk: Pubkey::default(),
            multicast_ip: [239, 0, 0, 2].into(),
            max_bandwidth: 1_000_000_000,
            status: doublezero_sdk::MulticastGroupStatus::Activated,
            code: "qa-payments-group".to_string(),
            publisher_count: 0,
            subscriber_count: 1,
            bond_ref: Pubkey::default(),
            private: false,
            max_publishers: 0,
            max_subscribers: 0,
        };

        let feed = Feed {
            account_type: AccountType::Feed,
            owner: Pubkey::new_unique(),
            bump_seed: 0,
            code: "qa-payments".to_string(),
            name: "QA Payments".to_string(),
            exchange: exchange_key,
            groups: vec![group_pubkey],
            ..Default::default()
        };

        let exchange = Exchange {
            account_type: AccountType::Exchange,
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 255,
            lat: 52.37,
            lng: 4.89,
            bgp_community: 10001,
            unused: 0,
            status: ExchangeStatus::Activated,
            code: "xams".to_string(),
            name: "Amsterdam".to_string(),
            reference_count: 0,
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
        };

        let recorded_pk = user.accesspass_pk;
        client
            .expect_get_user()
            .with(predicate::eq(GetUserCommand { pubkey: pda_pubkey }))
            .returning(move |_| Ok((pda_pubkey, user.clone())));
        // The user records a pass that has since been closed.
        client
            .expect_get_multiple_accounts()
            .with(predicate::eq(vec![recorded_pk]))
            .returning(|_| Ok(vec![None]));
        client
            .expect_list_multicastgroup()
            .with(predicate::eq(ListMulticastGroupCommand {}))
            .returning(move |_| {
                let mut map = std::collections::HashMap::new();
                map.insert(group_pubkey, mgroup.clone());
                Ok(map)
            });
        client
            .expect_list_tenant()
            .with(predicate::eq(ListTenantCommand {}))
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_device()
            .with(predicate::eq(ListDeviceCommand {}))
            .returning(|_| Ok(std::collections::HashMap::new()));
        // The feeds the user holds, then the metros those feeds serve; mockall tells the two reads
        // apart by the keys asked for.
        client
            .expect_get_multiple_accounts()
            .with(predicate::eq(vec![feed_key]))
            .returning(move |_| {
                Ok(vec![Some(Account {
                    data: borsh::to_vec(&feed).unwrap(),
                    ..Account::default()
                })])
            });
        client
            .expect_get_multiple_accounts()
            .with(predicate::eq(vec![exchange_key]))
            .returning(move |_| {
                Ok(vec![Some(Account {
                    data: borsh::to_vec(&exchange).unwrap(),
                    ..Account::default()
                })])
            });

        let mut output = Vec::new();
        let ctx = cli_context_default_for_tests();
        let res = block_on(
            GetUserCliCommand {
                pubkey: pda_pubkey.to_string(),
                json: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());

        let output_str = String::from_utf8(output).unwrap();
        let row = |header: &str| {
            output_str
                .lines()
                .find(|l| l.trim_start().starts_with(header))
                .unwrap_or_else(|| panic!("no {header} row"))
                .split('|')
                .nth(1)
                .unwrap()
                .trim()
                .to_string()
        };
        // The same `code:metro` form `access-pass get` uses for the seat this user holds.
        assert_eq!(row("feeds"), "qa-payments:xams");
        assert_eq!(row("subscribers"), "qa-payments-group");
    }

    #[test]
    fn test_cli_user_get_no_accesspass() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_user_old_pda(&client.get_program_id(), 1);

        let tenant_pubkey = Pubkey::new_unique();
        let tenant = Tenant {
            account_type: AccountType::Tenant,
            owner: client.get_payer(),
            bump_seed: 0,
            code: "test-tenant".to_string(),
            vrf_id: 100,
            reference_count: 0,
            administrators: vec![],
            token_account: Pubkey::default(),
            payment_status: TenantPaymentStatus::Paid,
            metro_routing: false,
            route_liveness: false,
            billing: TenantBillingConfig::default(),
            include_topologies: vec![],
        };

        let device_pubkey = Pubkey::new_unique();
        let device = Device {
            account_type: AccountType::Device,
            owner: client.get_payer(),
            index: 1,
            bump_seed: 0,
            location_pk: Pubkey::default(),
            exchange_pk: Pubkey::default(),
            device_type: DeviceType::Hybrid,
            public_ip: "192.168.1.1".parse().unwrap(),
            status: DeviceStatus::Activated,
            code: "test-device".to_string(),
            dz_prefixes: vec![].into(),
            metrics_publisher_pk: Pubkey::default(),
            contributor_pk: Pubkey::default(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            reference_count: 0,
            users_count: 0,
            max_users: 1000,
            device_health: DeviceHealth::ReadyForUsers,
            desired_status: DeviceDesiredStatus::Activated,
            unicast_users_count: 0,
            multicast_subscribers_count: 0,
            max_unicast_users: 1000,
            max_multicast_subscribers: 1000,
            reserved_seats: 0,
            multicast_publishers_count: 0,
            max_multicast_publishers: 0,
            ..Default::default()
        };

        let user = User {
            account_type: AccountType::User,
            index: 1,
            bump_seed: 255,
            user_type: UserType::IBRL,
            tenant_pk: tenant_pubkey,
            cyoa_type: UserCYOA::GREOverDIA,
            device_pk: device_pubkey,
            client_ip: [10, 0, 0, 1].into(),
            dz_ip: [10, 0, 0, 2].into(),
            tunnel_id: 0,
            tunnel_net: "10.2.3.4/24".parse().unwrap(),
            status: UserStatus::Activated,
            owner: pda_pubkey,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            ..Default::default()
        };

        client
            .expect_list_multicastgroup()
            .with(predicate::eq(ListMulticastGroupCommand {}))
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_tenant()
            .with(predicate::eq(ListTenantCommand {}))
            .returning(move |_| {
                let mut map = std::collections::HashMap::new();
                map.insert(tenant_pubkey, tenant.clone());
                Ok(map)
            });
        client
            .expect_list_device()
            .with(predicate::eq(ListDeviceCommand {}))
            .returning(move |_| {
                let mut map = std::collections::HashMap::new();
                map.insert(device_pubkey, device.clone());
                Ok(map)
            });
        client.expect_get_accesspass().returning(|_| Ok(None));

        client
            .expect_get_user()
            .with(predicate::eq(GetUserCommand { pubkey: pda_pubkey }))
            .returning(move |_| Ok((pda_pubkey, user.clone())));

        let mut output = Vec::new();
        let ctx = cli_context_default_for_tests();
        let res = block_on(
            GetUserCliCommand {
                pubkey: pda_pubkey.to_string(),
                json: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok(), "should succeed even without an access pass");
        let output_str = String::from_utf8(output).unwrap();
        let has_row = |header: &str, value: &str| {
            output_str
                .lines()
                .any(|l| l.contains(header) && l.contains(value))
        };
        assert!(
            has_row("account", &pda_pubkey.to_string()),
            "account row should contain pubkey"
        );
        assert!(
            has_row("status", "activated"),
            "status row should contain value"
        );
    }

    #[test]
    fn test_cli_user_get_shows_the_recorded_pass() {
        // The user records its prepaid pass. The command reads that pass by address and never
        // looks up the payer's 0.0.0.0 pass, which used to win and label the user EdgeSeat.
        let mut client = create_test_client();
        let user_pk = Pubkey::new_unique();
        let prepaid_pk = Pubkey::new_unique();
        let user = User {
            account_type: AccountType::User,
            user_type: UserType::IBRL,
            client_ip: [10, 0, 0, 1].into(),
            owner: Pubkey::new_unique(),
            status: UserStatus::Activated,
            accesspass_pk: prepaid_pk,
            ..Default::default()
        };
        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            owner: Pubkey::new_unique(),
            bump_seed: 255,
            accesspass_type: AccessPassType::Prepaid,
            client_ip: user.client_ip,
            user_payer: user.owner,
            last_access_epoch: u64::MAX,
            connection_count: 1,
            status: AccessPassStatus::Connected,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            flags: 0,
            tenant_allowlist: vec![],
            unicast_user_count: 1,
            max_unicast_users: 1,
            public_multicast_user_count: 0,
            max_multicast_users: 1,
        };

        client
            .expect_get_user()
            .with(predicate::eq(GetUserCommand { pubkey: user_pk }))
            .returning(move |_| Ok((user_pk, user.clone())));
        let program_id = client.get_program_id();
        client
            .expect_get_multiple_accounts()
            .with(predicate::eq(vec![prepaid_pk]))
            .returning(move |_| {
                Ok(vec![Some(Account {
                    data: borsh::to_vec(&accesspass).unwrap(),
                    owner: program_id,
                    ..Account::default()
                })])
            });
        client.expect_get_accesspass().times(0);
        client
            .expect_list_multicastgroup()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_tenant()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_device()
            .returning(|_| Ok(std::collections::HashMap::new()));

        let mut output = Vec::new();
        let res = block_on(
            GetUserCliCommand {
                pubkey: user_pk.to_string(),
                json: true,
            }
            .execute(&cli_context_default_for_tests(), &client, &mut output),
        );
        assert!(res.is_ok(), "{res:?}");
        let json: serde_json::Value = serde_json::from_slice(&output).unwrap();
        assert_eq!(json["accesspass"], "prepaid");
        assert_eq!(json["accesspass_pks"], prepaid_pk.to_string());
    }

    #[test]
    fn test_cli_user_get_fails_when_the_recorded_pass_read_fails() {
        // A failed RPC read must not print `none`, which an operator would read as a closed pass.
        let mut client = create_test_client();
        let user_pk = Pubkey::new_unique();
        let user = User {
            account_type: AccountType::User,
            user_type: UserType::IBRL,
            client_ip: [10, 0, 0, 1].into(),
            owner: Pubkey::new_unique(),
            status: UserStatus::Activated,
            accesspass_pk: Pubkey::new_unique(),
            ..Default::default()
        };

        client
            .expect_get_user()
            .with(predicate::eq(GetUserCommand { pubkey: user_pk }))
            .returning(move |_| Ok((user_pk, user.clone())));
        client
            .expect_get_multiple_accounts()
            .returning(|_| Err(eyre::eyre!("rpc timeout")));

        let mut output = Vec::new();
        let res = block_on(
            GetUserCliCommand {
                pubkey: user_pk.to_string(),
                json: true,
            }
            .execute(&cli_context_default_for_tests(), &client, &mut output),
        );
        assert!(res.unwrap_err().to_string().contains("rpc timeout"));
        assert!(output.is_empty());
    }
}
