use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_sdk::commands::{
    accesspass::get::GetAccessPassCommand, multicastgroup::list::ListMulticastGroupCommand,
    tenant::list::ListTenantCommand,
};
use doublezero_serviceability::state::accesspass::{AccessPassType, FeedSeat};
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

#[derive(Tabled, Serialize)]
struct AccessPassDisplay {
    pub account: String,
    #[tabled(rename = "type")]
    #[serde(rename = "type")]
    pub accesspass_type: String,
    #[tabled(skip)]
    pub feed_seats: Vec<FeedSeat>,
    pub client_ip: String,
    pub user_payer: String,
    pub tenant: String,
    pub multicast_pub: String,
    pub multicast_sub: String,
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
            feed_seats: match &accesspass.accesspass_type {
                AccessPassType::EdgeSeat(seats) => seats.clone(),
                _ => Vec::new(),
            },
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
        AccountType, MulticastGroup,
    };
    use doublezero_serviceability::state::{
        accesspass::{AccessPass, AccessPassStatus, AccessPassType, FeedSeat},
        tenant::{Tenant, TenantBillingConfig, TenantPaymentStatus},
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;
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

    #[test]
    fn test_cli_accesspass_get_json_renders_edge_seat_feeds() {
        let mut client = create_test_client();

        let client_ip = Ipv4Addr::new(10, 0, 0, 2);
        let user_payer = Pubkey::new_unique();
        let accesspass_pubkey = Pubkey::new_unique();
        let feed_key = Pubkey::new_unique();

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
            .returning(|_| Ok(HashMap::new()));
        client
            .expect_list_tenant()
            .with(predicate::eq(ListTenantCommand {}))
            .returning(|_| Ok(HashMap::new()));

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
        // array with each seat's per-feed users and billing windows.
        let json: serde_json::Value = serde_json::from_slice(&output).unwrap();
        let feeds = json["feed_seats"].as_array().unwrap();
        assert_eq!(feeds.len(), 1);
        assert_eq!(feeds[0]["feed_key"], feed_key.to_string());
        assert_eq!(feeds[0]["max_users"], 2);
        assert_eq!(feeds[0]["max_future_users"], 2);
        assert_eq!(feeds[0]["current_users"], 1);
        assert_eq!(feeds[0]["window_end"], 100);
        assert_eq!(feeds[0]["terminates_at"], 200);
    }
}
