use crate::{doublezerocommand::CliCommand, validators::validate_pubkey};
use clap::Args;
use doublezero_sdk::commands::{
    accesspass::get::GetAccessPassCommand, device::list::ListDeviceCommand,
    multicastgroup::list::ListMulticastGroupCommand, tenant::list::ListTenantCommand,
    user::get::GetUserCommand,
};
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, str::FromStr};

#[derive(Args, Debug)]
pub struct GetUserCliCommand {
    /// User Pubkey to retrieve
    #[arg(long, value_parser = validate_pubkey)]
    pub pubkey: String,
}

impl GetUserCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let pubkey = Pubkey::from_str(&self.pubkey)?;
        let (pubkey, user) = client.get_user(GetUserCommand { pubkey })?;

        let (_, accesspass) = client
            .get_accesspass(GetAccessPassCommand {
                client_ip: user.client_ip,
                user_payer: user.owner,
            })?
            .ok_or_else(|| eyre::eyre!("Access Pass not found"))?;
        let multicast_groups = client.list_multicastgroup(ListMulticastGroupCommand {})?;
        let tenants = client.list_tenant(ListTenantCommand {})?;
        let devices = client.list_device(ListDeviceCommand {})?;

        writeln!(
            out,
            "account: {}\r\n\
        user_type: {}\r\n\
        tenant: {}\r\n\
        device: {}\r\n\
        cyoa_type: {}\r\n\
        client_ip: {}\r\n\
        tunnel_net: {}\r\n\
        dz_ip: {}\r\n\
        accesspass: {}\r\n\
        publishers: {}\r\n\
        subscribers: {}\r\n\
        status: {}\r\n\
        owner: {}",
            pubkey,
            user.user_type,
            tenants
                .get(&user.tenant_pk)
                .map_or(user.tenant_pk.to_string(), |t| t.code.clone()),
            devices
                .get(&user.device_pk)
                .map_or(user.device_pk.to_string(), |d| d.code.clone()),
            user.cyoa_type,
            &user.client_ip,
            &user.tunnel_net,
            &user.dz_ip,
            accesspass,
            user.publishers
                .iter()
                .map(|pk| multicast_groups
                    .get(pk)
                    .map_or(pk.to_string(), |mg| mg.code.clone()))
                .collect::<Vec<_>>()
                .join(", "),
            user.subscribers
                .iter()
                .map(|pk| multicast_groups
                    .get(pk)
                    .map_or(pk.to_string(), |mg| mg.code.clone()))
                .collect::<Vec<_>>()
                .join(", "),
            user.status,
            user.owner
        )?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
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
        AccountType, Device, MulticastGroup, User, UserCYOA, UserStatus, UserType,
    };
    use doublezero_serviceability::{
        pda::{get_accesspass_pda, get_user_old_pda},
        state::{
            accesspass::{AccessPass, AccessPassStatus, AccessPassType},
            device::{DeviceDesiredStatus, DeviceHealth, DeviceStatus, DeviceType},
            tenant::{Tenant, TenantBillingConfig, TenantPaymentStatus},
        },
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

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
            multicast_users_count: 0,
            max_unicast_users: 1000,
            max_multicast_users: 1000,
            reserved_seats: 0,
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
        client
            .expect_get_accesspass()
            .with(predicate::eq(accesspass::get::GetAccessPassCommand {
                client_ip: user.client_ip,
                user_payer: user.owner,
            }))
            .returning(move |_| Ok(Some((accesspass_pubkey, accesspass.clone()))));

        client
            .expect_get_user()
            .with(predicate::eq(GetUserCommand { pubkey: pda_pubkey }))
            .returning(move |_| Ok((pda_pubkey, user.clone())));

        client
            .expect_delete_user()
            .with(predicate::eq(DeleteUserCommand { pubkey: pda_pubkey }))
            .returning(move |_| Ok(signature));

        /*****************************************************************************************************/
        // Expected success
        let mut output = Vec::new();
        let res = GetUserCliCommand {
            pubkey: pda_pubkey.to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by code");
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "account: CwpwPjV6LsVxHQ1Ye5bizyrXSa9j2Gk5C6y3WyMyYaA1\r\nuser_type: IBRL\r\ntenant: test-tenant\r\ndevice: test-device\r\ncyoa_type: GREOverDIA\r\nclient_ip: 10.0.0.1\r\ntunnel_net: 10.2.3.4/24\r\ndz_ip: 10.0.0.2\r\naccesspass: Prepaid: (expires epoch 10)\r\npublishers: \r\nsubscribers: test\r\nstatus: activated\r\nowner: CwpwPjV6LsVxHQ1Ye5bizyrXSa9j2Gk5C6y3WyMyYaA1\n");
    }
}
