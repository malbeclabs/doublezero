use super::helpers::look_for_ip;
use crate::servicecontroller::{ProvisioningRequest, ServiceController};
use clap::{Args, Subcommand, ValueEnum};
use doublezero_cli::{
    doublezerocommand::CliCommand,
    helpers::init_command,
    requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON, CHECK_USER_ALLOWLIST},
};
use doublezero_sdk::commands::{
    device::get::GetDeviceCommand, device::list::ListDeviceCommand,
    globalconfig::get::GetGlobalConfigCommand, multicastgroup::list::ListMulticastGroupCommand,
    multicastgroup::subscribe::SubscribeMulticastGroupCommand, user::create::CreateUserCommand,
    user::create_subscribe::CreateSubscribeUserCommand, user::get::GetUserCommand,
    user::list::ListUserCommand,
};
use doublezero_sdk::{
    ipv4_to_string, networkv4_to_string, Device, IpV4, NetworkV4, User, UserCYOA, UserStatus,
    UserType,
};

use eyre;
use indicatif::ProgressBar;
use std::str::FromStr;

use solana_sdk::pubkey::Pubkey;

use crate::requirements::check_doublezero;

#[derive(Clone, Debug, ValueEnum)]
pub enum MulticastMode {
    Publisher,
    Subscriber,
}

#[allow(clippy::upper_case_acronyms)]
#[derive(Clone, Debug, Subcommand)]
pub enum DzMode {
    IBRL {
        #[arg(short, long, default_value_t = false)]
        allocate_addr: bool,
    },
    //EdgeFiltering,
    Multicast {
        #[arg(value_enum)]
        mode: MulticastMode,

        #[arg()]
        multicast_group: String,
    },
}

#[derive(Args, Debug)]
pub struct ProvisioningCliCommand {
    #[clap(subcommand)]
    pub dz_mode: DzMode,

    #[arg(long, global = true)]
    pub client_ip: Option<String>,

    #[arg(long, global = true)]
    pub device: Option<String>,

    #[arg(short, long, global = true, default_value_t = false)]
    pub verbose: bool,
}

impl ProvisioningCliCommand {
    pub async fn execute(&self, client: &dyn CliCommand) -> eyre::Result<()> {
        let spinner = init_command();
        let controller = ServiceController::new(None);

        // Check requirements
        check_requirements(
            client,
            Some(&spinner),
            CHECK_ID_JSON | CHECK_BALANCE | CHECK_USER_ALLOWLIST,
        )?;

        check_doublezero(Some(&spinner))?;

        spinner.println("🔗  Start Provisioning User...");
        spinner.set_prefix("1/4 Public IP");

        // Get public IP
        let (client_ip, client_ip_str) = look_for_ip(&self.client_ip, &spinner).await?;

        spinner.println(format!("🔍  Provisioning User for IP: {}", client_ip_str));

        let (user_type, multicast_mode, multicast_group) = self.parse_dz_mode();

        match user_type {
            UserType::IBRL | UserType::IBRLWithAllocatedIP => {
                return self
                    .execute_ibrl(client, controller, user_type, client_ip, spinner)
                    .await;
            }
            UserType::EdgeFiltering => Err(eyre::eyre!("DzMode not supported")),
            UserType::Multicast => {
                return self
                    .execute_multicast(
                        client,
                        controller,
                        multicast_mode.unwrap(),
                        multicast_group.unwrap(),
                        client_ip,
                        spinner,
                    )
                    .await;
            }
        }
    }

    async fn execute_ibrl(
        &self,
        client: &dyn CliCommand,
        controller: ServiceController,
        user_type: UserType,
        client_ip: IpV4,
        spinner: ProgressBar,
    ) -> eyre::Result<()> {
        // Look for user
        let (user_pubkey, user) = self
            .find_or_create_user(client, &controller, &client_ip, &spinner, user_type)
            .await?;

        // Check user status
        match user.status {
            UserStatus::Activated => {
                // User is activated
                self.user_activated(
                    client,
                    &controller,
                    &user,
                    &client_ip,
                    &spinner,
                    user_type,
                    None,
                    None,
                )
                .await?
            }
            UserStatus::Rejected => {
                // User is rejected
                self.user_rejected(client, &user_pubkey, &spinner).await?;
            }
            _ => panic!("User status not expected"),
        }

        spinner.finish_with_message("Connected");

        // Finish
        Ok(())
    }

    async fn execute_multicast(
        &self,
        client: &dyn CliCommand,
        controller: ServiceController,
        multicast_mode: &MulticastMode,
        multicast_group: &String,
        client_ip: IpV4,
        spinner: ProgressBar,
    ) -> eyre::Result<()> {
        let mcast_groups = client.list_multicastgroup(ListMulticastGroupCommand {})?;
        let (mcast_group_pk, _) = mcast_groups
            .iter()
            .find(|(_, g)| g.code == *multicast_group)
            .expect("Multicast group not found");

        // Look for user
        let (user_pubkey, user) = self
            .find_or_create_user_and_subscribe(
                client,
                &controller,
                &client_ip,
                &spinner,
                multicast_mode,
                mcast_group_pk,
            )
            .await?;

        let mcast_pub_groups = user
            .publishers
            .iter()
            .map(|pk| ipv4_to_string(&mcast_groups.get(pk).unwrap().multicast_ip))
            .collect::<Vec<_>>();
        let mcast_sub_groups = user
            .subscribers
            .iter()
            .map(|pk| ipv4_to_string(&mcast_groups.get(pk).unwrap().multicast_ip))
            .collect::<Vec<_>>();

        // Check user status
        match user.status {
            UserStatus::Activated => {
                // User is activated
                self.user_activated(
                    client,
                    &controller,
                    &user,
                    &client_ip,
                    &spinner,
                    UserType::Multicast,
                    Some(mcast_pub_groups),
                    Some(mcast_sub_groups),
                )
                .await?
            }
            UserStatus::Rejected => {
                // User is rejected
                self.user_rejected(client, &user_pubkey, &spinner).await?;
            }
            _ => panic!("User status not expected"),
        }

        spinner.finish_with_message("Connected");

        // Finish
        Ok(())
    }

    fn parse_dz_mode(&self) -> (UserType, Option<&MulticastMode>, Option<&String>) {
        match &self.dz_mode {
            DzMode::IBRL { allocate_addr } => {
                if *allocate_addr {
                    (UserType::IBRLWithAllocatedIP, None, None)
                } else {
                    (UserType::IBRL, None, None)
                }
            } //DzMode::EdgeFiltering => UserType::EdgeFiltering,
            DzMode::Multicast {
                mode,
                multicast_group,
            } => (UserType::Multicast, Some(mode), Some(multicast_group)),
        }
    }

    async fn find_or_create_device(
        &self,
        client: &dyn CliCommand,
        controller: &ServiceController,
        spinner: &ProgressBar,
    ) -> eyre::Result<(Pubkey, Device)> {
        spinner.set_message("Searching for device account...");

        let devices = client.list_device(ListDeviceCommand {})?;
        let device_pk = match self.device.as_ref() {
            Some(device) => match device.parse::<Pubkey>() {
                Ok(pubkey) => pubkey,
                Err(_) => {
                    let (pubkey, _) = devices
                        .iter()
                        .find(|(_, d)| d.code == *device)
                        .expect("Device not found");
                    *pubkey
                }
            },
            None => {
                spinner.set_message("Reading latency stats...");
                let mut latencies = controller.latency().await.expect("Could not get latency");
                latencies.retain(|l| l.reachable);
                latencies.sort_by(|a, b| a.avg_latency_ns.cmp(&b.avg_latency_ns));

                spinner.set_message("Searching for device account...");
                Pubkey::from_str(&latencies.first().expect("No devices found").device_pk)
                    .expect("Unable to parse pubkey")
            }
        };

        let (_, device) = client
            .get_device(GetDeviceCommand {
                pubkey_or_code: device_pk.to_string(),
            })
            .expect("Unable to get device");

        Ok((device_pk, device))
    }

    async fn find_or_create_user(
        &self,
        client: &dyn CliCommand,
        controller: &ServiceController,
        client_ip: &IpV4,
        spinner: &ProgressBar,
        user_type: UserType,
    ) -> eyre::Result<(Pubkey, User)> {
        spinner.set_message("Searching for user account...");
        spinner.set_prefix("2/4 User");

        let users = client.list_user(ListUserCommand {})?;
        let filter_func: fn(&User, &IpV4) -> bool = |user, client_ip| {
            (user.user_type == UserType::IBRL || user.user_type == UserType::IBRLWithAllocatedIP)
                && user.client_ip == *client_ip
        };

        let user_pubkey = match users.iter().find(|(_, u)| filter_func(u, client_ip)) {
            Some((pubkey, _user)) => {
                spinner.println(format!("    An account already exists Pubkey: {}", pubkey));

                *pubkey
            }
            None => {
                spinner.println(format!(
                    "    Creating an account for the IP: {}",
                    ipv4_to_string(client_ip)
                ));

                let (device_pk, device) = self
                    .find_or_create_device(client, controller, spinner)
                    .await?;

                spinner.println(format!(
                    "    The Device has been selected: {} ",
                    device.code
                ));
                spinner.set_prefix("🔗 [3/4] User");

                let res = client.create_user(CreateUserCommand {
                    user_type,
                    device_pk,
                    cyoa_type: UserCYOA::GREOverDIA,
                    client_ip: *client_ip,
                });

                match res {
                    Ok((_, pubkey)) => {
                        spinner.set_message("User created");
                        Ok(pubkey)
                    }
                    Err(e) => {
                        spinner.finish_with_message("Error creating user");
                        spinner.println(format!("\n{}: {:?}\n", "Error", e));

                        Err(eyre::eyre!("Error creating user"))
                    }
                }
            }?,
        };

        let user = self.poll_for_user_activated(client, &user_pubkey, spinner)?;

        Ok((user_pubkey, user))
    }

    async fn find_or_create_user_and_subscribe(
        &self,
        client: &dyn CliCommand,
        controller: &ServiceController,
        client_ip: &IpV4,
        spinner: &ProgressBar,
        multicast_mode: &MulticastMode,
        mcast_group_pk: &Pubkey,
    ) -> eyre::Result<(Pubkey, User)> {
        spinner.set_message("Searching for user account...");
        spinner.set_prefix("2/4 User");

        let users = client.list_user(ListUserCommand {})?;
        let filter_func: fn(&User, &IpV4) -> bool =
            |user, client_ip| user.user_type == UserType::Multicast && user.client_ip == *client_ip;

        let user_pubkey = match users.iter().find(|(_, u)| filter_func(u, client_ip)) {
            Some((pubkey, user)) => {
                spinner.println(format!("    An account already exists Pubkey: {}", pubkey));
                spinner.set_prefix("🔗 [3/4] Subscribing");

                let (publisher, subscriber) = match multicast_mode {
                    MulticastMode::Publisher => (true, user.subscribers.contains(mcast_group_pk)),
                    MulticastMode::Subscriber => (user.publishers.contains(mcast_group_pk), true),
                };

                let res = client.subscribe_multicastgroup(SubscribeMulticastGroupCommand {
                    group_pk: *mcast_group_pk,
                    user_pk: *pubkey,
                    publisher,
                    subscriber,
                });
                match res {
                    Ok(_) => {
                        spinner.set_message("User subscribed");
                        Ok(*pubkey)
                    }
                    Err(e) => {
                        spinner.finish_with_message("Error subscribing user");
                        spinner.println(format!("\n{}: {:?}\n", "Error", e));

                        Err(eyre::eyre!("Error subscribing user"))
                    }
                }
            }?,
            None => {
                spinner.println(format!(
                    "    Creating an account for the IP: {}",
                    ipv4_to_string(client_ip)
                ));

                let (device_pk, device) = self
                    .find_or_create_device(client, controller, spinner)
                    .await?;

                spinner.println(format!(
                    "    The Device has been selected: {} ",
                    device.code
                ));
                spinner.set_prefix("🔗 [3/4] User");

                let (publisher, subscriber) = match multicast_mode {
                    MulticastMode::Publisher => (true, false),
                    MulticastMode::Subscriber => (false, true),
                };

                let res = client.create_subscribe_user(CreateSubscribeUserCommand {
                    user_type: UserType::Multicast,
                    device_pk,
                    cyoa_type: UserCYOA::GREOverDIA,
                    client_ip: *client_ip,
                    mgroup_pk: *mcast_group_pk,
                    publisher,
                    subscriber,
                });

                match res {
                    Ok((_, pubkey)) => {
                        spinner.set_message("User created");
                        Ok(pubkey)
                    }
                    Err(e) => {
                        spinner.finish_with_message("Error creating user");
                        spinner.println(format!("\n{}: {:?}\n", "Error", e));
                        Err(eyre::eyre!("Error creating user"))
                    }
                }
            }?,
        };

        let user = self.poll_for_user_activated(client, &user_pubkey, spinner)?;

        Ok((user_pubkey, user))
    }

    fn poll_for_user_activated(
        &self,
        client: &dyn CliCommand,
        user_pubkey: &Pubkey,
        spinner: &ProgressBar,
    ) -> eyre::Result<User> {
        spinner.set_message("Waiting for user activation...");
        loop {
            std::thread::sleep(std::time::Duration::from_secs(5));
            let (_, user) = client
                .get_user(GetUserCommand {
                    pubkey: *user_pubkey,
                })
                .expect("User not found");

            if user.status == UserStatus::Activated || user.status == UserStatus::Rejected {
                spinner.println(format!(
                    "    User activated with dz_ip: {}",
                    ipv4_to_string(&user.dz_ip)
                ));
                return Ok(user);
            }
        }
    }

    #[allow(clippy::too_many_arguments)]
    async fn user_activated(
        &self,
        client: &dyn CliCommand,
        controller: &ServiceController,
        user: &User,
        client_ip: &IpV4,
        spinner: &ProgressBar,
        user_type: UserType,
        mcast_pub_groups: Option<Vec<String>>,
        mcast_sub_groups: Option<Vec<String>>,
    ) -> eyre::Result<()> {
        spinner.println(format!(
            "    User activated with dz_ip: {}",
            ipv4_to_string(&user.dz_ip)
        ));

        spinner.set_prefix("3/4 Device");
        spinner.set_message("Reading devices...");

        let devices = client.list_device(ListDeviceCommand {})?;
        let prefixes = devices
            .values()
            .flat_map(|device| device.dz_prefixes.clone())
            .collect::<Vec<NetworkV4>>();

        spinner.set_message("Getting global-config...");
        let (_, config) = client
            .get_globalconfig(GetGlobalConfigCommand {})
            .expect("Unable to get config");

        spinner.set_message("Getting user account...");
        let device = devices.get(&user.device_pk).expect("Device not found");

        spinner.set_prefix("4/4 Provisioning");

        // Tunnel provisioning
        let tunnel_src = ipv4_to_string(&user.client_ip);
        let tunnel_dst = ipv4_to_string(&device.public_ip);
        let tunnel_net = networkv4_to_string(&user.tunnel_net);
        let doublezero_ip = ipv4_to_string(&user.dz_ip);
        let doublezero_prefixes: Vec<String> = prefixes
            .into_iter()
            .map(|net| networkv4_to_string(&net))
            .collect();

        if self.verbose {
            spinner.println(format!(
                "➤   Provisioning Local Tunnel for IP: {}\n\ttunnel_src: {}\n\ttunnel_dst: {}\n\ttunnel_net: {}\n\tdoublezero_ip: {}\n\tdoublezero_prefixes: {:?}\n\tlocal_asn: {}\n\tremote_asn: {}\n\tmcast_pub_groups: {:?}\n\tmcast_sub_groups: {:?}\n",
                ipv4_to_string(client_ip),
                tunnel_src,
                tunnel_dst,
                tunnel_net,
                doublezero_ip,
                doublezero_prefixes,
                config.local_asn,
                config.remote_asn,
                mcast_pub_groups.clone().unwrap_or_default(),
                mcast_sub_groups.clone().unwrap_or_default(),
            ));
        };

        spinner.set_message("Provisioning DoubleZero service...");
        match controller
            .provisioning(ProvisioningRequest {
                tunnel_src,
                tunnel_dst,
                tunnel_net,
                doublezero_ip,
                doublezero_prefixes,
                bgp_local_asn: Some(config.local_asn),
                bgp_remote_asn: Some(config.remote_asn),
                user_type: user_type.to_string(),
                mcast_pub_groups,
                mcast_sub_groups,
            })
            .await
        {
            Ok(res) => {
                spinner.println(format!("Provisioning: status: {}", res.status));
                spinner.finish_with_message("User Provisioned");
            }
            Err(e) => {
                spinner.finish_with_message("Error provisioning user");
                spinner.println(format!("\n{}: {:?}\n", "Error", e));
            }
        };

        Ok(())
    }

    async fn user_rejected(
        &self,
        client: &dyn CliCommand,
        user_pubkey: &Pubkey,
        spinner: &ProgressBar,
    ) -> eyre::Result<()> {
        spinner.println(format!("    {}", "User rejected"));

        spinner.set_message("Reading logs...");
        std::thread::sleep(std::time::Duration::from_secs(10));
        let msgs = client.get_logs(user_pubkey).expect("Unable to get logs");

        for mut msg in msgs {
            if msg.starts_with("Program log: Error: ") {
                spinner.println(format!("    {}", msg.split_off(20)));
            }
        }

        Ok(())
    }
}
