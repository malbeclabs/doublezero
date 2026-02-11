use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::commands::{
    accesspass::get::GetAccessPassCommand, multicastgroup::list::ListMulticastGroupCommand,
    tenant::list::ListTenantCommand,
};
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, net::Ipv4Addr};

#[derive(Args, Debug)]
pub struct GetAccessPassCliCommand {
    /// Client IP address
    #[arg(long)]
    pub client_ip: Ipv4Addr,
    /// User payer public key
    #[arg(long)]
    pub user_payer: Pubkey,
}

impl GetAccessPassCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
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

        writeln!(out, "account: {pubkey}")?;
        writeln!(out, "type: {}", accesspass.accesspass_type)?;
        writeln!(out, "client_ip: {}", accesspass.client_ip)?;
        writeln!(out, "user_payer: {}", accesspass.user_payer)?;
        writeln!(out, "tenant: {}", tenant_display.join(", "))?;
        writeln!(out, "multicast_pub: {}", pub_display.join(", "))?;
        writeln!(out, "multicast_sub: {}", sub_display.join(", "))?;
        writeln!(out, "last_access_epoch: {last_access_epoch}")?;
        writeln!(out, "remaining_epoch: {remaining_epoch}")?;
        writeln!(out, "flags: {}", accesspass.flags_string())?;
        writeln!(out, "connections: {}", accesspass.connection_count)?;
        writeln!(out, "status: {}", accesspass.status)?;
        writeln!(out, "owner: {}", accesspass.owner)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{accesspass::get::GetAccessPassCliCommand, tests::utils::create_test_client};
    use doublezero_sdk::{
        commands::{
            accesspass::get::GetAccessPassCommand, multicastgroup::list::ListMulticastGroupCommand,
            tenant::list::ListTenantCommand,
        },
        AccountType, MulticastGroup,
    };
    use doublezero_serviceability::state::{
        accesspass::{AccessPass, AccessPassStatus, AccessPassType},
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
        };

        let mgroup_pubkey = Pubkey::new_unique();
        let mgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 1,
            bump_seed: 1,
            owner: Pubkey::new_unique(),
            tenant_pk: tenant_pubkey,
            multicast_ip: [239, 0, 0, 1].into(),
            max_bandwidth: 1000000000,
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

        let mut output = Vec::new();
        let res = GetAccessPassCliCommand {
            client_ip,
            user_payer,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains(&format!("account: {accesspass_pubkey}")));
        assert!(output_str.contains("type: prepaid"));
        assert!(output_str.contains("client_ip: 10.0.0.1"));
        assert!(output_str.contains(&format!("user_payer: {user_payer}")));
        assert!(output_str.contains("tenant: my-tenant"));
        assert!(output_str.contains("multicast_pub: mcast-test"));
        assert!(output_str.contains("last_access_epoch: 200"));
        assert!(output_str.contains("remaining_epoch: 190"));
        assert!(output_str.contains("connections: 3"));
        assert!(output_str.contains("status: connected"));
        assert!(output_str.contains(&format!("owner: {}", accesspass.owner)));
    }
}
