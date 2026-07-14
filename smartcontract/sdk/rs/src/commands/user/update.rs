use crate::{
    commands::{device::get::GetDeviceCommand, user::get::GetUserCommand},
    DoubleZeroClient,
};
use doublezero_program_common::types::NetworkV4;
use doublezero_serviceability::{
    processors::user::update::UserUpdateArgs,
    state::user::{UserCYOA, UserType},
};
use doublezero_serviceability_instruction::user::update_user;
use solana_sdk::{pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateUserCommand {
    pub pubkey: Pubkey,
    pub user_type: Option<UserType>,
    pub cyoa_type: Option<UserCYOA>,
    pub dz_ip: Option<Ipv4Addr>,
    pub tunnel_id: Option<u16>,
    pub tunnel_net: Option<NetworkV4>,
    pub validator_pubkey: Option<Pubkey>,
    pub tenant_pk: Option<Pubkey>,
    pub tunnel_endpoint: Option<Ipv4Addr>,
}

impl UpdateUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        // UpdateUser always requires the user's resource-extension accounts so the
        // bitmaps stay in sync, even when only updating non-resource fields.
        let (_user_pubkey, user) = GetUserCommand {
            pubkey: self.pubkey,
        }
        .execute(client)?;

        let (_, device) = GetDeviceCommand {
            pubkey_or_code: user.device_pk.to_string(),
        }
        .execute(client)
        .map_err(|_| eyre::eyre!("Device not found"))?;

        let count = device.dz_prefixes.len();
        if count == 0 {
            return Err(eyre::eyre!(
                "Device {} has no dz_prefixes; cannot update user",
                user.device_pk
            ));
        }
        let dz_prefix_count = u8::try_from(count).map_err(|_| {
            eyre::eyre!(
                "Device {} has {} dz_prefixes, exceeds u8::MAX",
                user.device_pk,
                count
            )
        })?;
        let multicast_publisher_count = 1u8;

        client.send_transaction(update_user(
            &client.get_program_id(),
            &client.get_payer(),
            &self.pubkey,
            &user.device_pk,
            dz_prefix_count,
            &user.tenant_pk,
            UserUpdateArgs {
                user_type: self.user_type,
                cyoa_type: self.cyoa_type,
                dz_ip: self.dz_ip,
                tunnel_id: self.tunnel_id,
                tunnel_net: self.tunnel_net,
                validator_pubkey: self.validator_pubkey,
                tenant_pk: self.tenant_pk,
                dz_prefix_count,
                multicast_publisher_count,
                tunnel_endpoint: self.tunnel_endpoint,
            },
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::user::update::UpdateUserCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        processors::user::update::UserUpdateArgs,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            device::Device,
            user::{User, UserCYOA, UserStatus, UserType},
        },
    };
    use doublezero_serviceability_instruction::user::update_user;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::net::Ipv4Addr;

    #[test]
    fn test_commands_user_update_with_resource_fields() {
        let mut client = create_test_client();

        let payer = client.get_payer();
        let program_id = client.get_program_id();

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
            tunnel_id: 500,
            tunnel_net: "169.254.0.0/31".parse().unwrap(),
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

        // user.tenant_pk is default and tenant_pk is None, so no tenant accounts.
        let expected = update_user(
            &program_id,
            &payer,
            &user_pubkey,
            &device_pk,
            1,
            &Pubkey::default(),
            UserUpdateArgs {
                user_type: None,
                cyoa_type: None,
                dz_ip: None,
                tunnel_id: Some(501),
                tunnel_net: None,
                validator_pubkey: None,
                tenant_pk: None,
                dz_prefix_count: 1,
                multicast_publisher_count: 1,
                tunnel_endpoint: None,
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = UpdateUserCommand {
            pubkey: user_pubkey,
            user_type: None,
            cyoa_type: None,
            dz_ip: None,
            tunnel_id: Some(501),
            tunnel_net: None,
            validator_pubkey: None,
            tenant_pk: None,
            tunnel_endpoint: None,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    // Note: `test_commands_user_update_no_resource_fields_skips_accounts` was
    // removed — the SDK no longer has a "skip resource accounts" path. UpdateUser
    // always parses the ResourceExtension accounts so the bitmaps stay in sync,
    // even when only updating non-resource fields. The resource-fields path is
    // covered by `test_commands_user_update_with_resource_fields` above.
}
