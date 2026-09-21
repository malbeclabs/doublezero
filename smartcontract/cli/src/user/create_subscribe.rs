use crate::{
    doublezerocommand::CliCommand,
    helpers::parse_pubkey,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::validate_pubkey_or_code,
};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_sdk::{
    commands::{
        device::get::GetDeviceCommand,
        feed::get::GetFeedCommand,
        multicastgroup::get::GetMulticastGroupCommand,
        user::{create_subscribe::CreateSubscribeUserCommand, get::GetUserCommand},
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
    /// Custom owner pubkey (foundation allowlist only)
    #[arg(long)]
    pub owner: Option<String>,
    /// Feed pubkey or code for an EdgeSeat feed pass.
    #[arg(long)]
    pub feed: Option<String>,
}

impl CreateSubscribeUserCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
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

        let owner_pk = self
            .owner
            .as_deref()
            .map(|s| parse_pubkey(s).ok_or_else(|| eyre::eyre!("Invalid owner pubkey: {}", s)))
            .transpose()?;

        let feed_pk = match self.feed {
            None => None,
            Some(feed) => match parse_pubkey(&feed) {
                Some(pk) => Some(pk),
                None => {
                    let (pubkey, _) = client
                        .get_feed(GetFeedCommand {
                            pubkey_or_code: feed.clone(),
                            exchange: None,
                        })
                        .map_err(|e| eyre::eyre!("Feed lookup failed ({feed}): {e}"))?;
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
            mgroup_pks: vec![publisher_pk
                .or(subscriber_pk)
                .ok_or(eyre::eyre!("Subscriber is required if publisher is not"))?],
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            owner: owner_pk,
            feed_pk,
            ip_proof: None,
        })?;
        writeln!(out, "Signature: {signature}",)?;

        if self.wait {
            let (_, user) = client.get_user(GetUserCommand { pubkey })?;
            writeln!(out, "Status: {0}", user.status)?;
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    /// A fixed short (< 43 chars) base58 pubkey, so a `--subscriber` argument deterministically
    /// takes the code-resolution path: `Pubkey::new_unique()` straddles `parse_pubkey`'s length
    /// threshold depending on the process-global counter, which makes the run order matter.
    fn short_mgroup_pubkey() -> solana_sdk::pubkey::Pubkey {
        solana_sdk::pubkey::Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo")
    }

    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};

    use crate::{
        doublezerocommand::CliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
        user::create_subscribe::CreateSubscribeUserCliCommand,
    };
    use doublezero_sdk::{
        commands::{
            device::get::GetDeviceCommand, feed::get::GetFeedCommand,
            multicastgroup::get::GetMulticastGroupCommand,
            user::create_subscribe::CreateSubscribeUserCommand,
        },
        AccountType, Device, DeviceStatus, DeviceType, Feed, MulticastGroup, MulticastGroupStatus,
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
            multicast_subscribers_count: 0,
            max_unicast_users: 0,
            max_multicast_subscribers: 0,
            reserved_seats: 0,
            multicast_publishers_count: 0,
            max_multicast_publishers: 0,
            ..Default::default()
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
                mgroup_pks: vec![mgroup_pubkey],
                tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
                owner: None,
                feed_pk: None,
                ip_proof: None,
            }))
            .times(1)
            .returning(move |_| Ok((signature, pda_pubkey)));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let ctx = cli_context_default_for_tests();
        let res = block_on(
            CreateSubscribeUserCliCommand {
                device: "device1".to_string(),
                client_ip: [100, 0, 0, 1].into(),
                allocate_addr: false,
                publisher: None,
                subscriber: Some(mgroup_pubkey.to_string()),
                wait: false,
                owner: None,
                feed: None,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }

    // A pubkey `--feed` is passed straight through as the trailing Feed account for an
    // EdgeSeat feed pass.
    #[test]
    fn test_cli_user_create_subscribe_with_feed_pubkey() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_user_old_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let device_pubkey = Pubkey::new_unique();
        let device = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "device1".to_string(),
            contributor_pk: Pubkey::default(),
            location_pk: Pubkey::default(),
            exchange_pk: Pubkey::default(),
            device_type: DeviceType::Hybrid,
            public_ip: [10, 0, 0, 1].into(),
            dz_prefixes: "10.0.0.1/24".parse().unwrap(),
            owner: device_pubkey,
            metrics_publisher_pk: Pubkey::default(),
            status: DeviceStatus::Activated,
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 255,
            users_count: 0,
            device_health: doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
            desired_status:
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
            unicast_users_count: 0,
            multicast_subscribers_count: 0,
            max_unicast_users: 0,
            max_multicast_subscribers: 0,
            reserved_seats: 0,
            multicast_publishers_count: 0,
            max_multicast_publishers: 0,
            ..Default::default()
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
            .times(1)
            .returning(move |_| Ok((device_pubkey, device.clone())));

        let mgroup_pubkey = short_mgroup_pubkey();
        let mgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            owner: Pubkey::default(),
            index: 1,
            bump_seed: 1,
            tenant_pk: Pubkey::default(),
            code: "mg1".to_string(),
            multicast_ip: [239, 0, 0, 1].into(),
            max_bandwidth: 1_000_000_000,
            status: MulticastGroupStatus::Activated,
            publisher_count: 0,
            subscriber_count: 0,
        };
        client
            .expect_get_multicastgroup()
            .with(predicate::eq(GetMulticastGroupCommand {
                pubkey_or_code: mgroup_pubkey.to_string(),
            }))
            .times(1)
            .returning(move |_| Ok((mgroup_pubkey, mgroup.clone())));

        // A full-length pubkey (43-44 chars) so parse_pubkey takes the pubkey path
        // and passes it straight through, without a get_feed registry lookup.
        let feed_pubkey = Pubkey::from_str_const("7CTniUa88iJKUHTrCkB4TjAoG6TD7AMivhQeuqN2LPtX");
        client
            .expect_create_subscribe_user()
            .with(predicate::eq(CreateSubscribeUserCommand {
                user_type: UserType::Multicast,
                device_pk: device_pubkey,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: [100, 0, 0, 1].into(),
                publisher: false,
                subscriber: true,
                mgroup_pks: vec![mgroup_pubkey],
                tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
                owner: None,
                feed_pk: Some(feed_pubkey),
                ip_proof: None,
            }))
            .times(1)
            .returning(move |_| Ok((signature, pda_pubkey)));

        let mut output = Vec::new();
        let ctx = cli_context_default_for_tests();
        let res = block_on(
            CreateSubscribeUserCliCommand {
                device: "device1".to_string(),
                client_ip: [100, 0, 0, 1].into(),
                allocate_addr: false,
                publisher: None,
                subscriber: Some(mgroup_pubkey.to_string()),
                wait: false,
                owner: None,
                feed: Some(feed_pubkey.to_string()),
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());
    }

    // A bare `--feed` code (not a pubkey) is resolved through `get_feed` with exchange
    // None, and the resolved pubkey is passed as the trailing Feed account.
    #[test]
    fn test_cli_user_create_subscribe_with_feed_code() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_user_old_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let device_pubkey = Pubkey::new_unique();
        let device = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "device1".to_string(),
            contributor_pk: Pubkey::default(),
            location_pk: Pubkey::default(),
            exchange_pk: Pubkey::default(),
            device_type: DeviceType::Hybrid,
            public_ip: [10, 0, 0, 1].into(),
            dz_prefixes: "10.0.0.1/24".parse().unwrap(),
            owner: device_pubkey,
            metrics_publisher_pk: Pubkey::default(),
            status: DeviceStatus::Activated,
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 255,
            users_count: 0,
            device_health: doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
            desired_status:
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
            unicast_users_count: 0,
            multicast_subscribers_count: 0,
            max_unicast_users: 0,
            max_multicast_subscribers: 0,
            reserved_seats: 0,
            multicast_publishers_count: 0,
            max_multicast_publishers: 0,
            ..Default::default()
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
            .times(1)
            .returning(move |_| Ok((device_pubkey, device.clone())));

        let mgroup_pubkey = short_mgroup_pubkey();
        let mgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            owner: Pubkey::default(),
            index: 1,
            bump_seed: 1,
            tenant_pk: Pubkey::default(),
            code: "mg1".to_string(),
            multicast_ip: [239, 0, 0, 1].into(),
            max_bandwidth: 1_000_000_000,
            status: MulticastGroupStatus::Activated,
            publisher_count: 0,
            subscriber_count: 0,
        };
        client
            .expect_get_multicastgroup()
            .with(predicate::eq(GetMulticastGroupCommand {
                pubkey_or_code: mgroup_pubkey.to_string(),
            }))
            .times(1)
            .returning(move |_| Ok((mgroup_pubkey, mgroup.clone())));

        // "shreds-nyc" is not a pubkey, so parse_pubkey returns None and the CLI resolves
        // it through get_feed (exchange None), then passes the returned pubkey through.
        let resolved_feed_pubkey = Pubkey::new_unique();
        let feed = Feed {
            account_type: AccountType::Feed,
            owner: Pubkey::new_unique(),
            bump_seed: 255,
            code: "shreds-nyc".to_string(),
            name: "Shreds NYC".to_string(),
            exchange: Pubkey::new_unique(),
            groups: vec![],
            ..Default::default()
        };
        client
            .expect_get_feed()
            .with(predicate::eq(GetFeedCommand {
                pubkey_or_code: "shreds-nyc".to_string(),
                exchange: None,
            }))
            .times(1)
            .returning(move |_| Ok((resolved_feed_pubkey, feed.clone())));

        client
            .expect_create_subscribe_user()
            .with(predicate::eq(CreateSubscribeUserCommand {
                user_type: UserType::Multicast,
                device_pk: device_pubkey,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: [100, 0, 0, 1].into(),
                publisher: false,
                subscriber: true,
                mgroup_pks: vec![mgroup_pubkey],
                tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
                owner: None,
                feed_pk: Some(resolved_feed_pubkey),
                ip_proof: None,
            }))
            .times(1)
            .returning(move |_| Ok((signature, pda_pubkey)));

        let mut output = Vec::new();
        let ctx = cli_context_default_for_tests();
        let res = block_on(
            CreateSubscribeUserCliCommand {
                device: "device1".to_string(),
                client_ip: [100, 0, 0, 1].into(),
                allocate_addr: false,
                publisher: None,
                subscriber: Some(mgroup_pubkey.to_string()),
                wait: false,
                owner: None,
                feed: Some("shreds-nyc".to_string()),
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok(), "{res:?}");
    }

    // A get_feed failure (e.g. an ambiguous code spanning metros) is surfaced with the
    // underlying cause, not collapsed into a flat "not found" that hides the remedy.
    #[test]
    fn test_cli_user_create_subscribe_surfaces_feed_lookup_error() {
        let mut client = create_test_client();

        let device_pubkey = Pubkey::new_unique();
        let device = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "device1".to_string(),
            contributor_pk: Pubkey::default(),
            location_pk: Pubkey::default(),
            exchange_pk: Pubkey::default(),
            device_type: DeviceType::Hybrid,
            public_ip: [10, 0, 0, 1].into(),
            dz_prefixes: "10.0.0.1/24".parse().unwrap(),
            owner: device_pubkey,
            metrics_publisher_pk: Pubkey::default(),
            status: DeviceStatus::Activated,
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 255,
            users_count: 0,
            device_health: doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
            desired_status:
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
            unicast_users_count: 0,
            multicast_subscribers_count: 0,
            max_unicast_users: 0,
            max_multicast_subscribers: 0,
            reserved_seats: 0,
            multicast_publishers_count: 0,
            max_multicast_publishers: 0,
            ..Default::default()
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
            .times(1)
            .returning(move |_| Ok((device_pubkey, device.clone())));

        let mgroup_pubkey = short_mgroup_pubkey();
        let mgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            owner: Pubkey::default(),
            index: 1,
            bump_seed: 1,
            tenant_pk: Pubkey::default(),
            code: "mg1".to_string(),
            multicast_ip: [239, 0, 0, 1].into(),
            max_bandwidth: 1_000_000_000,
            status: MulticastGroupStatus::Activated,
            publisher_count: 0,
            subscriber_count: 0,
        };
        client
            .expect_get_multicastgroup()
            .with(predicate::eq(GetMulticastGroupCommand {
                pubkey_or_code: mgroup_pubkey.to_string(),
            }))
            .times(1)
            .returning(move |_| Ok((mgroup_pubkey, mgroup.clone())));

        // The SDK rejects an ambiguous code; the CLI must pass that cause through, not
        // mask it. create_subscribe_user is never reached, so it is not expected.
        client
            .expect_get_feed()
            .with(predicate::eq(GetFeedCommand {
                pubkey_or_code: "shreds-nyc".to_string(),
                exchange: None,
            }))
            .times(1)
            .returning(|_| {
                Err(eyre::eyre!(
                    "Feed code shreds-nyc is ambiguous: it exists in 2 metros"
                ))
            });

        let mut output = Vec::new();
        let ctx = cli_context_default_for_tests();
        let res = block_on(
            CreateSubscribeUserCliCommand {
                device: "device1".to_string(),
                client_ip: [100, 0, 0, 1].into(),
                allocate_addr: false,
                publisher: None,
                subscriber: Some(mgroup_pubkey.to_string()),
                wait: false,
                owner: None,
                feed: Some("shreds-nyc".to_string()),
            }
            .execute(&ctx, &client, &mut output),
        );
        let message = format!(
            "{}",
            res.expect_err("a feed lookup failure must surface as an error")
        );
        assert!(
            message.contains("shreds-nyc"),
            "error names the feed: {message}"
        );
        assert!(
            message.contains("ambiguous"),
            "error preserves the underlying cause: {message}"
        );
    }
}
