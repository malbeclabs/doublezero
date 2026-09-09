use crate::{doublezerocommand::CliCommand, feed::resolve::resolve_feed_labels};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_program_common::serializer;
use doublezero_sdk::commands::{
    accesspass::get::GetAccessPassCommand, multicastgroup::list::ListMulticastGroupCommand,
    tenant::list::ListTenantCommand,
};
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, net::Ipv4Addr};
use tabled::Tabled;

#[derive(Args, Debug)]
pub struct GetAccessPassCliCommand {
    /// Client IP address
    #[arg(long)]
    pub client_ip: Ipv4Addr,
    /// User payer public key
    #[arg(long)]
    pub user_payer: Pubkey,
    /// Output as JSON
    #[arg(long)]
    pub json: bool,
}

#[derive(Serialize)]
struct FeedSeatDisplay {
    // A feed is keyed by (code, exchange), so one pass can hold two feeds with the same code in
    // different metros. The key is what tells those two seats apart; the code and the metro name
    // them, and are kept as separate fields so neither has to be split back out of a joined string.
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub feed_key: Pubkey,
    pub feed_code: String,
    pub exchange_code: String,
    pub max_users: u8,
    pub max_future_users: u8,
    pub current_users: u8,
    pub anniversary_day: u8,
    pub window_end: i64,
    pub terminates_at: i64,
}

#[derive(Tabled, Serialize)]
struct AccessPassDisplay {
    pub account: String,
    #[tabled(rename = "type")]
    #[serde(rename = "type")]
    pub accesspass_type: String,
    #[tabled(skip)]
    pub feed_seats: Vec<FeedSeatDisplay>,
    pub client_ip: String,
    pub user_payer: String,
    pub tenant: String,
    pub multicast_pub: String,
    pub multicast_sub: String,
    pub feeds: String,
    pub feed_groups: String,
    pub last_access_epoch: String,
    pub remaining_epoch: String,
    pub flags: String,
    pub connections: u16,
    pub unicast_users: String,
    pub multicast_users: String,
    pub status: String,
    pub owner: String,
}

impl GetAccessPassCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        let epoch = client.get_epoch()?;

        let (pubkey, accesspass) = client
            .get_accesspass(GetAccessPassCommand {
                client_ip: self.client_ip,
                user_payer: self.user_payer,
            })?
            .ok_or_else(|| eyre::eyre!("Access Pass not found"))?;

        let mgroups = client.list_multicastgroup(ListMulticastGroupCommand {})?;
        let tenants = client.list_tenant(ListTenantCommand {})?;

        let tenant_display: Vec<String> = accesspass
            .tenant_allowlist
            .iter()
            .filter(|pk| **pk != Pubkey::default())
            .map(|pk| tenants.get(pk).map_or(pk.to_string(), |t| t.code.clone()))
            .collect();

        let pub_display: Vec<String> = accesspass
            .mgroup_pub_allowlist
            .iter()
            .map(|pk| mgroups.get(pk).map_or(pk.to_string(), |mg| mg.code.clone()))
            .collect();

        let sub_display: Vec<String> = accesspass
            .mgroup_sub_allowlist
            .iter()
            .map(|pk| mgroups.get(pk).map_or(pk.to_string(), |mg| mg.code.clone()))
            .collect();

        let feed_keys: Vec<Pubkey> = accesspass
            .feed_seats()
            .iter()
            .map(|seat| seat.feed_key)
            .collect();
        let feeds = resolve_feed_labels(client, &feed_keys)?;

        let feed_seats: Vec<FeedSeatDisplay> = accesspass
            .feed_seats()
            .iter()
            .map(|seat| FeedSeatDisplay {
                feed_key: seat.feed_key,
                feed_code: feeds.feed_code(&seat.feed_key),
                exchange_code: feeds.exchange_code(&seat.feed_key),
                max_users: seat.max_users,
                max_future_users: seat.max_future_users,
                current_users: seat.current_users,
                anniversary_day: seat.anniversary_day,
                window_end: seat.window_end,
                terminates_at: seat.terminates_at,
            })
            .collect();

        // TODO: qualify each group with its feed's metro. A group is joinable only on a device in
        // that feed's exchange, so this union overstates what a pass with feeds in two metros
        // grants in any one of them.
        let mut feed_group_display: Vec<String> = Vec::new();
        for seat in accesspass.feed_seats() {
            let Some(feed) = feeds.feed(&seat.feed_key) else {
                continue;
            };
            for group in &feed.groups {
                let code = mgroups
                    .get(group)
                    .map_or(group.to_string(), |mg| mg.code.clone());
                if !feed_group_display.contains(&code) {
                    feed_group_display.push(code);
                }
            }
        }

        let remaining_epoch = if accesspass.last_access_epoch == u64::MAX {
            "MAX".to_string()
        } else {
            accesspass
                .last_access_epoch
                .saturating_sub(epoch)
                .to_string()
        };

        let last_access_epoch = if accesspass.last_access_epoch == u64::MAX {
            "MAX".to_string()
        } else {
            accesspass.last_access_epoch.to_string()
        };

        let display = AccessPassDisplay {
            account: pubkey.to_string(),
            accesspass_type: accesspass.accesspass_type.to_string(),
            // Each feed is named `code:metro`, because the code alone does not identify one: a pass
            // holding one code in three metros holds three feeds, and their codes all read alike.
            feeds: accesspass
                .feed_seats()
                .iter()
                .map(|seat| feeds.label(&seat.feed_key))
                .collect::<Vec<_>>()
                .join(", "),
            feed_groups: feed_group_display.join(", "),
            feed_seats,
            client_ip: accesspass.client_ip.to_string(),
            user_payer: accesspass.user_payer.to_string(),
            tenant: tenant_display.join(", "),
            multicast_pub: pub_display.join(", "),
            multicast_sub: sub_display.join(", "),
            last_access_epoch,
            remaining_epoch,
            flags: accesspass.flags_string(),
            connections: accesspass.connection_count,
            unicast_users: format!(
                "{} / {}",
                accesspass.unicast_user_count, accesspass.max_unicast_users
            ),
            multicast_users: format!(
                "{} / {}",
                accesspass.multicast_user_count, accesspass.max_multicast_users
            ),
            status: accesspass.status.to_string(),
            owner: accesspass.owner.to_string(),
        };

        if self.json {
            let json = serde_json::to_string_pretty(&display)?;
            writeln!(out, "{json}")?;
        } else {
            let headers = AccessPassDisplay::headers();
            let fields = display.fields();
            let max_len = headers.iter().map(|h| h.len()).max().unwrap_or(0);
            for (header, value) in headers.iter().zip(fields.iter()) {
                writeln!(out, " {header:<max_len$} | {value}")?;
            }
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{accesspass::get::GetAccessPassCliCommand, tests::utils::create_test_client};
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_sdk::{
        commands::{
            accesspass::get::GetAccessPassCommand, multicastgroup::list::ListMulticastGroupCommand,
            tenant::list::ListTenantCommand,
        },
        AccountType, Exchange, ExchangeStatus, Feed, MulticastGroup,
    };
    use doublezero_serviceability::state::{
        accesspass::{AccessPass, AccessPassStatus, AccessPassType, FeedSeat},
        tenant::{Tenant, TenantBillingConfig, TenantPaymentStatus},
    };
    use mockall::predicate;
    use solana_sdk::{account::Account, pubkey::Pubkey};
    use std::{collections::HashMap, net::Ipv4Addr};

    #[test]
    fn test_cli_accesspass_get() {
        let mut client = create_test_client();

        let client_ip = Ipv4Addr::new(10, 0, 0, 1);
        let user_payer = Pubkey::new_unique();
        let accesspass_pubkey = Pubkey::new_unique();

        let tenant_pubkey = Pubkey::new_unique();
        let tenant = Tenant {
            account_type: AccountType::Tenant,
            owner: Pubkey::new_unique(),
            bump_seed: 0,
            code: "my-tenant".to_string(),
            vrf_id: 100,
            reference_count: 1,
            administrators: vec![],
            token_account: Pubkey::default(),
            payment_status: TenantPaymentStatus::Paid,
            metro_routing: false,
            route_liveness: false,
            billing: TenantBillingConfig::default(),
            include_topologies: vec![],
        };

        let mgroup_pubkey = Pubkey::new_unique();
        let mgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 1,
            bump_seed: 1,
            owner: Pubkey::new_unique(),
            tenant_pk: tenant_pubkey,
            multicast_ip: [239, 0, 0, 1].into(),
            max_bandwidth: 1_000_000_000,
            status: doublezero_sdk::MulticastGroupStatus::Activated,
            code: "mcast-test".to_string(),
            publisher_count: 1,
            subscriber_count: 5,
        };

        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 255,
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            user_payer,
            last_access_epoch: 200,
            connection_count: 3,
            status: AccessPassStatus::Connected,
            mgroup_pub_allowlist: vec![mgroup_pubkey],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![tenant_pubkey],
            owner: Pubkey::new_unique(),
            flags: 0,
            unicast_user_count: 2,
            max_unicast_users: 5,
            multicast_user_count: 1,
            max_multicast_users: 3,
        };

        let accesspass_clone = accesspass.clone();

        client
            .expect_get_accesspass()
            .with(predicate::eq(GetAccessPassCommand {
                client_ip,
                user_payer,
            }))
            .returning(move |_| Ok(Some((accesspass_pubkey, accesspass_clone.clone()))));
        client
            .expect_list_multicastgroup()
            .with(predicate::eq(ListMulticastGroupCommand {}))
            .returning(move |_| {
                let mut map = HashMap::new();
                map.insert(mgroup_pubkey, mgroup.clone());
                Ok(map)
            });
        client
            .expect_list_tenant()
            .with(predicate::eq(ListTenantCommand {}))
            .returning(move |_| {
                let mut map = HashMap::new();
                map.insert(tenant_pubkey, tenant.clone());
                Ok(map)
            });

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            GetAccessPassCliCommand {
                client_ip,
                user_payer,
                json: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        let has_row = |header: &str, value: &str| {
            output_str
                .lines()
                .any(|l| l.contains(header) && l.contains(value))
        };
        assert!(
            has_row("account", &accesspass_pubkey.to_string()),
            "account row should contain pubkey"
        );
        assert!(has_row("type", "prepaid"), "type row should contain value");
        assert!(
            has_row("client_ip", "10.0.0.1"),
            "client_ip row should contain value"
        );
        assert!(
            has_row("user_payer", &user_payer.to_string()),
            "user_payer row should contain value"
        );
        assert!(
            has_row("tenant", "my-tenant"),
            "tenant row should contain value"
        );
        assert!(
            has_row("multicast_pub", "mcast-test"),
            "multicast_pub row should contain value"
        );
        assert!(
            has_row("last_access_epoch", "200"),
            "last_access_epoch row should contain value"
        );
        assert!(
            has_row("remaining_epoch", "190"),
            "remaining_epoch row should contain value"
        );
        assert!(
            has_row("connections", "3"),
            "connections row should contain value"
        );
        assert!(
            has_row("unicast_users", "2 / 5"),
            "unicast_users row should contain count / max"
        );
        assert!(
            has_row("multicast_users", "1 / 3"),
            "multicast_users row should contain count / max"
        );
        assert!(
            has_row("status", "connected"),
            "status row should contain value"
        );
        assert!(
            has_row("owner", &accesspass.owner.to_string()),
            "owner row should contain value"
        );
    }

    fn test_exchange(code: &str) -> Exchange {
        Exchange {
            account_type: AccountType::Exchange,
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 255,
            lat: 52.37,
            lng: 4.89,
            bgp_community: 10001,
            unused: 0,
            status: ExchangeStatus::Activated,
            code: code.to_string(),
            name: code.to_string(),
            reference_count: 0,
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
        }
    }

    fn account_for<T: borsh::BorshSerialize>(value: &T) -> Option<Account> {
        Some(Account {
            data: borsh::to_vec(value).unwrap(),
            ..Account::default()
        })
    }

    /// An EdgeSeat pass with one feed, that feed carrying one group and served from one metro. The
    /// feed, its exchange and the group all resolve, so the display shows codes rather than keys.
    fn edge_seat_client(
        client_ip: Ipv4Addr,
        user_payer: Pubkey,
        accesspass_pubkey: Pubkey,
        feed_key: Pubkey,
        exchange_key: Pubkey,
        group_key: Pubkey,
    ) -> crate::doublezerocommand::MockCliCommand {
        let mut client = create_test_client();

        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 255,
            accesspass_type: AccessPassType::EdgeSeat(vec![FeedSeat {
                feed_key,
                max_users: 2,
                max_future_users: 2,
                current_users: 1,
                anniversary_day: 15,
                window_end: 100,
                terminates_at: 200,
            }]),
            client_ip,
            user_payer,
            last_access_epoch: u64::MAX,
            connection_count: 0,
            status: AccessPassStatus::Connected,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            owner: Pubkey::new_unique(),
            flags: 0,
            unicast_user_count: 0,
            max_unicast_users: 0,
            multicast_user_count: 1,
            max_multicast_users: 2,
        };

        let feed = Feed {
            account_type: AccountType::Feed,
            owner: Pubkey::new_unique(),
            bump_seed: 0,
            code: "qa-payments".to_string(),
            name: "QA Payments".to_string(),
            exchange: exchange_key,
            groups: vec![group_key],
            ..Default::default()
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
        };

        client
            .expect_get_accesspass()
            .with(predicate::eq(GetAccessPassCommand {
                client_ip,
                user_payer,
            }))
            .returning(move |_| Ok(Some((accesspass_pubkey, accesspass.clone()))));
        client
            .expect_list_multicastgroup()
            .with(predicate::eq(ListMulticastGroupCommand {}))
            .returning(move |_| {
                let mut map = HashMap::new();
                map.insert(group_key, mgroup.clone());
                Ok(map)
            });
        client
            .expect_list_tenant()
            .with(predicate::eq(ListTenantCommand {}))
            .returning(|_| Ok(HashMap::new()));
        // Two reads: the feeds the pass names, then the metros those feeds serve. Mockall matches
        // by predicate, so the two expectations are told apart by the keys asked for.
        client
            .expect_get_multiple_accounts()
            .with(predicate::eq(vec![feed_key]))
            .returning(move |_| Ok(vec![account_for(&feed)]));
        client
            .expect_get_multiple_accounts()
            .with(predicate::eq(vec![exchange_key]))
            .returning(|_| Ok(vec![account_for(&test_exchange("xams"))]));

        client
    }

    #[test]
    fn test_cli_accesspass_get_json_renders_edge_seat_feeds() {
        let client_ip = Ipv4Addr::new(10, 0, 0, 2);
        let user_payer = Pubkey::new_unique();
        let accesspass_pubkey = Pubkey::new_unique();
        let feed_key = Pubkey::new_unique();
        let exchange_key = Pubkey::new_unique();
        let group_key = Pubkey::new_unique();
        let client = edge_seat_client(
            client_ip,
            user_payer,
            accesspass_pubkey,
            feed_key,
            exchange_key,
            group_key,
        );

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            GetAccessPassCliCommand {
                client_ip,
                user_payer,
                json: true,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());

        // Lock in the JSON shape consumers depend on: an EdgeSeat pass emits a feed_seats
        // array that names each feed by key, by code and by the metro it serves, and carries its
        // users and billing windows. The code stays bare here; only the joined `feeds` row
        // qualifies it, so a consumer never has to split a code back out of a string.
        let json: serde_json::Value = serde_json::from_slice(&output).unwrap();
        let feeds = json["feed_seats"].as_array().unwrap();
        assert_eq!(feeds.len(), 1);
        assert_eq!(feeds[0]["feed_key"], feed_key.to_string());
        assert_eq!(feeds[0]["feed_code"], "qa-payments");
        assert_eq!(feeds[0]["exchange_code"], "xams");
        assert_eq!(feeds[0]["max_users"], 2);
        assert_eq!(feeds[0]["max_future_users"], 2);
        assert_eq!(feeds[0]["current_users"], 1);
        assert_eq!(feeds[0]["window_end"], 100);
        assert_eq!(feeds[0]["terminates_at"], 200);
        assert_eq!(json["feeds"], "qa-payments:xams");
        assert_eq!(json["feed_groups"], "qa-payments-group");
    }

    #[test]
    fn test_cli_accesspass_get_table_renders_edge_seat_feeds() {
        let client_ip = Ipv4Addr::new(10, 0, 0, 2);
        let user_payer = Pubkey::new_unique();
        let accesspass_pubkey = Pubkey::new_unique();
        let feed_key = Pubkey::new_unique();
        let exchange_key = Pubkey::new_unique();
        let group_key = Pubkey::new_unique();
        let client = edge_seat_client(
            client_ip,
            user_payer,
            accesspass_pubkey,
            feed_key,
            exchange_key,
            group_key,
        );

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            GetAccessPassCliCommand {
                client_ip,
                user_payer,
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
        assert_eq!(row("feeds"), "qa-payments:xams");
        assert_eq!(row("feed_groups"), "qa-payments-group");
        // The allowlists are empty on this pass, and the feed does not fill them in.
        assert_eq!(row("multicast_pub"), "");
        assert_eq!(row("multicast_sub"), "");
    }

    /// A pass that names a feed we cannot load still identifies the feed, by key.
    #[test]
    fn test_cli_accesspass_get_renders_unresolved_feed_by_key() {
        let mut client = create_test_client();

        let client_ip = Ipv4Addr::new(10, 0, 0, 3);
        let user_payer = Pubkey::new_unique();
        let accesspass_pubkey = Pubkey::new_unique();
        let feed_key = Pubkey::new_unique();

        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 255,
            accesspass_type: AccessPassType::EdgeSeat(vec![FeedSeat {
                feed_key,
                max_users: 1,
                max_future_users: 1,
                current_users: 0,
                anniversary_day: 3,
                window_end: 100,
                terminates_at: 200,
            }]),
            client_ip,
            user_payer,
            last_access_epoch: u64::MAX,
            connection_count: 0,
            status: AccessPassStatus::Connected,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            owner: Pubkey::new_unique(),
            flags: 0,
            unicast_user_count: 0,
            max_unicast_users: 0,
            multicast_user_count: 0,
            max_multicast_users: 1,
        };

        client
            .expect_get_accesspass()
            .with(predicate::eq(GetAccessPassCommand {
                client_ip,
                user_payer,
            }))
            .returning(move |_| Ok(Some((accesspass_pubkey, accesspass.clone()))));
        client
            .expect_list_multicastgroup()
            .with(predicate::eq(ListMulticastGroupCommand {}))
            .returning(|_| Ok(HashMap::new()));
        client
            .expect_list_tenant()
            .with(predicate::eq(ListTenantCommand {}))
            .returning(|_| Ok(HashMap::new()));
        client
            .expect_get_multiple_accounts()
            .with(predicate::eq(vec![feed_key]))
            .returning(|_| Ok(vec![None]));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            GetAccessPassCliCommand {
                client_ip,
                user_payer,
                json: true,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());

        let json: serde_json::Value = serde_json::from_slice(&output).unwrap();
        assert_eq!(json["feed_seats"][0]["feed_key"], feed_key.to_string());
        assert_eq!(json["feed_seats"][0]["feed_code"], feed_key.to_string());
        // No feed means no exchange to name, and the key is unique, so nothing qualifies it. Note
        // the exchange read is never made either: only a feed we could read names a metro.
        assert_eq!(json["feed_seats"][0]["exchange_code"], "");
        assert_eq!(json["feeds"], feed_key.to_string());
        assert_eq!(json["feed_groups"], "");
    }

    /// The reported bug: a pass holding one feed code in three metros holds three different feeds,
    /// and the row named all three the same. Each is now qualified by the metro it serves.
    #[test]
    fn test_cli_accesspass_get_qualifies_feeds_sharing_a_code_by_metro() {
        let mut client = create_test_client();

        let client_ip = Ipv4Addr::new(10, 0, 0, 4);
        let user_payer = Pubkey::new_unique();
        let accesspass_pubkey = Pubkey::new_unique();
        let feed_keys = [
            Pubkey::new_unique(),
            Pubkey::new_unique(),
            Pubkey::new_unique(),
        ];
        let exchange_keys = [
            Pubkey::new_unique(),
            Pubkey::new_unique(),
            Pubkey::new_unique(),
        ];

        let seat = |feed_key: Pubkey| FeedSeat {
            feed_key,
            max_users: 1,
            max_future_users: 1,
            current_users: 0,
            anniversary_day: 15,
            window_end: 100,
            terminates_at: 200,
        };
        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 255,
            accesspass_type: AccessPassType::EdgeSeat(
                feed_keys.iter().map(|key| seat(*key)).collect(),
            ),
            client_ip,
            user_payer,
            last_access_epoch: u64::MAX,
            connection_count: 0,
            status: AccessPassStatus::Connected,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            owner: Pubkey::new_unique(),
            flags: 0,
            unicast_user_count: 0,
            max_unicast_users: 0,
            multicast_user_count: 0,
            max_multicast_users: 3,
        };

        // Three separate feed accounts that happen to share a code, one per metro.
        let feeds: Vec<Feed> = exchange_keys
            .iter()
            .map(|exchange| Feed {
                account_type: AccountType::Feed,
                owner: Pubkey::new_unique(),
                bump_seed: 0,
                code: "lashay1-feed".to_string(),
                name: "Lashay 1".to_string(),
                exchange: *exchange,
                groups: vec![],
                ..Default::default()
            })
            .collect();

        client
            .expect_get_accesspass()
            .with(predicate::eq(GetAccessPassCommand {
                client_ip,
                user_payer,
            }))
            .returning(move |_| Ok(Some((accesspass_pubkey, accesspass.clone()))));
        client
            .expect_list_multicastgroup()
            .with(predicate::eq(ListMulticastGroupCommand {}))
            .returning(|_| Ok(HashMap::new()));
        client
            .expect_list_tenant()
            .with(predicate::eq(ListTenantCommand {}))
            .returning(|_| Ok(HashMap::new()));
        client
            .expect_get_multiple_accounts()
            .with(predicate::eq(feed_keys.to_vec()))
            .returning(move |_| Ok(feeds.iter().map(account_for).collect()));
        client
            .expect_get_multiple_accounts()
            .with(predicate::eq(exchange_keys.to_vec()))
            .returning(|_| {
                Ok(vec![
                    account_for(&test_exchange("xams")),
                    account_for(&test_exchange("xfra")),
                    account_for(&test_exchange("xdfw")),
                ])
            });

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            GetAccessPassCliCommand {
                client_ip,
                user_payer,
                json: true,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());

        let json: serde_json::Value = serde_json::from_slice(&output).unwrap();
        assert_eq!(
            json["feeds"],
            "lashay1-feed:xams, lashay1-feed:xfra, lashay1-feed:xdfw"
        );
        let seats = json["feed_seats"].as_array().unwrap();
        assert_eq!(seats[0]["exchange_code"], "xams");
        assert_eq!(seats[1]["exchange_code"], "xfra");
        assert_eq!(seats[2]["exchange_code"], "xdfw");
    }

    /// A feed pointing at an exchange the ledger cannot produce still has to be told apart from its
    /// same-coded siblings, so it is qualified by the exchange key rather than left bare.
    #[test]
    fn test_cli_accesspass_get_falls_back_to_the_exchange_key() {
        let mut client = create_test_client();

        let client_ip = Ipv4Addr::new(10, 0, 0, 5);
        let user_payer = Pubkey::new_unique();
        let accesspass_pubkey = Pubkey::new_unique();
        let feed_key = Pubkey::new_unique();
        let exchange_key = Pubkey::new_unique();

        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 255,
            accesspass_type: AccessPassType::EdgeSeat(vec![FeedSeat {
                feed_key,
                max_users: 1,
                max_future_users: 1,
                current_users: 0,
                anniversary_day: 15,
                window_end: 100,
                terminates_at: 200,
            }]),
            client_ip,
            user_payer,
            last_access_epoch: u64::MAX,
            connection_count: 0,
            status: AccessPassStatus::Connected,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            owner: Pubkey::new_unique(),
            flags: 0,
            unicast_user_count: 0,
            max_unicast_users: 0,
            multicast_user_count: 0,
            max_multicast_users: 1,
        };

        let feed = Feed {
            account_type: AccountType::Feed,
            owner: Pubkey::new_unique(),
            bump_seed: 0,
            code: "qa-payments".to_string(),
            name: "QA Payments".to_string(),
            exchange: exchange_key,
            groups: vec![],
            ..Default::default()
        };

        client
            .expect_get_accesspass()
            .with(predicate::eq(GetAccessPassCommand {
                client_ip,
                user_payer,
            }))
            .returning(move |_| Ok(Some((accesspass_pubkey, accesspass.clone()))));
        client
            .expect_list_multicastgroup()
            .with(predicate::eq(ListMulticastGroupCommand {}))
            .returning(|_| Ok(HashMap::new()));
        client
            .expect_list_tenant()
            .with(predicate::eq(ListTenantCommand {}))
            .returning(|_| Ok(HashMap::new()));
        client
            .expect_get_multiple_accounts()
            .with(predicate::eq(vec![feed_key]))
            .returning(move |_| Ok(vec![account_for(&feed)]));
        client
            .expect_get_multiple_accounts()
            .with(predicate::eq(vec![exchange_key]))
            .returning(|_| Ok(vec![None]));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            GetAccessPassCliCommand {
                client_ip,
                user_payer,
                json: true,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());

        let json: serde_json::Value = serde_json::from_slice(&output).unwrap();
        assert_eq!(json["feeds"], format!("qa-payments:{exchange_key}"));
        assert_eq!(
            json["feed_seats"][0]["exchange_code"],
            exchange_key.to_string()
        );
    }
}
