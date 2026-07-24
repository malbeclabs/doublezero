use crate::{
    doublezerocommand::CliCommand,
    helpers::parse_pubkey,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::{validate_pubkey, validate_pubkey_or_code},
};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_sdk::commands::{
    multicastgroup::{get::GetMulticastGroupCommand, subscribe::UpdateMulticastGroupRolesCommand},
    user::get::GetUserCommand,
};
use std::io::Write;

#[derive(Args, Debug)]
pub struct SubscribeUserCliCommand {
    /// User Pubkey to subscribe
    #[arg(long, value_parser = validate_pubkey)]
    pub user: String,
    /// Multicast group Pubkey or code to update (can be specified multiple times)
    #[arg(long = "group", value_parser = validate_pubkey_or_code, num_args = 1..)]
    pub groups: Vec<String>,
    /// Add (`--publisher` or `--publisher true`) or remove (`--publisher false`) the
    /// publisher role. When omitted, the current publisher role is left unchanged.
    #[arg(long, num_args = 0..=1, default_missing_value = "true")]
    pub publisher: Option<bool>,
    /// Add (`--subscriber` or `--subscriber true`) or remove (`--subscriber false`) the
    /// subscriber role. When omitted, the current subscriber role is left unchanged.
    #[arg(long, num_args = 0..=1, default_missing_value = "true")]
    pub subscriber: Option<bool>,
    /// Wait for the subscription to complete.
    #[arg(short, long, default_value_t = false)]
    pub wait: bool,
}

impl SubscribeUserCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        // Require at least one role flag so we never issue a silent no-op. Note
        // this only rejects the both-omitted case: re-asserting a role the user
        // already holds (e.g. `--publisher true` on a current publisher) still
        // issues a transaction the processor treats as a no-op, which costs a
        // signature/credits.
        if self.publisher.is_none() && self.subscriber.is_none() {
            eyre::bail!(
                "Specify at least one of --publisher[=true|false] or --subscriber[=true|false]"
            );
        }

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

        // Update roles for each group. An omitted flag preserves the user's
        // current role for that group; the processor sets absolute state
        // (idempotent add when true, idempotent remove when false), not a
        // relative toggle.
        //
        // Preserving an already-held role re-asserts it as `true`, which the
        // processor re-checks against the current onchain allowlist before the
        // idempotent add/remove. If that allowlist drifted since the user
        // subscribed, an unrelated role removal can be rejected with NotAllowed.
        // This is an inherited processor property, not a regression here.
        for group_pk in &group_pks {
            let publisher = self
                .publisher
                .unwrap_or_else(|| user.publishers.contains(group_pk));
            let subscriber = self
                .subscriber
                .unwrap_or_else(|| user.subscribers.contains(group_pk));
            let signature =
                client.update_multicastgroup_roles(UpdateMulticastGroupRolesCommand {
                    user_pk,
                    group_pk: *group_pk,
                    client_ip: user.client_ip,
                    publisher,
                    subscriber,
                    device_pk: None,
                    feed_pk: None,
                })?;
            writeln!(out, "Updated roles for {group_pk}: {signature}")?;
        }

        if self.wait {
            let (_, user) = client.get_user(GetUserCommand { pubkey: user_pk })?;
            writeln!(out, "User status: {}", user.status)?;
            for group_pk in &group_pks {
                let (_, mgroup) = client.get_multicastgroup(GetMulticastGroupCommand {
                    pubkey_or_code: group_pk.to_string(),
                })?;
                writeln!(out, "Multicast group {group_pk} status: {}", mgroup.status)?;
            }
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};

    use crate::{
        doublezerocommand::CliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
        user::subscribe::SubscribeUserCliCommand,
    };
    use doublezero_sdk::{
        commands::{
            multicastgroup::{
                get::GetMulticastGroupCommand, subscribe::UpdateMulticastGroupRolesCommand,
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
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            feed_pks: vec![],
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
            .expect_update_multicastgroup_roles()
            .with(predicate::eq(UpdateMulticastGroupRolesCommand {
                user_pk: user_pubkey,
                group_pk: mgroup_pubkey,
                client_ip,
                publisher: false,
                subscriber: true,
                device_pk: None,
                feed_pk: None,
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let ctx = cli_context_default_for_tests();
        let res = block_on(
            SubscribeUserCliCommand {
                user: user_pubkey.to_string(),
                groups: vec![mgroup_pubkey.to_string()],
                publisher: Some(false),
                subscriber: Some(true),
                wait: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        let sig_str = signature.to_string();
        assert_eq!(
            output_str,
            format!("Updated roles for {mgroup_pubkey}: {sig_str}\n")
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
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            feed_pks: vec![],
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
            .expect_update_multicastgroup_roles()
            .times(2)
            .returning(move |_| Ok(signature));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let ctx = cli_context_default_for_tests();
        let res = block_on(
            SubscribeUserCliCommand {
                user: user_pubkey.to_string(),
                groups: vec![mgroup_pubkey1.to_string(), mgroup_pubkey2.to_string()],
                publisher: Some(false),
                subscriber: Some(true),
                wait: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        let sig_str = signature.to_string();
        assert_eq!(
            output_str,
            format!("Updated roles for {mgroup_pubkey1}: {sig_str}\nUpdated roles for {mgroup_pubkey2}: {sig_str}\n")
        );
    }

    #[test]
    fn test_cli_user_unsubscribe_publisher_preserves_subscriber() {
        let mut client = create_test_client();

        let (user_pubkey, _bump_seed) = get_user_old_pda(&client.get_program_id(), 1);
        let signature = Signature::new_unique();
        let client_ip = [192, 168, 1, 100].into();

        let mgroup_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");

        // User is currently both a publisher and subscriber of the group.
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
            publishers: vec![mgroup_pubkey],
            subscribers: vec![mgroup_pubkey],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            feed_pks: vec![],
        };

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
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
            publisher_count: 1,
            subscriber_count: 1,
        };

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
        // Removing only the publisher role must preserve the existing subscriber role.
        client
            .expect_update_multicastgroup_roles()
            .with(predicate::eq(UpdateMulticastGroupRolesCommand {
                user_pk: user_pubkey,
                group_pk: mgroup_pubkey,
                client_ip,
                publisher: false,
                subscriber: true,
                device_pk: None,
                feed_pk: None,
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let ctx = cli_context_default_for_tests();
        let res = block_on(
            SubscribeUserCliCommand {
                user: user_pubkey.to_string(),
                groups: vec![mgroup_pubkey.to_string()],
                publisher: Some(false),
                subscriber: None,
                wait: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,
            format!("Updated roles for {mgroup_pubkey}: {signature}\n")
        );
    }

    #[test]
    fn test_cli_user_unsubscribe_subscriber_preserves_publisher() {
        let mut client = create_test_client();

        let (user_pubkey, _bump_seed) = get_user_old_pda(&client.get_program_id(), 1);
        let signature = Signature::new_unique();
        let client_ip = [192, 168, 1, 100].into();

        let mgroup_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");

        // User is currently both a publisher and subscriber of the group.
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
            publishers: vec![mgroup_pubkey],
            subscribers: vec![mgroup_pubkey],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            feed_pks: vec![],
        };

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
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
            publisher_count: 1,
            subscriber_count: 1,
        };

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
        // Removing only the subscriber role must preserve the existing publisher role.
        client
            .expect_update_multicastgroup_roles()
            .with(predicate::eq(UpdateMulticastGroupRolesCommand {
                user_pk: user_pubkey,
                group_pk: mgroup_pubkey,
                client_ip,
                publisher: true,
                subscriber: false,
                device_pk: None,
                feed_pk: None,
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let ctx = cli_context_default_for_tests();
        let res = block_on(
            SubscribeUserCliCommand {
                user: user_pubkey.to_string(),
                groups: vec![mgroup_pubkey.to_string()],
                publisher: None,
                subscriber: Some(false),
                wait: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,
            format!("Updated roles for {mgroup_pubkey}: {signature}\n")
        );
    }

    #[test]
    fn test_cli_user_subscribe_requires_role_flag() {
        let mut client = create_test_client();

        let (user_pubkey, _bump_seed) = get_user_old_pda(&client.get_program_id(), 1);
        let mgroup_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));

        let mut output = Vec::new();
        let ctx = cli_context_default_for_tests();
        let res = block_on(
            SubscribeUserCliCommand {
                user: user_pubkey.to_string(),
                groups: vec![mgroup_pubkey.to_string()],
                publisher: None,
                subscriber: None,
                wait: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        let err = res.unwrap_err().to_string();
        assert!(
            err.contains("Specify at least one of --publisher"),
            "expected role-flag guidance, got: {err}"
        );
    }

    #[test]
    fn test_cli_user_subscribe_parses_role_flags() {
        use clap::Parser;

        #[derive(Parser, Debug)]
        struct TestCli {
            #[command(subcommand)]
            command: TestCommand,
        }

        #[derive(clap::Subcommand, Debug)]
        enum TestCommand {
            Subscribe(SubscribeUserCliCommand),
        }

        let user_pk = Pubkey::new_unique().to_string();
        let g1 = Pubkey::new_unique().to_string();
        let g2 = Pubkey::new_unique().to_string();

        let parse = |args: Vec<&str>| -> SubscribeUserCliCommand {
            let TestCli {
                command: TestCommand::Subscribe(cmd),
            } = TestCli::try_parse_from(args).expect("parse should succeed");
            cmd
        };

        // Bare `--publisher` means true.
        let cmd = parse(vec![
            "test",
            "subscribe",
            "--user",
            &user_pk,
            "--group",
            &g1,
            "--publisher",
        ]);
        assert_eq!(cmd.publisher, Some(true));
        assert_eq!(cmd.subscriber, None);

        // Explicit `--publisher false` means false.
        let cmd = parse(vec![
            "test",
            "subscribe",
            "--user",
            &user_pk,
            "--group",
            &g1,
            "--publisher",
            "false",
        ]);
        assert_eq!(cmd.publisher, Some(false));

        // Variadic `--group g1 g2` followed by a bare `--publisher` parses
        // unambiguously: both groups land in `groups`, not as publisher's value.
        let cmd = parse(vec![
            "test",
            "subscribe",
            "--user",
            &user_pk,
            "--group",
            &g1,
            &g2,
            "--publisher",
        ]);
        assert_eq!(cmd.groups, vec![g1, g2]);
        assert_eq!(cmd.publisher, Some(true));
    }
}
