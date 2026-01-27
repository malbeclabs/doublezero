use crate::{
    doublezerocommand::CliCommand,
    helpers::parse_pubkey,
    poll_for_activation::{poll_for_multicastgroup_activated, poll_for_user_activated},
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::{validate_pubkey, validate_pubkey_or_code},
};
use clap::Args;
use doublezero_sdk::commands::{
    multicastgroup::{get::GetMulticastGroupCommand, subscribe::SubscribeMulticastGroupCommand},
    user::get::GetUserCommand,
};
use std::io::Write;

#[derive(Args, Debug)]
pub struct SubscribeUserCliCommand {
    /// User Pubkey to subscribe
    #[arg(long, value_parser = validate_pubkey)]
    pub user: String,
    /// Multicast group Pubkey or code to subscribe to (can be specified multiple times)
    #[arg(long = "group", value_parser = validate_pubkey_or_code, num_args = 1..)]
    pub groups: Vec<String>,
    /// Subscribe as a publisher
    #[arg(long)]
    pub publisher: bool,
    /// Subscribe as a subscriber
    #[arg(long)]
    pub subscriber: bool,
    /// Wait for the subscription to complete.
    #[arg(short, long, default_value_t = false)]
    pub wait: bool,
}

impl SubscribeUserCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let (user_pk, user) = client.get_user(GetUserCommand {
            pubkey: parse_pubkey(&self.user).ok_or_else(|| eyre::eyre!("Invalid user pubkey"))?,
        })?;

        // Resolve all group pubkeys
        let mut group_pks = Vec::new();
        for group in &self.groups {
            let group_pk = match parse_pubkey(group) {
                Some(pk) => pk,
                None => {
                    let (pubkey, _) = client
                        .get_multicastgroup(GetMulticastGroupCommand {
                            pubkey_or_code: group.to_string(),
                        })
                        .map_err(|_| eyre::eyre!("MulticastGroup not found ({})", group))?;
                    pubkey
                }
            };
            group_pks.push(group_pk);
        }

        // Subscribe to each group
        for group_pk in &group_pks {
            let signature = client.subscribe_multicastgroup(SubscribeMulticastGroupCommand {
                user_pk,
                group_pk: *group_pk,
                client_ip: user.client_ip,
                publisher: self.publisher,
                subscriber: self.subscriber,
            })?;
            writeln!(out, "Subscribed to {group_pk}: {signature}")?;
        }

        if self.wait {
            let user = poll_for_user_activated(client, &user_pk)?;
            writeln!(out, "User status: {}", user.status)?;
            for group_pk in &group_pks {
                let mgroup = poll_for_multicastgroup_activated(client, group_pk)?;
                writeln!(out, "Multicast group {group_pk} status: {}", mgroup.status)?;
            }
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
        user::subscribe::SubscribeUserCliCommand,
    };
    use doublezero_sdk::{
        commands::{
            multicastgroup::{
                get::GetMulticastGroupCommand, subscribe::SubscribeMulticastGroupCommand,
            },
            user::get::GetUserCommand,
        },
        AccountType, MulticastGroup, MulticastGroupStatus, User, UserCYOA, UserType,
    };
    use doublezero_serviceability::pda::get_user_old_pda;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_user_subscribe() {
        let mut client = create_test_client();

        let (user_pubkey, _bump_seed) = get_user_old_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let client_ip = [192, 168, 1, 100].into();
        let user = User {
            account_type: AccountType::User,
            index: 1,
            bump_seed: 255,
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            device_pk: Pubkey::new_unique(),
            owner: client.get_payer(),
            tenant_pk: Pubkey::default(),
            client_ip,
            dz_ip: client_ip,
            tunnel_id: 12345,
            tunnel_net: "192.168.1.0/24".parse().unwrap(),
            status: doublezero_sdk::UserStatus::Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
        };

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

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_user()
            .with(predicate::eq(GetUserCommand {
                pubkey: user_pubkey,
            }))
            .returning(move |_| Ok((user_pubkey, user.clone())));
        client
            .expect_get_multicastgroup()
            .with(predicate::eq(GetMulticastGroupCommand {
                pubkey_or_code: mgroup_pubkey.to_string(),
            }))
            .returning(move |_| Ok((mgroup_pubkey, mgroup.clone())));
        client
            .expect_subscribe_multicastgroup()
            .with(predicate::eq(SubscribeMulticastGroupCommand {
                user_pk: user_pubkey,
                group_pk: mgroup_pubkey,
                client_ip,
                publisher: false,
                subscriber: true,
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let res = SubscribeUserCliCommand {
            user: user_pubkey.to_string(),
            groups: vec![mgroup_pubkey.to_string()],
            publisher: false,
            subscriber: true,
            wait: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,
            format!("Subscribed to {mgroup_pubkey}: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n")
        );
    }

    #[test]
    fn test_cli_user_subscribe_multiple_groups() {
        let mut client = create_test_client();

        let (user_pubkey, _bump_seed) = get_user_old_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let client_ip = [192, 168, 1, 100].into();
        let user = User {
            account_type: AccountType::User,
            index: 1,
            bump_seed: 255,
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            device_pk: Pubkey::new_unique(),
            owner: client.get_payer(),
            tenant_pk: Pubkey::default(),
            client_ip,
            dz_ip: client_ip,
            tunnel_id: 12345,
            tunnel_net: "192.168.1.0/24".parse().unwrap(),
            status: doublezero_sdk::UserStatus::Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
        };

        let mgroup_pubkey1 = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");
        let mgroup1 = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 1,
            bump_seed: 255,
            tenant_pk: Pubkey::new_unique(),
            multicast_ip: [239, 1, 1, 1].into(),
            max_bandwidth: 1000,
            status: MulticastGroupStatus::Activated,
            code: "group1".to_string(),
            owner: mgroup_pubkey1,
            publisher_count: 0,
            subscriber_count: 0,
        };

        let mgroup_pubkey2 = Pubkey::from_str_const("11111116EPqoQskEM2Pddp8KTL9JoFhVBkC8GXfRH");
        let mgroup2 = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 2,
            bump_seed: 254,
            tenant_pk: Pubkey::new_unique(),
            multicast_ip: [239, 1, 1, 2].into(),
            max_bandwidth: 1000,
            status: MulticastGroupStatus::Activated,
            code: "group2".to_string(),
            owner: mgroup_pubkey2,
            publisher_count: 0,
            subscriber_count: 0,
        };

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_user()
            .with(predicate::eq(GetUserCommand {
                pubkey: user_pubkey,
            }))
            .returning(move |_| Ok((user_pubkey, user.clone())));
        client
            .expect_get_multicastgroup()
            .with(predicate::eq(GetMulticastGroupCommand {
                pubkey_or_code: mgroup_pubkey1.to_string(),
            }))
            .returning(move |_| Ok((mgroup_pubkey1, mgroup1.clone())));
        client
            .expect_get_multicastgroup()
            .with(predicate::eq(GetMulticastGroupCommand {
                pubkey_or_code: mgroup_pubkey2.to_string(),
            }))
            .returning(move |_| Ok((mgroup_pubkey2, mgroup2.clone())));
        client
            .expect_subscribe_multicastgroup()
            .times(2)
            .returning(move |_| Ok(signature));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let res = SubscribeUserCliCommand {
            user: user_pubkey.to_string(),
            groups: vec![mgroup_pubkey1.to_string(), mgroup_pubkey2.to_string()],
            publisher: false,
            subscriber: true,
            wait: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        let sig_str = "3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv";
        assert_eq!(
            output_str,
            format!("Subscribed to {mgroup_pubkey1}: {sig_str}\nSubscribed to {mgroup_pubkey2}: {sig_str}\n")
        );
    }
}
