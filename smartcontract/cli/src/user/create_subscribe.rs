use crate::{
    doublezerocommand::CliCommand,
    helpers::parse_pubkey,
    poll_for_activation::poll_for_user_activated,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::validate_pubkey_or_code,
};
use clap::Args;
use doublezero_sdk::{
    commands::{
        device::get::GetDeviceCommand, multicastgroup::get::GetMulticastGroupCommand,
        user::create_subscribe::CreateSubscribeUserCommand,
    },
    *,
};
use std::{io::Write, net::Ipv4Addr};

#[derive(Args, Debug)]
pub struct CreateSubscribeUserCliCommand {
    /// Device Pubkey or code to associate with the user
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub device: String,
    /// Client IP address in IPv4 format
    #[arg(long)]
    pub client_ip: Ipv4Addr,
    /// Allocate a new address for the user
    #[arg(short, long, default_value_t = false)]
    pub allocate_addr: bool,
    /// Multicast group publisher Pubkey or code
    #[arg(long)]
    pub publisher: Option<String>,
    /// Multicast group subscriber Pubkey or code
    #[arg(long)]
    pub subscriber: Option<String>,
    /// Wait for the user to be activated
    #[arg(short, long, default_value_t = false)]
    pub wait: bool,
}

impl CreateSubscribeUserCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let device_pk = match parse_pubkey(&self.device) {
            Some(pk) => pk,
            None => {
                let (pubkey, _) = client
                    .get_device(GetDeviceCommand {
                        pubkey_or_code: self.device.clone(),
                    })
                    .map_err(|_| eyre::eyre!("Device not found"))?;
                pubkey
            }
        };

        let publisher_pk = match self.publisher {
            None => None,
            Some(publisher) => match parse_pubkey(&publisher) {
                Some(pk) => Some(pk),
                None => {
                    let (pubkey, _) = client
                        .get_multicastgroup(GetMulticastGroupCommand {
                            pubkey_or_code: publisher.to_string(),
                        })
                        .map_err(|_| eyre::eyre!("MulticastGroup not found {}", publisher))?;
                    Some(pubkey)
                }
            },
        };

        let subscriber_pk = match self.subscriber {
            None => None,
            Some(subscriber) => match parse_pubkey(&subscriber) {
                Some(pk) => Some(pk),
                None => {
                    let (pubkey, _) = client
                        .get_multicastgroup(GetMulticastGroupCommand {
                            pubkey_or_code: subscriber.to_string(),
                        })
                        .map_err(|_| eyre::eyre!("MulticastGroup not found ({})", subscriber))?;
                    Some(pubkey)
                }
            },
        };

        let (signature, pubkey) = client.create_subscribe_user(CreateSubscribeUserCommand {
            user_type: UserType::Multicast,
            device_pk,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: self.client_ip,
            publisher: publisher_pk.is_some(),
            subscriber: subscriber_pk.is_some(),
            mgroup_pk: publisher_pk
                .or(subscriber_pk)
                .ok_or(eyre::eyre!("Subscriber is required if publisher is not"))?,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        })?;
        writeln!(out, "Signature: {signature}",)?;

        if self.wait {
            let user = poll_for_user_activated(client, &pubkey)?;
            writeln!(out, "Status: {0}", user.status)?;
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        doublezerocommand::CliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
        user::create_subscribe::CreateSubscribeUserCliCommand,
    };
    use doublezero_sdk::{
        commands::{
            device::get::GetDeviceCommand, multicastgroup::get::GetMulticastGroupCommand,
            user::create_subscribe::CreateSubscribeUserCommand,
        },
        AccountType, Device, DeviceStatus, DeviceType, MulticastGroup, MulticastGroupStatus,
        UserCYOA, UserType,
    };
    use doublezero_serviceability::pda::get_user_old_pda;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::net::Ipv4Addr;

    #[test]
    fn test_cli_user_create_subscribe() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_user_old_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);
        let mgroup_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");
        let mgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 1,
            bump_seed: 255,
            tenant_pk: Pubkey::new_unique(),
            multicast_ip: [239, 1, 1, 1].into(),
            max_bandwidth: 1000,
            status: MulticastGroupStatus::Activated,
            code: "test".to_string(),
            owner: mgroup_pubkey,
            publisher_count: 0,
            subscriber_count: 0,
        };

        let contributor_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let device_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");
        let device = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "device1".to_string(),
            contributor_pk,
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Hybrid,
            public_ip: [10, 0, 0, 1].into(),
            dz_prefixes: "10.0.0.1/24,11.0.0.1/24".parse().unwrap(),
            owner: device_pubkey,
            metrics_publisher_pk: Pubkey::new_unique(),
            status: DeviceStatus::Activated,
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 255,
            users_count: 0,
            device_health: doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
            desired_status:
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
            unicast_users_count: 0,
            multicast_users_count: 0,
            max_unicast_users: 0,
            max_multicast_users: 0,
        };

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_device()
            .with(predicate::eq(GetDeviceCommand {
                pubkey_or_code: "device1".to_string(),
            }))
            .returning(move |_| Ok((device_pubkey, device.clone())));
        client
            .expect_get_multicastgroup()
            .with(predicate::eq(GetMulticastGroupCommand {
                pubkey_or_code: mgroup_pubkey.to_string(),
            }))
            .returning(move |_| Ok((mgroup_pubkey, mgroup.clone())));
        client
            .expect_create_subscribe_user()
            .with(predicate::eq(CreateSubscribeUserCommand {
                user_type: UserType::Multicast,
                device_pk: device_pubkey,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: [100, 0, 0, 1].into(),
                publisher: false,
                subscriber: true,
                mgroup_pk: mgroup_pubkey,
                tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            }))
            .times(1)
            .returning(move |_| Ok((signature, pda_pubkey)));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let res = CreateSubscribeUserCliCommand {
            device: "device1".to_string(),
            client_ip: [100, 0, 0, 1].into(),
            allocate_addr: false,
            publisher: None,
            subscriber: Some(mgroup_pubkey.to_string()),
            wait: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }
}
