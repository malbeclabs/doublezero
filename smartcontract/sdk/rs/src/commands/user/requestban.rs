use std::collections::HashSet;

use crate::{
    commands::{
        device::get::GetDeviceCommand,
        multicastgroup::{
            list::ListMulticastGroupCommand, subscribe::UpdateMulticastGroupRolesCommand,
        },
    },
    DoubleZeroClient,
};
use doublezero_serviceability::processors::user::requestban::UserRequestBanArgs;
use doublezero_serviceability_instruction::user::request_ban_user;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct RequestBanUserCommand {
    pub pubkey: Pubkey,
}

impl RequestBanUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let user = client
            .get(self.pubkey)
            .map_err(|_| eyre::eyre!("User not found ({})", self.pubkey))?
            .get_user()
            .map_err(|e| eyre::eyre!(e))?;

        let unique_mgroup_pks: Vec<Pubkey> = user
            .publishers
            .iter()
            .chain(user.subscribers.iter())
            .copied()
            .collect::<HashSet<_>>()
            .into_iter()
            .collect();
        let multicastgroups = ListMulticastGroupCommand {}.execute(client)?;
        for mgroup_pk in &unique_mgroup_pks {
            if multicastgroups.contains_key(mgroup_pk) {
                UpdateMulticastGroupRolesCommand {
                    group_pk: *mgroup_pk,
                    user_pk: self.pubkey,
                    client_ip: user.client_ip,
                    publisher: false,
                    subscriber: false,
                    device_pk: None,
                    feed_pk: None,
                }
                .execute(client)?;
            }
        }

        let (_, device) = GetDeviceCommand {
            pubkey_or_code: user.device_pk.to_string(),
        }
        .execute(client)
        .map_err(|_| eyre::eyre!("Device not found"))?;
        let dz_prefix_count = device.dz_prefixes.len();
        if dz_prefix_count == 0 {
            return Err(eyre::eyre!(
                "Device {} has no dz_prefixes; cannot request-ban user",
                user.device_pk
            ));
        }
        let dz_prefix_count_u8 = u8::try_from(dz_prefix_count).map_err(|_| {
            eyre::eyre!(
                "Device {} has {} dz_prefixes, exceeds u8::MAX",
                user.device_pk,
                dz_prefix_count
            )
        })?;

        // The builder derives globalstate + resource-extension PDAs + the dz_prefix
        // block loop.
        client.send_transaction(request_ban_user(
            &client.get_program_id(),
            &client.get_payer(),
            &self.pubkey,
            &user.device_pk,
            dz_prefix_count_u8,
            UserRequestBanArgs {
                dz_prefix_count: dz_prefix_count_u8,
                multicast_publisher_count: 1,
            },
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::user::requestban::RequestBanUserCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        processors::user::requestban::UserRequestBanArgs,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            device::Device,
            user::{User, UserCYOA, UserStatus, UserType},
        },
    };
    use doublezero_serviceability_instruction::user::request_ban_user;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::net::Ipv4Addr;

    #[test]
    fn test_request_ban_user() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();

        let user_pubkey = Pubkey::new_unique();
        let device_pk = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        let user = User {
            account_type: AccountType::User,
            owner: payer,
            bump_seed: 0,
            index: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRLWithAllocatedIP,
            device_pk,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            dz_ip: Ipv4Addr::new(10, 0, 0, 1),
            tunnel_id: 100,
            tunnel_net: "10.1.0.0/31".parse().unwrap(),
            status: UserStatus::Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            feed_pk: Pubkey::default(),
        };

        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .returning(move |_| Ok(AccountData::User(user.clone())));

        let device = Device {
            account_type: AccountType::Device,
            dz_prefixes: "10.0.0.0/24".parse().unwrap(),
            ..Default::default()
        };
        client
            .expect_get()
            .with(predicate::eq(device_pk))
            .returning(move |_| Ok(AccountData::Device(device.clone())));

        client
            .expect_gets()
            .with(predicate::eq(AccountType::MulticastGroup))
            .returning(|_| Ok(std::collections::HashMap::new()));

        // The device advertises one dz_prefix ("10.0.0.0/24"), so dz_prefix_count = 1.
        let expected = request_ban_user(
            &program_id,
            &payer,
            &user_pubkey,
            &device_pk,
            1,
            UserRequestBanArgs {
                dz_prefix_count: 1,
                multicast_publisher_count: 1,
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = RequestBanUserCommand {
            pubkey: user_pubkey,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
