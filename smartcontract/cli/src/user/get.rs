use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::commands::device::get::GetDeviceCommand;
use doublezero_sdk::commands::multicastgroup::list::ListMulticastGroupCommand;
use doublezero_sdk::commands::user::get::GetUserCommand;
use doublezero_sdk::*;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use std::str::FromStr;

#[derive(Args, Debug)]
pub struct GetUserCliCommand {
    #[arg(long)]
    pub pubkey: String,
}

impl GetUserCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let pubkey = Pubkey::from_str(&self.pubkey)?;

        let user = client.get_user(GetUserCommand { pubkey });
        if let Ok((pubkey, user)) = user {
            let device = client.get_device(GetDeviceCommand {
                pubkey_or_code: user.device_pk.to_string(),
            });

            let (device_code, location_name) = match device {
                Ok((_, device)) => {
                    let location = client.get_location(GetLocationCommand {
                        pubkey_or_code: device.location_pk.to_string(),
                    });
                    let location_name = match location {
                        Ok((_, loc)) => loc.name,
                        Err(_) => device.location_pk.to_string(),
                    };

                    (device.code, location_name)
                }
                Err(_) => (user.device_pk.to_string(), "Unknown".to_string()),
            };

            let mgroups = client.list_multicastgroup(ListMulticastGroupCommand {})?;

            writeln!(out,
            "account: {}\r\nuser_type: {}\r\ndevice: {}\r\nlocation: {}\r\ncyoa_type: {}\r\nclient_ip: {}\r\ntunnel_net: {}\r\ndz_ip: {}\r\nstatus: {}\r\nowner: {}\r\n",
            pubkey,
            user.user_type,
            device_code,
            location_name,
            user.cyoa_type,
            ipv4_to_string(&user.client_ip),
            networkv4_to_string(&user.tunnel_net),
            ipv4_to_string(&user.dz_ip),
            user.status,
            user.owner
        )?;

            if user.publishers.len() > 0 {
                writeln!(out, "multicast_publishers:")?;
                for pk in user.publishers {
                    match mgroups.get(&pk) {
                        Some(mgroup) => {
                            writeln!(out, "  - {} ({})", mgroup.code, pk)?;
                        }
                        None => {
                            writeln!(out, "  - {}", pk)?;
                        }
                    };
                }
            }
            if user.subscribers.len() > 0 {
                writeln!(out, "multicast_subscribers:")?;
                for pk in user.subscribers {
                    match mgroups.get(&pk) {
                        Some(mgroup) => {
                            writeln!(out, "  - {} ({})", mgroup.code, pk)?;
                        }
                        None => {
                            writeln!(out, "  - {}", pk)?;
                        }
                    };
                }
            }
            writeln!(out)?;
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::doublezerocommand::CliCommand;
    use crate::tests::tests::create_test_client;
    use crate::user::get::GetUserCliCommand;
    use doublezero_sdk::commands::device::get::GetDeviceCommand;
    use doublezero_sdk::commands::multicastgroup::list::ListMulticastGroupCommand;
    use doublezero_sdk::commands::user::delete::DeleteUserCommand;
    use doublezero_sdk::commands::user::get::GetUserCommand;
    use doublezero_sdk::AccountType;
    use doublezero_sdk::Device;
    use doublezero_sdk::DeviceStatus;
    use doublezero_sdk::DeviceType;
    use doublezero_sdk::GetLocationCommand;
    use doublezero_sdk::Location;
    use doublezero_sdk::LocationStatus;
    use doublezero_sdk::MulticastGroup;
    use doublezero_sdk::User;
    use doublezero_sdk::UserCYOA;
    use doublezero_sdk::UserStatus;
    use doublezero_sdk::UserType;
    use doublezero_sla_program::pda::get_user_pda;
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_cli_user_get() {
        let mut client = create_test_client();

        let (user_pk, _bump_seed) = get_user_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let location_pk = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo2");
        let location = Location {
            code: "test_location".to_string(),
            name: "Test Location".to_string(),
            owner: user_pk,
            bump_seed: 255,
            account_type: AccountType::Location,
            index: 1,
            lat: 1.0,
            lng: 1.0,
            loc_id: 0,
            status: LocationStatus::Activated,
            country: "US".to_string(),
        };
        client
            .expect_get_location()
            .with(predicate::eq(GetLocationCommand {
                pubkey_or_code: location_pk.to_string(),
            }))
            .returning(move |_| Ok((location_pk, location.clone())));

        let device_pk = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo1");
        let device = Device {
            code: "test_device".to_string(),
            location_pk: location_pk,
            owner: user_pk,
            bump_seed: 255,
            account_type: AccountType::Device,
            index: 1,
            device_type: DeviceType::Switch,
            exchange_pk: Pubkey::default(),
            public_ip: [10, 0, 0, 1],
            status: DeviceStatus::Activated,
            dz_prefixes: vec![([10, 0, 0, 2], 24)],
        };
        client
            .expect_get_device()
            .with(predicate::eq(GetDeviceCommand {
                pubkey_or_code: device_pk.to_string(),
            }))
            .returning(move |_| Ok((device_pk, device.clone())));

        let mgroup_pk = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo3");
        let mgroup = MulticastGroup {
            code: "test_mgroup".to_string(),
            owner: Pubkey::default(),
            bump_seed: 255,
            account_type: AccountType::MulticastGroup,
            index: 1,
            status: doublezero_sdk::MulticastGroupStatus::Activated,
            tenant_pk: Pubkey::default(),
            multicast_ip: [239, 1, 1, 1],
            max_bandwidth: 1000,
            pub_allowlist: vec![client.get_payer()],
            sub_allowlist: vec![client.get_payer()],
            publishers: vec![user_pk.clone()],
            subscribers: vec![],
        };

        client
            .expect_list_multicastgroup()
            .with(predicate::eq(ListMulticastGroupCommand {}))
            .returning(move |_| {
                let mut map = std::collections::HashMap::new();
                map.insert(mgroup_pk, mgroup.clone());
                Ok(map)
            });

        let user = User {
            account_type: AccountType::User,
            index: 1,
            bump_seed: 255,
            user_type: UserType::IBRL,
            tenant_pk: Pubkey::default(),
            cyoa_type: UserCYOA::GREOverDIA,
            device_pk: device_pk.clone(),
            client_ip: [10, 0, 0, 1],
            dz_ip: [10, 0, 0, 2],
            tunnel_id: 0,
            tunnel_net: ([10, 2, 3, 4], 24),
            status: UserStatus::Activated,
            owner: user_pk,
            publishers: vec![mgroup_pk],
            subscribers: vec![],
        };

        client
            .expect_get_user()
            .with(predicate::eq(GetUserCommand { pubkey: user_pk }))
            .returning(move |_| Ok((user_pk, user.clone())));

        client
            .expect_delete_user()
            .with(predicate::eq(DeleteUserCommand { index: 1 }))
            .returning(move |_| Ok(signature));

        /*****************************************************************************************************/
        // Expected success
        let mut output = Vec::new();
        let res = GetUserCliCommand {
            pubkey: user_pk.to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by code");
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "account: CJTXjCEbDDgQoccJgEbNGc63QwWzJtdAoSio36zVXHQw\r\nuser_type: IBRL\r\ndevice: test_device\r\nlocation: Test Location\r\ncyoa_type: GREOverDIA\r\nclient_ip: 10.0.0.1\r\ntunnel_net: 10.2.3.4/24\r\ndz_ip: 10.0.0.2\r\nstatus: activated\r\nowner: CJTXjCEbDDgQoccJgEbNGc63QwWzJtdAoSio36zVXHQw\r\n\nmulticast_publishers:\n  - test_mgroup (11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo3)\n\n");
    }
}
