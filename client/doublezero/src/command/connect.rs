use super::helpers::look_for_ip;
use crate::{
    dzd_latency::{best_latency, retrieve_latencies, select_tunnel_endpoint},
    requirements::check_doublezero,
    routes::resolve_route,
    servicecontroller::{
        ProvisioningRequest, ResolveRouteRequest, ServiceController, ServiceControllerImpl,
    },
};
use backon::{BlockingRetryable, ExponentialBuilder};
use clap::{Args, Subcommand, ValueEnum};
use doublezero_cli::{
    doublezerocommand::CliCommand,
    helpers::{init_command, parse_pubkey},
    requirements::{check_accesspass, check_requirements, CHECK_BALANCE, CHECK_ID_JSON},
};
use doublezero_program_common::types::NetworkV4;
use doublezero_sdk::{
    commands::{
        device::{get::GetDeviceCommand, list::ListDeviceCommand},
        globalconfig::get::GetGlobalConfigCommand,
        multicastgroup::{
            list::ListMulticastGroupCommand, subscribe::SubscribeMulticastGroupCommand,
        },
        tenant::get::GetTenantCommand,
        user::{
            create::CreateUserCommand, create_subscribe::CreateSubscribeUserCommand,
            get::GetUserCommand, list::ListUserCommand,
        },
    },
    Device, User, UserCYOA, UserStatus, UserType,
};
use eyre;
use indicatif::ProgressBar;
use solana_sdk::pubkey::Pubkey;
use std::{collections::HashMap, net::Ipv4Addr, str::FromStr, time::Duration};

#[derive(Clone, Debug, PartialEq, ValueEnum)]
pub enum MulticastMode {
    Publisher,
    Subscriber,
}

#[allow(clippy::upper_case_acronyms)]
#[derive(Clone, Debug, Subcommand)]
pub enum DzMode {
    /// Provision a user in IBRL mode
    IBRL {
        /// provide tenant code or pubkey
        tenant: Option<String>,
        /// Allocate a new address for the user
        #[arg(short, long, default_value_t = false)]
        allocate_addr: bool,
    },
    //EdgeFiltering,
    /// Provision a user in Multicast mode
    Multicast {
        /// (Legacy) Multicast mode: Publisher or Subscriber
        #[arg(value_enum)]
        mode: Option<MulticastMode>,

        /// (Legacy) Multicast group code(s)
        #[arg(num_args = 0..)]
        multicast_groups: Vec<String>,

        /// Multicast groups to publish to
        #[arg(long, num_args = 1..)]
        pub_groups: Vec<String>,

        /// Multicast groups to subscribe to
        #[arg(long, num_args = 1..)]
        sub_groups: Vec<String>,
    },
}

#[derive(Args, Debug)]
pub struct ProvisioningCliCommand {
    #[clap(subcommand)]
    pub dz_mode: DzMode,

    /// Client IP address in IPv4 format
    #[arg(long, global = true)]
    pub client_ip: Option<String>,

    /// Device Pubkey or code to associate with the user
    #[arg(long, global = true)]
    pub device: Option<String>,

    /// Verbose output
    #[arg(short, long, global = true, default_value_t = false)]
    pub verbose: bool,
}

enum ParsedDzMode {
    Ibrl(UserType, Option<String>),
    Multicast {
        pub_groups: Vec<String>,
        sub_groups: Vec<String>,
    },
}

impl ProvisioningCliCommand {
    pub async fn execute(&self, client: &dyn CliCommand) -> eyre::Result<()> {
        let controller = ServiceControllerImpl::new(None);
        self.execute_with_service_controller(client, &controller)
            .await
    }

    pub async fn execute_with_service_controller<T: ServiceController>(
        &self,
        client: &dyn CliCommand,
        controller: &T,
    ) -> eyre::Result<()> {
        let spinner = init_command(5);

        // Check requirements
        check_requirements(client, Some(&spinner), CHECK_ID_JSON | CHECK_BALANCE)?;
        check_doublezero(controller, client, Some(&spinner)).await?;

        spinner.println(format!(
            "🔗  Start Provisioning User to {}...",
            client.get_environment()
        ));

        // Get public IP
        let (client_ip, client_ip_str) = look_for_ip(&self.client_ip, &spinner).await?;

        if !check_accesspass(client, client_ip)? {
            println!(
                "❌  Unable to find a valid AccessPass for the IP: {client_ip_str} UserPayer: {}",
                client.get_payer()
            );
            return Err(eyre::eyre!(
                "A valid AccessPass is required to connect. Please contact support to obtain one."
            ));
        }

        spinner.inc(1);
        spinner.println(format!("    DoubleZero ID: {}", client.get_payer()));
        spinner.println(format!("🔍  Provisioning User for IP: {client_ip_str}"));

        match self.parse_dz_mode()? {
            ParsedDzMode::Ibrl(user_type, tenant) => {
                self.execute_ibrl(client, controller, user_type, client_ip, tenant, &spinner)
                    .await
            }
            ParsedDzMode::Multicast {
                pub_groups,
                sub_groups,
            } => {
                self.execute_multicast(
                    client,
                    controller,
                    &pub_groups,
                    &sub_groups,
                    client_ip,
                    &spinner,
                )
                .await
            }
        }?;

        spinner.println("✅  User Provisioned");
        spinner.finish_and_clear();

        Ok(())
    }

    async fn execute_ibrl<T: ServiceController>(
        &self,
        client: &dyn CliCommand,
        controller: &T,
        user_type: UserType,
        client_ip: Ipv4Addr,
        tenant: Option<String>,
        spinner: &ProgressBar,
    ) -> eyre::Result<()> {
        // Look for user
        let (user_pubkey, user, tunnel_src) = self
            .find_or_create_user(client, controller, &client_ip, spinner, user_type, tenant)
            .await?;

        // Check user status
        match user.status {
            UserStatus::Activated => {
                // User is activated
                self.user_activated(
                    client,
                    controller,
                    &user,
                    &tunnel_src,
                    spinner,
                    user_type,
                    None,
                    None,
                )
                .await
            }
            UserStatus::Rejected => {
                // User is rejected
                self.user_rejected(client, &user_pubkey, spinner).await
            }
            _ => eyre::bail!("User status not expected"),
        }
    }

    async fn execute_multicast<T: ServiceController>(
        &self,
        client: &dyn CliCommand,
        controller: &T,
        pub_groups: &[String],
        sub_groups: &[String],
        client_ip: Ipv4Addr,
        spinner: &ProgressBar,
    ) -> eyre::Result<()> {
        let mcast_groups = client.list_multicastgroup(ListMulticastGroupCommand)?;

        // Resolve pub group codes to pubkeys
        let mut pub_group_pks = Vec::new();
        for group_code in pub_groups {
            let (pk, _) = mcast_groups
                .iter()
                .find(|(_, g)| g.code == *group_code)
                .ok_or_else(|| eyre::eyre!("Multicast group not found: {}", group_code))?;
            if pub_group_pks.contains(pk) {
                eyre::bail!("Duplicate multicast pub group: {}", group_code);
            }
            pub_group_pks.push(*pk);
        }

        // Resolve sub group codes to pubkeys
        let mut sub_group_pks = Vec::new();
        for group_code in sub_groups {
            let (pk, _) = mcast_groups
                .iter()
                .find(|(_, g)| g.code == *group_code)
                .ok_or_else(|| eyre::eyre!("Multicast group not found: {}", group_code))?;
            if sub_group_pks.contains(pk) {
                eyre::bail!("Duplicate multicast sub group: {}", group_code);
            }
            sub_group_pks.push(*pk);
        }

        // Look for user and subscribe to all groups
        let (user_pubkey, user) = self
            .find_or_create_user_and_subscribe(
                client,
                controller,
                &client_ip,
                spinner,
                &pub_group_pks,
                &sub_group_pks,
            )
            .await?;

        let mcast_pub_groups = user
            .publishers
            .iter()
            .map(|pk| {
                mcast_groups
                    .get(pk)
                    .map(|group| group.multicast_ip.to_string())
                    .ok_or_else(|| eyre::eyre!("Missing multicast group for publisher: {}", pk))
            })
            .collect::<Result<Vec<_>, _>>()?;

        let mcast_sub_groups = user
            .subscribers
            .iter()
            .map(|pk| {
                mcast_groups
                    .get(pk)
                    .map(|group| group.multicast_ip.to_string())
                    .ok_or_else(|| eyre::eyre!("Missing multicast group for subscriber: {}", pk))
            })
            .collect::<Result<Vec<_>, _>>()?;

        let tunnel_src = if user.has_allocated_dz_ip() {
            let devices = client.list_device(ListDeviceCommand)?;
            let device = devices
                .get(&user.device_pk)
                .ok_or_else(|| eyre::eyre!("Device not found for user"))?;
            resolve_tunnel_src(controller, device).await?
        } else {
            client_ip
        };

        match user.status {
            UserStatus::Activated => {
                self.user_activated(
                    client,
                    controller,
                    &user,
                    &tunnel_src,
                    spinner,
                    user.user_type,
                    Some(mcast_pub_groups),
                    Some(mcast_sub_groups),
                )
                .await?
            }
            UserStatus::Rejected => {
                self.user_rejected(client, &user_pubkey, spinner).await?;
            }
            _ => eyre::bail!("User status not expected"),
        }
        Ok(())
    }

    fn parse_dz_mode(&self) -> eyre::Result<ParsedDzMode> {
        match &self.dz_mode {
            DzMode::IBRL {
                tenant,
                allocate_addr,
            } => {
                if *allocate_addr {
                    Ok(ParsedDzMode::Ibrl(UserType::IBRLWithAllocatedIP, tenant.clone()))
                } else {
                    Ok(ParsedDzMode::Ibrl(UserType::IBRL, tenant.clone()))
                }
            }
            DzMode::Multicast {
                mode,
                multicast_groups,
                pub_groups,
                sub_groups,
            } => {
                let has_legacy = mode.is_some() || !multicast_groups.is_empty();
                let has_new = !pub_groups.is_empty() || !sub_groups.is_empty();

                if has_legacy && has_new {
                    eyre::bail!("Cannot mix legacy positional args (mode + groups) with --publish/--subscribe flags");
                }

                if has_legacy {
                    let mode = mode.as_ref().ok_or_else(|| {
                        eyre::eyre!("Multicast mode (publisher/subscriber) is required when using positional arguments")
                    })?;
                    if multicast_groups.is_empty() {
                        eyre::bail!("At least one multicast group code is required");
                    }
                    let (pg, sg) = match mode {
                        MulticastMode::Publisher => (multicast_groups.clone(), vec![]),
                        MulticastMode::Subscriber => (vec![], multicast_groups.clone()),
                    };
                    Ok(ParsedDzMode::Multicast {
                        pub_groups: pg,
                        sub_groups: sg,
                    })
                } else if has_new {
                    Ok(ParsedDzMode::Multicast {
                        pub_groups: pub_groups.clone(),
                        sub_groups: sub_groups.clone(),
                    })
                } else {
                    eyre::bail!("At least one of --publish or --subscribe is required, or use legacy syntax: multicast <publisher|subscriber> <groups...>")
                }
            }
        }
    }

    async fn find_or_create_device<T: ServiceController>(
        &self,
        client: &dyn CliCommand,
        controller: &T,
        devices: &HashMap<Pubkey, Device>,
        spinner: &ProgressBar,
        exclude_ips: &[Ipv4Addr],
    ) -> eyre::Result<(Pubkey, Device, Ipv4Addr)> {
        spinner.set_message("Searching for the nearest device...");
        // filter out existing devices for users with existing tunnels
        // put some arbitrary cap on latency for second devices
        let (device_pk, tunnel_endpoint) = match self.device.as_ref() {
            Some(device) => {
                let pk = match device.parse::<Pubkey>() {
                    Ok(pubkey) => pubkey,
                    Err(_) => {
                        let (pubkey, _) = devices
                            .iter()
                            .find(|(_, d)| d.code == *device)
                            .ok_or(eyre::eyre!("Device not found"))?;
                        *pubkey
                    }
                };
                // For explicit device selection, use latency data to pick the best endpoint
                let latencies =
                    retrieve_latencies(controller, devices, false, Some(spinner)).await?;
                let dev = devices.get(&pk);
                let device_public_ip = dev.map(|d| d.public_ip).unwrap_or(Ipv4Addr::UNSPECIFIED);
                let endpoint = select_tunnel_endpoint(
                    &latencies,
                    &pk.to_string(),
                    device_public_ip,
                    exclude_ips,
                );
                (pk, endpoint)
            }
            None => {
                let latency =
                    best_latency(controller, devices, true, Some(spinner), None, exclude_ips)
                        .await?;
                spinner.set_message("Reading device account...");
                let pk = Pubkey::from_str(&latency.device_pk)
                    .map_err(|_| eyre::eyre!("Unable to parse pubkey"))?;
                // Use select_tunnel_endpoint to pick the best available endpoint for this
                // device, respecting exclude_ips. best_latency picks the device but the
                // returned record's device_ip might be an excluded endpoint.
                let latencies =
                    retrieve_latencies(controller, devices, false, Some(spinner)).await?;
                let device_public_ip = devices
                    .get(&pk)
                    .map(|d| d.public_ip)
                    .unwrap_or(Ipv4Addr::UNSPECIFIED);
                let endpoint = select_tunnel_endpoint(
                    &latencies,
                    &pk.to_string(),
                    device_public_ip,
                    exclude_ips,
                );
                (pk, endpoint)
            }
        };

        let (_, device) = client
            .get_device(GetDeviceCommand {
                pubkey_or_code: device_pk.to_string(),
            })
            .map_err(|_| eyre::eyre!("Unable to get device"))?;

        // If user explicitly specified a device, check if it's eligible
        if self.device.is_some() && !device.is_device_eligible_for_provisioning() {
            return Err(eyre::eyre!(
                "Device is not accepting more users (at capacity or max_users=0)"
            ));
        }

        Ok((device_pk, device, tunnel_endpoint))
    }

    async fn find_or_create_user<T: ServiceController>(
        &self,
        client: &dyn CliCommand,
        controller: &T,
        client_ip: &Ipv4Addr,
        spinner: &ProgressBar,
        user_type: UserType,
        tenant: Option<String>,
    ) -> eyre::Result<(Pubkey, User, Ipv4Addr)> {
        spinner.set_message("Searching for user account...");
        spinner.inc(1);

        let users = client.list_user(ListUserCommand)?;
        let mut devices = client.list_device(ListDeviceCommand)?;

        // Only filter devices if auto-selecting; keep all if user specified a device
        if self.device.is_none() {
            devices.retain(|_, d| d.is_device_eligible_for_provisioning());
        }

        // Find user by both client_ip AND user_type to support multiple tunnel types per IP
        let matched_user = users
            .iter()
            .find(|(_, u)| u.client_ip == *client_ip && u.user_type == user_type);

        let mut tunnel_src = *client_ip;
        let user_pubkey = match matched_user {
            Some((pubkey, user)) => {
                spinner.println(format!(
                    "    An account already exists with Pubkey: {pubkey}"
                ));
                if user.status == UserStatus::PendingBan || user.status == UserStatus::Banned {
                    spinner.println("❌  The user is banned.");
                    eyre::bail!("User is banned.");
                }

                let device = devices.get(&user.device_pk).ok_or(eyre::eyre!(
                    "Device {} not found for user {}",
                    user.device_pk,
                    pubkey
                ))?;

                if user_type == UserType::IBRLWithAllocatedIP {
                    tunnel_src = resolve_tunnel_src(controller, device).await?;
                }

                *pubkey
            }
            None => {
                spinner.println("    Creating user account...");

                let exclude_ips: Vec<Ipv4Addr> = exclude_ips(&users, client_ip, &devices);

                let (device_pk, device, tunnel_endpoint) = self
                    .find_or_create_device(client, controller, &devices, spinner, &exclude_ips)
                    .await?;

                spinner.println(format!("    Device selected: {} ", device.code));
                spinner.inc(1);

                if user_type == UserType::IBRLWithAllocatedIP {
                    tunnel_src = resolve_tunnel_src(controller, &device).await?;
                }

                let tenant = tenant.or_else(|| {
                    doublezero_sdk::read_doublezero_config()
                        .ok()
                        .and_then(|(_, cfg)| cfg.tenant)
                });

                let tenant_pk = match tenant {
                    Some(tenant_str) => match parse_pubkey(&tenant_str) {
                        Some(pk) => Some(pk),
                        None => {
                            let (pubkey, _) = client
                                .get_tenant(GetTenantCommand {
                                    pubkey_or_code: tenant_str.clone(),
                                })
                                .map_err(|_| eyre::eyre!("Tenant not found"))?;
                            Some(pubkey)
                        }
                    },
                    None => None,
                };

                let res = client.create_user(CreateUserCommand {
                    user_type,
                    device_pk,
                    cyoa_type: UserCYOA::GREOverDIA,
                    client_ip: *client_ip,
                    tunnel_endpoint,
                    tenant_pk,
                });

                match res {
                    Ok((_, pubkey)) => {
                        spinner.set_message("User created");
                        Ok(pubkey)
                    }
                    Err(e) => {
                        spinner.println("❌ Error creating user");
                        spinner.println(format!("\n{}: {:?}\n", "Error", e));

                        Err(eyre::eyre!("Error creating user"))
                    }
                }
            }?,
        };

        let user = self.poll_for_user_activated(client, &user_pubkey, spinner)?;

        Ok((user_pubkey, user, tunnel_src))
    }

    async fn find_or_create_user_and_subscribe<T: ServiceController>(
        &self,
        client: &dyn CliCommand,
        controller: &T,
        client_ip: &Ipv4Addr,
        spinner: &ProgressBar,
        pub_group_pks: &[Pubkey],
        sub_group_pks: &[Pubkey],
    ) -> eyre::Result<(Pubkey, User)> {
        spinner.set_message("Searching for user account...");
        spinner.inc(1);

        let users = client.list_user(ListUserCommand)?;
        let mut devices = client.list_device(ListDeviceCommand)?;

        // Only filter devices if auto-selecting; keep all if user specified a device
        if self.device.is_none() {
            devices.retain(|_, d| d.is_device_eligible_for_provisioning());
        }

        // Find all users for this IP - multiple user accounts per IP are allowed (one per UserType)
        let matched_users: Vec<_> = users
            .iter()
            .filter(|(_, u)| u.client_ip == *client_ip)
            .collect();

        let ibrl_user = matched_users
            .iter()
            .find(|(_, u)| matches!(u.user_type, UserType::IBRL | UserType::IBRLWithAllocatedIP))
            .copied();

        let mcast_user = matched_users
            .iter()
            .find(|(_, u)| u.user_type == UserType::Multicast)
            .copied();

        // Combine all group pks (deduplicated) for user creation (first group goes in create_subscribe_user)
        let mut all_group_pks: Vec<Pubkey> = Vec::new();
        for pk in pub_group_pks.iter().chain(sub_group_pks.iter()) {
            if !all_group_pks.contains(pk) {
                all_group_pks.push(*pk);
            }
        }

        let user_pubkey = match (ibrl_user, mcast_user) {
            // IBRL user exists but no Multicast user - create a separate Multicast user
            // This allows concurrent unicast (IBRL) and multicast tunnels for the same client IP
            (Some((ibrl_user_pk, ibrl_user)), None) => {
                spinner.println(format!(
                    "    Creating separate Multicast user for concurrent tunnels (IBRL user: {})",
                    ibrl_user_pk
                ));

                // Select a separate device from the IBRL user to allow independent tunnels
                // Exclude the IBRL user's tunnel endpoint to ensure we get a different device
                let exclude_ips: Vec<Ipv4Addr> = exclude_ips(&users, client_ip, &devices);

                let (device_pk, device, tunnel_endpoint) = self
                    .find_or_create_device(client, controller, &devices, spinner, &exclude_ips)
                    .await?;

                spinner.println(format!(
                    "    The Device has been selected: {} ",
                    device.code
                ));

                // Create user with first group (pick from pub_groups first, then sub_groups)
                let first_group_pk = all_group_pks
                    .first()
                    .ok_or_else(|| eyre::eyre!("At least one multicast group is required"))?;

                let res = client.create_subscribe_user(CreateSubscribeUserCommand {
                    user_type: UserType::Multicast,
                    device_pk,
                    cyoa_type: ibrl_user.cyoa_type,
                    client_ip: *client_ip,
                    mgroup_pk: *first_group_pk,
                    publisher: pub_group_pks.contains(first_group_pk),
                    subscriber: sub_group_pks.contains(first_group_pk),
                    tunnel_endpoint,
                });

                let user_pk = match res {
                    Ok((_, user_pk)) => {
                        spinner.set_message("Multicast user created");
                        user_pk
                    }
                    Err(e) => {
                        spinner.println("❌ Error creating Multicast user");
                        spinner.println(format!("\n{}: {:?}\n", "Error", e));
                        eyre::bail!("Error creating Multicast user: {:?}", e);
                    }
                };

                // Wait for user to be activated before subscribing to additional groups
                if all_group_pks.len() > 1 {
                    self.poll_for_user_activated(client, &user_pk, spinner)?;
                }

                // Subscribe to remaining groups
                for group_pk in all_group_pks.iter().skip(1) {
                    spinner.println(format!("    Subscribing to group: {group_pk}"));
                    client.subscribe_multicastgroup(SubscribeMulticastGroupCommand {
                        user_pk,
                        group_pk: *group_pk,
                        client_ip: *client_ip,
                        publisher: pub_group_pks.contains(group_pk),
                        subscriber: sub_group_pks.contains(group_pk),
                    })?;
                }

                user_pk
            }
            // Both IBRL and Multicast users exist - add subscription to existing Multicast user
            (Some(_), Some((user_pk, user))) | (None, Some((user_pk, user))) => {
                // Ensure user is activated before subscribing to new groups
                if user.status != UserStatus::Activated {
                    self.poll_for_user_activated(client, user_pk, spinner)?;
                }

                // Subscribe to any pub groups not already subscribed
                for group_pk in pub_group_pks {
                    if !user.publishers.contains(group_pk) {
                        spinner.println(format!(
                            "    Adding publisher subscription to existing Multicast user: {}",
                            user_pk
                        ));

                        let res = client.subscribe_multicastgroup(SubscribeMulticastGroupCommand {
                            user_pk: *user_pk,
                            group_pk: *group_pk,
                            client_ip: *client_ip,
                            publisher: true,
                            subscriber: false,
                        });

                        match res {
                            Ok(_) => {
                                spinner.set_message("Publisher subscription added");
                            }
                            Err(e) => {
                                spinner.println("❌ Error adding publisher subscription");
                                spinner.println(format!("\n{}: {:?}\n", "Error", e));
                                eyre::bail!("Error adding publisher subscription to existing user");
                            }
                        }
                    }
                }

                // Subscribe to any sub groups not already subscribed
                for group_pk in sub_group_pks {
                    if !user.subscribers.contains(group_pk) {
                        spinner.println(format!(
                            "    Adding subscriber subscription to existing Multicast user: {}",
                            user_pk
                        ));

                        let res = client.subscribe_multicastgroup(SubscribeMulticastGroupCommand {
                            user_pk: *user_pk,
                            group_pk: *group_pk,
                            client_ip: *client_ip,
                            publisher: false,
                            subscriber: true,
                        });

                        match res {
                            Ok(_) => {
                                spinner.set_message("Subscriber subscription added");
                            }
                            Err(e) => {
                                spinner.println("❌ Error adding subscriber subscription");
                                spinner.println(format!("\n{}: {:?}\n", "Error", e));
                                eyre::bail!(
                                    "Error adding subscriber subscription to existing user"
                                );
                            }
                        }
                    }
                }

                *user_pk
            }
            // No user exists, create a new Multicast user
            (None, None) => {
                spinner.println(format!("    Creating an account for the IP: {client_ip}"));

                let exclude_ips: Vec<Ipv4Addr> = exclude_ips(&users, client_ip, &devices);

                let (device_pk, device, tunnel_endpoint) = self
                    .find_or_create_device(client, controller, &devices, spinner, &exclude_ips)
                    .await?;

                spinner.println(format!(
                    "    The Device has been selected: {} ",
                    device.code
                ));
                spinner.inc(1);

                // Create user with first group (pick from pub_groups first, then sub_groups)
                let first_group_pk = all_group_pks
                    .first()
                    .ok_or_else(|| eyre::eyre!("At least one multicast group is required"))?;

                let res = client.create_subscribe_user(CreateSubscribeUserCommand {
                    user_type: UserType::Multicast,
                    device_pk,
                    cyoa_type: UserCYOA::GREOverDIA,
                    client_ip: *client_ip,
                    mgroup_pk: *first_group_pk,
                    publisher: pub_group_pks.contains(first_group_pk),
                    subscriber: sub_group_pks.contains(first_group_pk),
                    tunnel_endpoint,
                });

                let user_pk = match res {
                    Ok((_, pubkey)) => {
                        spinner.set_message("User created");
                        pubkey
                    }
                    Err(e) => {
                        spinner.println("❌ Error creating user");
                        spinner.println(format!("\n{}: {:?}\n", "Error", e));
                        return Err(eyre::eyre!("Error creating user"));
                    }
                };

                // Wait for user to be activated before subscribing to additional groups
                if all_group_pks.len() > 1 {
                    self.poll_for_user_activated(client, &user_pk, spinner)?;
                }

                // Subscribe to remaining groups
                for group_pk in all_group_pks.iter().skip(1) {
                    spinner.println(format!("    Subscribing to group: {group_pk}"));
                    client.subscribe_multicastgroup(SubscribeMulticastGroupCommand {
                        user_pk,
                        group_pk: *group_pk,
                        client_ip: *client_ip,
                        publisher: pub_group_pks.contains(group_pk),
                        subscriber: sub_group_pks.contains(group_pk),
                    })?;
                }

                user_pk
            }
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

        // activator polling is done every 1-minute, so if the activator websocket misses the user
        // create, then we may need to wait up to 2 minutes for the activator to pick up the user
        let builder = ExponentialBuilder::new()
            .with_max_times(8) // 1+2+4+8+16+32+32+32 = 127 seconds max
            .with_min_delay(Duration::from_secs(1))
            .with_max_delay(Duration::from_secs(32));

        let get_activated_user = || {
            client
                .get_user(GetUserCommand {
                    pubkey: *user_pubkey,
                })
                .and_then(|(pk, user)| {
                    if user.status != UserStatus::Activated {
                        Err(eyre::eyre!("User not activated yet"))
                    } else {
                        Ok((pk, user))
                    }
                })
                .map_err(|e| eyre::eyre!(e.to_string()))
        };

        get_activated_user
            .retry(builder)
            .notify(|_, dur| {
                spinner.set_message(format!(
                    "Waiting for user activation (checking in {dur:?})..."
                ))
            })
            .call()
            .map(|(_, user)| user)
            .map_err(|_| eyre::eyre!("Timeout waiting for user activation"))
    }

    #[allow(clippy::too_many_arguments)]
    async fn user_activated<T: ServiceController>(
        &self,
        client: &dyn CliCommand,
        controller: &T,
        user: &User,
        tunnel_src: &Ipv4Addr,
        spinner: &ProgressBar,
        user_type: UserType,
        mcast_pub_groups: Option<Vec<String>>,
        mcast_sub_groups: Option<Vec<String>>,
    ) -> eyre::Result<()> {
        spinner.inc(1);
        spinner.set_message("Reading devices...");

        let devices = client.list_device(ListDeviceCommand)?;
        let prefixes = devices
            .values()
            .flat_map(|device| Into::<Vec<NetworkV4>>::into(device.dz_prefixes.clone()))
            .collect::<Vec<NetworkV4>>();

        spinner.set_message("Getting global-config...");
        let (_, config) = client
            .get_globalconfig(GetGlobalConfigCommand)
            .map_err(|_| eyre::eyre!("Unable to get global config"))?;

        spinner.set_message("Getting user account...");
        let device = devices
            .get(&user.device_pk)
            .ok_or(eyre::eyre!("Device not found"))?;

        spinner.inc(1);

        // Tunnel provisioning: use the activator-assigned tunnel endpoint if set,
        // otherwise fall back to the device's public IP.
        let tunnel_dst = if user.has_tunnel_endpoint() {
            user.tunnel_endpoint.to_string()
        } else {
            device.public_ip.to_string()
        };
        let tunnel_net = user.tunnel_net.to_string();
        let doublezero_ip = &user.dz_ip;
        let doublezero_prefixes: Vec<String> =
            prefixes.into_iter().map(|net| net.to_string()).collect();

        // Determine the effective user type for provisioning:
        // - For pure Multicast users: use Multicast
        // - For IBRL users (even when adding multicast): use the user's actual type
        // This preserves backward compatibility where multicast groups are added to existing IBRL tunnels
        let effective_user_type =
            if user_type == UserType::Multicast && user.user_type != UserType::Multicast {
                // Adding multicast to an existing IBRL user - use their actual type
                user.user_type
            } else {
                user_type
            };

        if self.verbose {
            spinner.println(format!(
                "➤   Provisioning Local Tunnel for IP: {}\n\ttunnel_src: {}\n\ttunnel_dst: {}\n\ttunnel_net: {}\n\tdoublezero_ip: {}\n\tdoublezero_prefixes: {:?}\n\tlocal_asn: {}\n\tremote_asn: {}\n\tmcast_pub_groups: {:?}\n\tmcast_sub_groups: {:?}\n",
                user.client_ip,
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
                tunnel_src: tunnel_src.to_string(),
                tunnel_dst: tunnel_dst.clone(),
                tunnel_net: tunnel_net.clone(),
                doublezero_ip: doublezero_ip.to_string(),
                doublezero_prefixes: doublezero_prefixes.clone(),
                bgp_local_asn: Some(config.local_asn),
                bgp_remote_asn: Some(config.remote_asn),
                user_type: effective_user_type.to_string(),
                mcast_pub_groups: mcast_pub_groups.clone(),
                mcast_sub_groups: mcast_sub_groups.clone(),
            })
            .await
        {
            Ok(res) => {
                spinner.println(format!(
                    "    Service provisioned with status: {}",
                    res.status
                ));
            }
            Err(e) => {
                spinner.println(format!("❌ Error provisioning service: {e:?}"));
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
        let msgs = client
            .get_logs(user_pubkey)
            .map_err(|_| eyre::eyre!("Unable to get logs"))?;

        for mut msg in msgs {
            if msg.starts_with("Program log: Error: ") {
                spinner.println(format!("    {}", msg.split_off(20)));
            }
        }

        Ok(())
    }
}

async fn resolve_tunnel_src<T: ServiceController>(
    controller: &T,
    device: &Device,
) -> eyre::Result<Ipv4Addr> {
    let resolved_route = resolve_route(
        controller,
        None,
        ResolveRouteRequest {
            dst: device.public_ip,
        },
    )
    .await?;
    if let Some(src) = resolved_route.src {
        Ok(src)
    } else {
        Err(eyre::eyre!(
            "Unable to resolve route for device IP: {}",
            device.code
        ))
    }
}

fn exclude_ips(
    users: &HashMap<Pubkey, User>,
    client_ip: &Ipv4Addr,
    devices: &HashMap<Pubkey, Device>,
) -> Vec<Ipv4Addr> {
    users
        .iter()
        .filter(|(_, u)| u.client_ip == *client_ip && u.has_unicast_tunnel())
        .map(|(_, u)| {
            if u.has_tunnel_endpoint() {
                u.tunnel_endpoint
            } else {
                devices
                    .get(&u.device_pk)
                    .map(|d| d.public_ip)
                    .unwrap_or(Ipv4Addr::UNSPECIFIED)
            }
        })
        .filter(|ip| *ip != Ipv4Addr::UNSPECIFIED)
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::servicecontroller::{
        LatencyRecord, MockServiceController, ProvisioningResponse, ResolveRouteRequest,
        ResolveRouteResponse,
    };
    use doublezero_cli::{doublezerocommand::MockCliCommand, tests::utils::create_test_client};
    use doublezero_config::Environment;
    use doublezero_sdk::{
        commands::accesspass::get::GetAccessPassCommand, tests::utils::create_temp_config,
        utils::parse_pubkey,
    };
    use doublezero_serviceability::{
        pda::get_accesspass_pda,
        state::{
            accesspass::{AccessPass, AccessPassStatus, AccessPassType},
            accounttype::AccountType,
            device::{Device, DeviceStatus, DeviceType},
            globalconfig::GlobalConfig,
            multicastgroup::{MulticastGroup, MulticastGroupStatus},
            tenant::{Tenant, TenantBillingConfig, TenantPaymentStatus},
        },
    };
    use mockall::predicate;
    use solana_sdk::signature::Signature;
    use std::{
        collections::HashMap,
        sync::{Arc, Mutex, OnceLock},
        thread,
        time::Duration,
    };
    use tempfile::TempDir;

    static TMPDIR: OnceLock<TempDir> = OnceLock::new();

    fn get_temp_dir() -> &'static TempDir {
        TMPDIR.get_or_init(|| create_temp_config().expect("Failed to create temp config"))
    }

    #[ctor::ctor]
    fn setup() {
        let temp_dir = get_temp_dir();
        println!("Using TMPDIR = {}", temp_dir.path().display());
    }

    struct TestFixture {
        pub global_cfg: GlobalConfig,
        pub client: MockCliCommand,
        pub controller: MockServiceController,
        pub devices: Arc<Mutex<HashMap<Pubkey, Device>>>,
        pub users: Arc<Mutex<HashMap<Pubkey, User>>>,
        pub latencies: Arc<Mutex<Vec<LatencyRecord>>>,
        pub mcast_groups: Arc<Mutex<HashMap<Pubkey, MulticastGroup>>>,
        pub tenants: Arc<Mutex<HashMap<Pubkey, Tenant>>>,
        pub default_tenant_pk: Pubkey,
    }

    impl TestFixture {
        pub fn new() -> Self {
            // Create a default tenant
            let default_tenant_pk = Pubkey::new_unique();
            let default_tenant = Tenant {
                account_type: AccountType::Tenant,
                owner: Pubkey::new_unique(),
                bump_seed: 1,
                code: "test-tenant".to_string(),
                vrf_id: 100,
                reference_count: 0,
                administrators: vec![],
                payment_status: TenantPaymentStatus::Paid,
                token_account: Pubkey::default(),
                metro_route: false,
                route_liveness: false,
                billing: TenantBillingConfig::default(),
            };

            let mut tenants = HashMap::new();
            tenants.insert(default_tenant_pk, default_tenant);

            let mut fixture = Self {
                global_cfg: GlobalConfig {
                    account_type: AccountType::GlobalConfig,
                    owner: Pubkey::new_unique(),
                    bump_seed: 1,
                    local_asn: 65000,
                    remote_asn: 65001,
                    device_tunnel_block: "10.0.0.0/24".parse().unwrap(),
                    user_tunnel_block: "10.0.1.0/24".parse().unwrap(),
                    multicastgroup_block: "239.0.0.0/24".parse().unwrap(),
                    next_bgp_community: 10000,
                },
                client: create_test_client(),
                controller: MockServiceController::new(),
                devices: Arc::new(Mutex::new(HashMap::new())),
                users: Arc::new(Mutex::new(HashMap::new())),
                latencies: Arc::new(Mutex::new(vec![])),
                mcast_groups: Arc::new(Mutex::new(HashMap::new())),
                tenants: Arc::new(Mutex::new(tenants)),
                default_tenant_pk,
            };

            fixture
                .controller
                .expect_get_env()
                .returning_st(|| Ok(Environment::default()));

            fixture
                .client
                .expect_get_environment()
                .returning_st(Environment::default);

            fixture
                .controller
                .expect_service_controller_check()
                .return_const(true);

            fixture
                .controller
                .expect_service_controller_can_open()
                .return_const(true);

            let latencies = fixture.latencies.clone();
            fixture
                .controller
                .expect_latency()
                .returning_st(move || Ok(latencies.lock().unwrap().clone()));

            let global_cfg = fixture.global_cfg.clone();
            fixture
                .client
                .expect_get_globalconfig()
                .returning_st(move |_| Ok((Pubkey::new_unique(), global_cfg.clone())));

            let payer = fixture.client.get_payer();

            let (accesspass_pk, _) = get_accesspass_pda(
                &fixture.client.get_program_id(),
                &Ipv4Addr::new(1, 2, 3, 4),
                &payer,
            );
            let accesspass = AccessPass {
                account_type: AccountType::AccessPass,
                owner: payer,
                bump_seed: 1,
                client_ip: Ipv4Addr::new(1, 2, 3, 4),
                user_payer: payer,
                last_access_epoch: u64::MAX,
                accesspass_type: AccessPassType::Prepaid,
                connection_count: 0,
                status: AccessPassStatus::Requested,
                mgroup_pub_allowlist: vec![],
                mgroup_sub_allowlist: vec![],
                tenant_allowlist: vec![],
                flags: 0,
            };
            fixture
                .client
                .expect_get_accesspass()
                .with(predicate::eq(GetAccessPassCommand {
                    client_ip: Ipv4Addr::new(1, 2, 3, 4),
                    user_payer: payer,
                }))
                .returning_st(move |_| Ok((accesspass_pk, accesspass.clone())));

            let users = fixture.users.clone();
            fixture
                .client
                .expect_list_user()
                .returning_st(move |_| Ok(users.lock().unwrap().clone()));

            let devices = fixture.devices.clone();
            fixture
                .client
                .expect_list_device()
                .returning_st(move |_| Ok(devices.lock().unwrap().clone()));

            let mcast_groups = fixture.mcast_groups.clone();
            fixture
                .client
                .expect_list_multicastgroup()
                .returning_st(move |_| Ok(mcast_groups.lock().unwrap().clone()));

            let users = fixture.users.clone();
            fixture.client.expect_get_user().returning_st(move |cmd| {
                thread::sleep(Duration::from_secs(1));
                let user_pk = cmd.pubkey;
                let users = users.lock().unwrap();
                let user = users.get(&user_pk);
                match user {
                    Some(user) => Ok((user_pk, user.clone())),
                    None => Err(eyre::eyre!("User not found")),
                }
            });

            let devices = fixture.devices.clone();
            fixture.client.expect_get_device().returning_st(move |cmd| {
                thread::sleep(Duration::from_secs(1));
                let devices = devices.lock().unwrap();
                match parse_pubkey(&cmd.pubkey_or_code) {
                    Some(pk) => match devices.get(&pk) {
                        Some(device) => Ok((pk, device.clone())),
                        None => Err(eyre::eyre!("Invalid Account Type")),
                    },
                    None => {
                        let dev = devices.iter().find(|(_, v)| v.code == cmd.pubkey_or_code);
                        match dev {
                            Some((pk, device)) => Ok((*pk, device.clone())),
                            None => Err(eyre::eyre!("Device not found")),
                        }
                    }
                }
            });

            let tenants = fixture.tenants.clone();
            fixture.client.expect_get_tenant().returning_st(move |cmd| {
                let tenants = tenants.lock().unwrap();
                match parse_pubkey(&cmd.pubkey_or_code) {
                    Some(pk) => match tenants.get(&pk) {
                        Some(tenant) => Ok((pk, tenant.clone())),
                        None => Err(eyre::eyre!("Invalid Account Type")),
                    },
                    None => {
                        let tenant = tenants.iter().find(|(_, v)| v.code == cmd.pubkey_or_code);
                        match tenant {
                            Some((pk, tenant)) => Ok((*pk, tenant.clone())),
                            None => Err(eyre::eyre!("Tenant not found")),
                        }
                    }
                }
            });

            fixture
        }

        pub fn add_device(
            &mut self,
            device_type: DeviceType,
            latency_ns: i32,
            reachable: bool,
        ) -> (Pubkey, Device) {
            let mut devices = self.devices.lock().unwrap();
            let device_number = devices.len() + 1;
            let pk = Pubkey::new_unique();
            let device_ip = format!("5.6.7.{device_number}");
            self.latencies.lock().unwrap().push(LatencyRecord {
                device_pk: pk.to_string(),
                device_ip: device_ip.clone(),
                device_code: format!("device{device_number}"),
                min_latency_ns: latency_ns,
                max_latency_ns: latency_ns,
                avg_latency_ns: latency_ns,
                reachable,
            });
            let device = Device {
                account_type: AccountType::Device,
                owner: Pubkey::new_unique(),
                index: device_number as u128,
                bump_seed: 255,
                reference_count: 0,
                contributor_pk: Pubkey::new_unique(),
                location_pk: Pubkey::new_unique(),
                exchange_pk: Pubkey::new_unique(),
                device_type,
                public_ip: device_ip.parse().unwrap(),
                status: DeviceStatus::Activated,
                metrics_publisher_pk: Pubkey::default(),
                code: format!("device{device_number}"),
                dz_prefixes: format!("10.{}.0.0/24", device_number).parse().unwrap(),
                mgmt_vrf: "default".to_string(),
                interfaces: vec![],
                max_users: 255,
                users_count: 0,
                device_health:
                    doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
                desired_status:
                    doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
            };
            devices.insert(pk, device.clone());
            (pk, device)
        }

        pub fn add_multicast_group(
            &mut self,
            code: &str,
            multicast_ip: &str,
        ) -> (Pubkey, MulticastGroup) {
            let mut mcast_groups = self.mcast_groups.lock().unwrap();
            let pk = Pubkey::new_unique();
            let group = MulticastGroup {
                account_type: AccountType::MulticastGroup,
                owner: Pubkey::new_unique(),
                index: 1,
                bump_seed: 1,
                tenant_pk: Pubkey::new_unique(),
                multicast_ip: multicast_ip.parse().unwrap(),
                max_bandwidth: 10_000_000_000,
                status: MulticastGroupStatus::Activated,
                code: code.to_string(),
                publisher_count: 0,
                subscriber_count: 0,
            };
            mcast_groups.insert(pk, group.clone());
            (pk, group)
        }

        pub fn add_tenant(&mut self, code: &str) -> (Pubkey, Tenant) {
            let mut tenants = self.tenants.lock().unwrap();
            let pk = Pubkey::new_unique();
            let tenant = Tenant {
                account_type: AccountType::Tenant,
                owner: Pubkey::new_unique(),
                bump_seed: 1,
                code: code.to_string(),
                vrf_id: 100,
                reference_count: 0,
                administrators: vec![],
                payment_status: TenantPaymentStatus::Paid,
                token_account: Pubkey::default(),
                metro_route: false,
                route_liveness: false,
                billing: TenantBillingConfig::default(),
            };
            tenants.insert(pk, tenant.clone());
            (pk, tenant)
        }

        pub fn create_user(
            &mut self,
            user_type: UserType,
            device_pk: Pubkey,
            client_ip: &str,
        ) -> User {
            // Look up device's public_ip to set as tunnel_endpoint
            let tunnel_endpoint = self
                .devices
                .lock()
                .unwrap()
                .get(&device_pk)
                .map(|d| d.public_ip)
                .unwrap_or(Ipv4Addr::UNSPECIFIED);

            User {
                account_type: AccountType::User,
                owner: Pubkey::new_unique(),
                index: 1,
                bump_seed: 1,
                user_type,
                device_pk,
                tenant_pk: Pubkey::new_unique(),
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: client_ip.parse().unwrap(),
                dz_ip: client_ip.parse().unwrap(),
                tunnel_id: 1,
                tunnel_net: "10.1.1.0/31".parse().unwrap(),
                status: UserStatus::Activated,
                publishers: vec![],
                subscribers: vec![],
                validator_pubkey: Pubkey::new_unique(),
                tunnel_endpoint,
            }
        }

        pub fn add_user(&mut self, user: &User) -> Pubkey {
            let mut users = self.users.lock().unwrap();
            let pk = Pubkey::new_unique();
            users.insert(pk, user.clone());
            let users = self.users.clone();
            self.client
                .expect_list_user()
                .returning_st(move |_| Ok(users.lock().unwrap().clone()));
            pk
        }

        pub fn expect_create_user(&mut self, pk: Pubkey, user: &User) {
            self.expect_create_user_with_tenant(pk, user, Some(self.default_tenant_pk));
        }

        pub fn expect_create_user_with_tenant(
            &mut self,
            pk: Pubkey,
            user: &User,
            tenant_pk: Option<Pubkey>,
        ) {
            let expected_create_user_command = CreateUserCommand {
                user_type: user.user_type,
                device_pk: user.device_pk,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: user.client_ip,
                tunnel_endpoint: user.tunnel_endpoint,
                tenant_pk,
            };

            let users = self.users.clone();
            let user = user.clone();
            self.client
                .expect_create_user()
                .times(1)
                .with(predicate::eq(expected_create_user_command))
                .returning_st(move |_| {
                    thread::sleep(Duration::from_secs(1));
                    users.lock().unwrap().insert(pk, user.clone());
                    Ok((Signature::default(), pk))
                });
        }

        pub fn expect_create_subscribe_user(
            &mut self,
            pk: Pubkey,
            user: &User,
            mcast_group_pk: Pubkey,
            publisher: bool,
            subscriber: bool,
        ) {
            let expected_create_subscribe_user_command = CreateSubscribeUserCommand {
                user_type: user.user_type,
                device_pk: user.device_pk,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: user.client_ip,
                mgroup_pk: mcast_group_pk,
                publisher,
                subscriber,
                tunnel_endpoint: user.tunnel_endpoint,
            };

            let users = self.users.clone();
            let mut user = user.clone();
            if publisher {
                user.publishers.push(mcast_group_pk);
            }
            if subscriber {
                user.subscribers.push(mcast_group_pk);
            }
            self.client
                .expect_create_subscribe_user()
                .times(1)
                .with(predicate::eq(expected_create_subscribe_user_command))
                .returning_st(move |_| {
                    thread::sleep(Duration::from_secs(1));
                    users.lock().unwrap().insert(pk, user.clone());
                    Ok((Signature::default(), pk))
                });
        }

        #[allow(dead_code)]
        pub fn expect_subscribe_multicastgroup(
            &mut self,
            user_pk: Pubkey,
            mcast_group_pk: Pubkey,
            client_ip: Ipv4Addr,
            publisher: bool,
            subscriber: bool,
        ) {
            let expected_command = SubscribeMulticastGroupCommand {
                user_pk,
                group_pk: mcast_group_pk,
                client_ip,
                publisher,
                subscriber,
            };

            let users = self.users.clone();
            self.client
                .expect_subscribe_multicastgroup()
                .times(1)
                .with(predicate::eq(expected_command))
                .returning_st(move |cmd| {
                    thread::sleep(Duration::from_secs(1));
                    let mut users = users.lock().unwrap();
                    if let Some(user) = users.get_mut(&cmd.user_pk) {
                        if cmd.publisher {
                            user.publishers.push(cmd.group_pk);
                        }
                        if cmd.subscriber {
                            user.subscribers.push(cmd.group_pk);
                        }
                    }
                    Ok(Signature::default())
                });
        }

        pub fn expected_provisioning_request(
            &mut self,
            user_type: UserType,
            client_ip: &str,
            device_ip: &str,
            mcast_pub_groups: Option<Vec<String>>,
            mcast_sub_groups: Option<Vec<String>>,
        ) {
            self.expected_provisioning_request_with_tunnel_src(
                user_type,
                client_ip,
                client_ip,
                device_ip,
                mcast_pub_groups,
                mcast_sub_groups,
            );
        }

        pub fn expected_provisioning_request_with_tunnel_src(
            &mut self,
            user_type: UserType,
            client_ip: &str,
            tunnel_src: &str,
            device_ip: &str,
            mcast_pub_groups: Option<Vec<String>>,
            mcast_sub_groups: Option<Vec<String>>,
        ) {
            let expected_request = ProvisioningRequest {
                tunnel_src: tunnel_src.to_string(),
                tunnel_dst: device_ip.to_string(),
                tunnel_net: "10.1.1.0/31".to_string(),
                doublezero_ip: client_ip.to_string(),
                doublezero_prefixes: self
                    .devices
                    .lock()
                    .unwrap()
                    .values()
                    .map(|d| d.dz_prefixes.to_string())
                    .collect(),
                bgp_local_asn: Some(self.global_cfg.local_asn),
                bgp_remote_asn: Some(self.global_cfg.remote_asn),
                user_type: user_type.to_string(),
                mcast_pub_groups,
                mcast_sub_groups,
            };

            self.controller
                .expect_provisioning()
                .times(1)
                .with(predicate::eq(expected_request))
                .returning_st(move |_| {
                    thread::sleep(Duration::from_secs(1));
                    Ok(ProvisioningResponse {
                        status: "success".to_string(),
                        description: None,
                    })
                });
        }

        pub fn expect_resolve_route(&mut self, device_ip: Ipv4Addr, resolved_src: Ipv4Addr) {
            self.controller
                .expect_resolve_route()
                .times(1)
                .withf(move |req: &ResolveRouteRequest| req.dst == device_ip)
                .returning_st(move |_| {
                    Ok(ResolveRouteResponse {
                        src: Some(resolved_src),
                    })
                });
        }

        pub fn expect_resolve_route_failure(&mut self, device_ip: Ipv4Addr) {
            self.controller
                .expect_resolve_route()
                .times(1)
                .withf(move |req: &ResolveRouteRequest| req.dst == device_ip)
                .returning_st(move |_| Ok(ResolveRouteResponse { src: None }));
        }

        pub fn expect_resolve_route_error(&mut self, device_ip: Ipv4Addr) {
            self.controller
                .expect_resolve_route()
                .times(1)
                .withf(move |req: &ResolveRouteRequest| req.dst == device_ip)
                .returning_st(move |_| Err(eyre::eyre!("Failed to resolve route")));
        }
    }

    #[tokio::test]
    async fn test_connect_command_ibrl_hybrid() {
        let mut fixture = TestFixture::new();

        // Create a tenant for this test
        let (tenant_pk, tenant) = fixture.add_tenant("my-tenant");

        let (device1_pk, device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
        // Add a second device for concurrent tunnels (IBRL + Multicast must go to different devices)
        let (device2_pk, device2) = fixture.add_device(DeviceType::Hybrid, 110, true);
        let user = fixture.create_user(UserType::IBRL, device1_pk, "1.2.3.4");
        let user_pk = Pubkey::new_unique();
        fixture.expect_create_user_with_tenant(user_pk, &user, Some(tenant_pk));
        fixture.expected_provisioning_request(
            UserType::IBRL,
            user.client_ip.to_string().as_str(),
            device1.public_ip.to_string().as_str(),
            None,
            None,
        );

        // print new line for readability in test output
        println!();

        let command = ProvisioningCliCommand {
            dz_mode: DzMode::IBRL {
                tenant: Some(tenant.code.clone()),
                allocate_addr: false,
            },
            client_ip: Some(user.client_ip.to_string()),
            device: None,
            verbose: false,
        };

        let result = command
            .execute_with_service_controller(&fixture.client, &fixture.controller)
            .await;
        assert!(result.is_ok());

        println!("Test that adding a multicast tunnel with an existing IBRL creates a separate Multicast user");
        let (mcast_group_pk, mcast_group) = fixture.add_multicast_group("test-group", "239.0.0.1");

        // When IBRL user exists, a separate Multicast user should be created on a different device
        // (exclude_ips prevents reusing the same device as the IBRL tunnel)
        let mcast_user = fixture.create_user(UserType::Multicast, device2_pk, "1.2.3.4");
        fixture.expect_create_subscribe_user(
            Pubkey::new_unique(),
            &mcast_user,
            mcast_group_pk,
            true,  // publisher
            false, // subscriber
        );
        fixture.expected_provisioning_request(
            UserType::Multicast,
            user.client_ip.to_string().as_str(),
            device2.public_ip.to_string().as_str(),
            Some(vec![mcast_group.multicast_ip.to_string()]),
            Some(vec![]),
        );

        let command = ProvisioningCliCommand {
            dz_mode: DzMode::Multicast {
                mode: Some(MulticastMode::Publisher),
                multicast_groups: vec!["test-group".to_string()],
                pub_groups: vec![],
                sub_groups: vec![],
            },
            client_ip: Some(user.client_ip.to_string()),
            device: None,
            verbose: false,
        };

        let result = command
            .execute_with_service_controller(&fixture.client, &fixture.controller)
            .await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_connect_command_ibrl_edge() {
        let mut fixture = TestFixture::new();

        // Create a tenant for this test
        let (tenant_pk, tenant) = fixture.add_tenant("edge-tenant");

        let (device1_pk, device1) = fixture.add_device(DeviceType::Edge, 100, true);
        // Add a second device for concurrent tunnels (IBRL + Multicast must go to different devices)
        let (device2_pk, device2) = fixture.add_device(DeviceType::Edge, 110, true);
        let user = fixture.create_user(UserType::IBRL, device1_pk, "1.2.3.4");
        let user_pk = Pubkey::new_unique();
        fixture.expect_create_user_with_tenant(user_pk, &user, Some(tenant_pk));
        fixture.expected_provisioning_request(
            UserType::IBRL,
            user.client_ip.to_string().as_str(),
            device1.public_ip.to_string().as_str(),
            None,
            None,
        );

        // print new line for readability in test output
        println!();

        let command = ProvisioningCliCommand {
            dz_mode: DzMode::IBRL {
                tenant: Some(tenant.code.clone()),
                allocate_addr: false,
            },
            client_ip: Some(user.client_ip.to_string()),
            device: None,
            verbose: false,
        };

        let result = command
            .execute_with_service_controller(&fixture.client, &fixture.controller)
            .await;
        assert!(result.is_ok());

        println!("Test that adding a multicast tunnel with an existing IBRL creates a separate Multicast user");
        let (mcast_group_pk, mcast_group) = fixture.add_multicast_group("test-group", "239.0.0.1");

        // When IBRL user exists, a separate Multicast user should be created on a different device
        // (exclude_ips prevents reusing the same device as the IBRL tunnel)
        let mcast_user = fixture.create_user(UserType::Multicast, device2_pk, "1.2.3.4");
        fixture.expect_create_subscribe_user(
            Pubkey::new_unique(),
            &mcast_user,
            mcast_group_pk,
            true,  // publisher
            false, // subscriber
        );
        fixture.expected_provisioning_request(
            UserType::Multicast,
            user.client_ip.to_string().as_str(),
            device2.public_ip.to_string().as_str(),
            Some(vec![mcast_group.multicast_ip.to_string()]),
            Some(vec![]),
        );

        let command = ProvisioningCliCommand {
            dz_mode: DzMode::Multicast {
                mode: Some(MulticastMode::Publisher),
                multicast_groups: vec!["test-group".to_string()],
                pub_groups: vec![],
                sub_groups: vec![],
            },
            client_ip: Some(user.client_ip.to_string()),
            device: None,
            verbose: false,
        };

        let result = command
            .execute_with_service_controller(&fixture.client, &fixture.controller)
            .await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_connect_command_ibrl_transit() {
        let mut fixture = TestFixture::new();

        let (device1_pk, _device1) = fixture.add_device(DeviceType::Transit, 100, true);
        let user = fixture.create_user(UserType::IBRL, device1_pk, "1.2.3.4");

        // print new line for readability in test output
        println!();

        let command = ProvisioningCliCommand {
            dz_mode: DzMode::IBRL {
                tenant: Some("test-tenant".to_string()),
                allocate_addr: false,
            },
            client_ip: Some(user.client_ip.to_string()),
            device: None,
            verbose: false,
        };

        let result = command
            .execute_with_service_controller(&fixture.client, &fixture.controller)
            .await;
        // Should fail because Transit devices are not allowed for IBRL
        assert!(result.is_err());
    }

    #[tokio::test]
    async fn test_connect_command_ibrl_allocate_hybrid() {
        let mut fixture = TestFixture::new();

        // Create a tenant for this test
        let (tenant_pk, tenant) = fixture.add_tenant("allocate-tenant");

        let (device1_pk, device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
        let user = fixture.create_user(UserType::IBRLWithAllocatedIP, device1_pk, "1.2.3.4");
        fixture.expect_create_user_with_tenant(Pubkey::new_unique(), &user, Some(tenant_pk));

        let resolved_src = Ipv4Addr::new(192, 168, 1, 100);
        fixture.expect_resolve_route(device1.public_ip, resolved_src);

        fixture.expected_provisioning_request_with_tunnel_src(
            UserType::IBRLWithAllocatedIP,
            user.client_ip.to_string().as_str(),
            resolved_src.to_string().as_str(),
            device1.public_ip.to_string().as_str(),
            None,
            None,
        );

        // print new line for readability in test output
        println!();

        let command = ProvisioningCliCommand {
            dz_mode: DzMode::IBRL {
                tenant: Some(tenant.code.clone()),
                allocate_addr: true,
            },
            client_ip: Some(user.client_ip.to_string()),
            device: None,
            verbose: false,
        };

        let result = command
            .execute_with_service_controller(&fixture.client, &fixture.controller)
            .await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_connect_command_ibrl_allocate_edge() {
        let mut fixture = TestFixture::new();

        // Create a tenant for this test
        let (tenant_pk, tenant) = fixture.add_tenant("edge-allocate-tenant");

        let (device1_pk, device1) = fixture.add_device(DeviceType::Edge, 100, true);
        let user = fixture.create_user(UserType::IBRLWithAllocatedIP, device1_pk, "1.2.3.4");
        fixture.expect_create_user_with_tenant(Pubkey::new_unique(), &user, Some(tenant_pk));

        let resolved_src = Ipv4Addr::new(192, 168, 1, 101);
        fixture.expect_resolve_route(device1.public_ip, resolved_src);

        fixture.expected_provisioning_request_with_tunnel_src(
            UserType::IBRLWithAllocatedIP,
            user.client_ip.to_string().as_str(),
            resolved_src.to_string().as_str(),
            device1.public_ip.to_string().as_str(),
            None,
            None,
        );

        // print new line for readability in test output
        println!();

        let command = ProvisioningCliCommand {
            dz_mode: DzMode::IBRL {
                tenant: Some(tenant.code.clone()),
                allocate_addr: true,
            },
            client_ip: Some(user.client_ip.to_string()),
            device: None,
            verbose: false,
        };

        let result = command
            .execute_with_service_controller(&fixture.client, &fixture.controller)
            .await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_connect_command_ibrl_allocate_transit() {
        let mut fixture = TestFixture::new();

        let (device1_pk, _device1) = fixture.add_device(DeviceType::Transit, 100, true);
        let user = fixture.create_user(UserType::IBRLWithAllocatedIP, device1_pk, "1.2.3.4");

        // print new line for readability in test output
        println!();

        let command = ProvisioningCliCommand {
            dz_mode: DzMode::IBRL {
                tenant: Some("test-tenant".to_string()),
                allocate_addr: true,
            },
            client_ip: Some(user.client_ip.to_string()),
            device: None,
            verbose: false,
        };

        let result = command
            .execute_with_service_controller(&fixture.client, &fixture.controller)
            .await;
        // Should fail because Transit devices are not allowed for IBRL with allocate_addr
        assert!(result.is_err());
    }

    #[tokio::test]
    async fn test_connect_banned_user() {
        let mut fixture = TestFixture::new();

        let (device1_pk, _device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
        let mut user = fixture.create_user(UserType::IBRL, device1_pk, "1.2.3.4");
        user.status = UserStatus::Banned;
        fixture.add_user(&user);

        let command = ProvisioningCliCommand {
            dz_mode: DzMode::IBRL {
                tenant: Some("test-tenant".to_string()),
                allocate_addr: false,
            },
            client_ip: Some(user.client_ip.to_string()),
            device: None,
            verbose: false,
        };

        let result = command
            .execute_with_service_controller(&fixture.client, &fixture.controller)
            .await;
        assert!(result.is_err());
    }

    #[tokio::test]
    async fn test_connect_command_multicast_publisher() {
        let mut fixture = TestFixture::new();

        let (mcast_group_pk, mcast_group) = fixture.add_multicast_group("test-group", "239.0.0.1");
        let (device1_pk, device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
        let user = fixture.create_user(UserType::Multicast, device1_pk, "1.2.3.4");
        fixture.expect_create_subscribe_user(
            Pubkey::new_unique(),
            &user,
            mcast_group_pk,
            true,
            false,
        );
        fixture.expected_provisioning_request(
            UserType::Multicast,
            user.client_ip.to_string().as_str(),
            device1.public_ip.to_string().as_str(),
            Some(vec![mcast_group.multicast_ip.to_string()]),
            Some(vec![]),
        );

        // print new line for readability in test output
        println!();

        let command = ProvisioningCliCommand {
            dz_mode: DzMode::Multicast {
                mode: Some(MulticastMode::Publisher),
                multicast_groups: vec!["test-group".to_string()],
                pub_groups: vec![],
                sub_groups: vec![],
            },
            client_ip: Some(user.client_ip.to_string()),
            device: None,
            verbose: false,
        };

        let result = command
            .execute_with_service_controller(&fixture.client, &fixture.controller)
            .await;
        assert!(result.is_ok());
    }

    async fn execute_multicast_test_succeed_adding_second_group(multicast_mode: MulticastMode) {
        let mut fixture = TestFixture::new();

        let (mcast_group_pk, mcast_group) = fixture.add_multicast_group("test-group", "239.0.0.1");
        let (mcast_group2_pk, mcast_group2) =
            fixture.add_multicast_group("test-group2", "239.0.0.2");
        let (device1_pk, device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
        let mut user = fixture.create_user(UserType::Multicast, device1_pk, "1.2.3.4");

        let (publisher, subscriber) = match multicast_mode {
            MulticastMode::Publisher => (true, false),
            MulticastMode::Subscriber => (false, true),
        };

        // User already has first group
        if multicast_mode == MulticastMode::Subscriber {
            user.subscribers.push(mcast_group_pk);
        } else {
            user.publishers.push(mcast_group_pk);
        }

        let user_pk = fixture.add_user(&user);

        // Expect subscribe to second group
        fixture.expect_subscribe_multicastgroup(
            user_pk,
            mcast_group2_pk,
            user.client_ip,
            publisher,
            subscriber,
        );

        // After subscribing, user will have both groups
        let (expect_publishers, expect_subscribers) = if multicast_mode == MulticastMode::Subscriber
        {
            (
                Some(vec![]),
                Some(vec![
                    mcast_group.multicast_ip.to_string(),
                    mcast_group2.multicast_ip.to_string(),
                ]),
            )
        } else {
            (
                Some(vec![
                    mcast_group.multicast_ip.to_string(),
                    mcast_group2.multicast_ip.to_string(),
                ]),
                Some(vec![]),
            )
        };

        fixture.expected_provisioning_request(
            UserType::Multicast,
            user.client_ip.to_string().as_str(),
            device1.public_ip.to_string().as_str(),
            expect_publishers,
            expect_subscribers,
        );

        // print new line for readability in test output
        println!();

        let command = ProvisioningCliCommand {
            dz_mode: DzMode::Multicast {
                mode: Some(multicast_mode),
                multicast_groups: vec!["test-group2".to_string()],
                pub_groups: vec![],
                sub_groups: vec![],
            },
            client_ip: Some(user.client_ip.to_string()),
            device: None,
            verbose: false,
        };

        let result = command
            .execute_with_service_controller(&fixture.client, &fixture.controller)
            .await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_connect_command_multicast_publisher_rejects_duplicate_groups() {
        let mut fixture = TestFixture::new();

        fixture.add_multicast_group("test-group", "239.0.0.1");
        let (device1_pk, _) = fixture.add_device(DeviceType::Hybrid, 100, true);
        let user = fixture.create_user(UserType::Multicast, device1_pk, "1.2.3.4");

        println!();

        let command = ProvisioningCliCommand {
            dz_mode: DzMode::Multicast {
                mode: Some(MulticastMode::Publisher),
                // Pass the same group twice — should error
                multicast_groups: vec!["test-group".to_string(), "test-group".to_string()],
                pub_groups: vec![],
                sub_groups: vec![],
            },
            client_ip: Some(user.client_ip.to_string()),
            device: None,
            verbose: false,
        };

        let result = command
            .execute_with_service_controller(&fixture.client, &fixture.controller)
            .await;
        assert!(result.is_err());
        assert!(result
            .unwrap_err()
            .to_string()
            .contains("Duplicate multicast pub group"));
    }

    #[tokio::test]
    async fn test_connect_command_multicast_publisher_can_add_subscriber_group() {
        let mut fixture = TestFixture::new();

        let (mcast_group_pk, mcast_group) = fixture.add_multicast_group("test-group", "239.0.0.1");
        let (mcast_group2_pk, mcast_group2) =
            fixture.add_multicast_group("test-group2", "239.0.0.2");
        let (device1_pk, device1) = fixture.add_device(DeviceType::Hybrid, 100, true);

        // Create a user who is already a publisher
        let mut user = fixture.create_user(UserType::Multicast, device1_pk, "1.2.3.4");
        user.publishers.push(mcast_group_pk);
        let user_pk = fixture.add_user(&user);

        // Expect subscribe_multicastgroup call for the new subscriber group
        fixture.expect_subscribe_multicastgroup(
            user_pk,
            mcast_group2_pk,
            user.client_ip,
            false,
            true,
        );

        fixture.expected_provisioning_request(
            UserType::Multicast,
            user.client_ip.to_string().as_str(),
            device1.public_ip.to_string().as_str(),
            Some(vec![mcast_group.multicast_ip.to_string()]),
            Some(vec![mcast_group2.multicast_ip.to_string()]),
        );

        println!();

        // Add subscriber group to existing publisher - should succeed
        let command = ProvisioningCliCommand {
            dz_mode: DzMode::Multicast {
                mode: Some(MulticastMode::Subscriber),
                multicast_groups: vec!["test-group2".to_string()],
                pub_groups: vec![],
                sub_groups: vec![],
            },
            client_ip: Some(user.client_ip.to_string()),
            device: None,
            verbose: false,
        };

        let result = command
            .execute_with_service_controller(&fixture.client, &fixture.controller)
            .await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_connect_command_multicast_subscriber_can_add_publisher_group() {
        let mut fixture = TestFixture::new();

        let (mcast_group_pk, mcast_group) = fixture.add_multicast_group("test-group", "239.0.0.1");
        let (mcast_group2_pk, mcast_group2) =
            fixture.add_multicast_group("test-group2", "239.0.0.2");
        let (device1_pk, device1) = fixture.add_device(DeviceType::Hybrid, 100, true);

        // Create a user who is already a subscriber
        let mut user = fixture.create_user(UserType::Multicast, device1_pk, "1.2.3.4");
        user.subscribers.push(mcast_group_pk);
        let user_pk = fixture.add_user(&user);

        // Expect subscribe_multicastgroup call for the new publisher group
        fixture.expect_subscribe_multicastgroup(
            user_pk,
            mcast_group2_pk,
            user.client_ip,
            true,
            false,
        );

        fixture.expected_provisioning_request(
            UserType::Multicast,
            user.client_ip.to_string().as_str(),
            device1.public_ip.to_string().as_str(),
            Some(vec![mcast_group2.multicast_ip.to_string()]),
            Some(vec![mcast_group.multicast_ip.to_string()]),
        );

        println!();

        // Add publisher group to existing subscriber - should succeed
        let command = ProvisioningCliCommand {
            dz_mode: DzMode::Multicast {
                mode: Some(MulticastMode::Publisher),
                multicast_groups: vec!["test-group2".to_string()],
                pub_groups: vec![],
                sub_groups: vec![],
            },
            client_ip: Some(user.client_ip.to_string()),
            device: None,
            verbose: false,
        };

        let result = command
            .execute_with_service_controller(&fixture.client, &fixture.controller)
            .await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_connect_command_multicast_publisher_succeed_adding_second_group() {
        execute_multicast_test_succeed_adding_second_group(MulticastMode::Publisher).await;
    }

    #[tokio::test]
    async fn test_connect_command_multicast_subscriber_succeed_adding_second_group() {
        execute_multicast_test_succeed_adding_second_group(MulticastMode::Subscriber).await;
    }

    async fn execute_multicast_test_succeed_already_in_the_group(multicast_mode: MulticastMode) {
        let mut fixture = TestFixture::new();

        let (mcast_group_pk, mcast_group) = fixture.add_multicast_group("test-group", "239.0.0.1");
        let (device1_pk, device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
        let mut user = fixture.create_user(UserType::Multicast, device1_pk, "1.2.3.4");

        let expect_publishers;
        let expect_subscribers;

        if multicast_mode == MulticastMode::Subscriber {
            user.subscribers.push(mcast_group_pk);
            expect_publishers = Some(vec![]);
            expect_subscribers = Some(vec![mcast_group.multicast_ip.to_string()]);
        } else {
            user.publishers.push(mcast_group_pk);
            expect_publishers = Some(vec![mcast_group.multicast_ip.to_string()]);
            expect_subscribers = Some(vec![]);
        }

        fixture.add_user(&user);

        fixture.expected_provisioning_request(
            UserType::Multicast,
            user.client_ip.to_string().as_str(),
            device1.public_ip.to_string().as_str(),
            expect_publishers,
            expect_subscribers,
        );

        // print new line for readability in test output
        println!();

        let command = ProvisioningCliCommand {
            dz_mode: DzMode::Multicast {
                mode: Some(multicast_mode),
                multicast_groups: vec!["test-group".to_string()],
                pub_groups: vec![],
                sub_groups: vec![],
            },
            client_ip: Some(user.client_ip.to_string()),
            device: None,
            verbose: false,
        };

        let result = command
            .execute_with_service_controller(&fixture.client, &fixture.controller)
            .await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_connect_command_multicast_publisher_succeed_already_in_group() {
        execute_multicast_test_succeed_already_in_the_group(MulticastMode::Publisher).await;
    }

    #[tokio::test]
    async fn test_connect_command_multicast_subscriber_succeed_already_in_group() {
        execute_multicast_test_succeed_already_in_the_group(MulticastMode::Subscriber).await;
    }

    #[tokio::test]
    async fn test_connect_command_multicast_subscribe() {
        let mut fixture = TestFixture::new();

        let (mcast_group_pk, mcast_group) = fixture.add_multicast_group("test-group", "239.0.0.1");
        (_, _) = fixture.add_multicast_group("test-group2", "239.0.0.2");
        let (device1_pk, device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
        // Add a second device for concurrent tunnels (Multicast + IBRL must go to different devices)
        let (device2_pk, device2) = fixture.add_device(DeviceType::Hybrid, 110, true);
        let user = fixture.create_user(UserType::Multicast, device1_pk, "1.2.3.4");
        fixture.expect_create_subscribe_user(
            Pubkey::new_unique(),
            &user,
            mcast_group_pk,
            false,
            true,
        );
        fixture.expected_provisioning_request(
            UserType::Multicast,
            user.client_ip.to_string().as_str(),
            device1.public_ip.to_string().as_str(),
            Some(vec![]),
            Some(vec![mcast_group.multicast_ip.to_string()]),
        );

        let command = ProvisioningCliCommand {
            dz_mode: DzMode::Multicast {
                mode: Some(MulticastMode::Subscriber),
                multicast_groups: vec!["test-group".to_string()],
                pub_groups: vec![],
                sub_groups: vec![],
            },
            client_ip: Some(user.client_ip.to_string()),
            device: None,
            verbose: false,
        };

        let result = command
            .execute_with_service_controller(&fixture.client, &fixture.controller)
            .await;
        assert!(result.is_ok());

        // Test that adding an IBRL tunnel with an existing multicast succeeds on a DIFFERENT device
        // (concurrent tunnels from same client IP must go to different devices)
        let ibrl_user = fixture.create_user(UserType::IBRL, device2_pk, "1.2.3.4");
        fixture.expect_create_user(Pubkey::new_unique(), &ibrl_user);

        fixture.expected_provisioning_request(
            UserType::IBRL,
            ibrl_user.client_ip.to_string().as_str(),
            device2.public_ip.to_string().as_str(),
            None,
            None,
        );

        let command = ProvisioningCliCommand {
            dz_mode: DzMode::IBRL {
                tenant: Some("test-tenant".to_string()),
                allocate_addr: false,
            },
            client_ip: Some(ibrl_user.client_ip.to_string()),
            device: None,
            verbose: false,
        };

        let result = command
            .execute_with_service_controller(&fixture.client, &fixture.controller)
            .await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_connect_command_delayed_latencies() {
        let mut fixture = TestFixture::new();

        let (mcast_group_pk, mcast_group) = fixture.add_multicast_group("test-group", "239.0.0.1");
        (_, _) = fixture.add_multicast_group("test-group2", "239.0.0.2");
        let (device1_pk, device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
        // Add a second device for concurrent tunnels (Multicast + IBRL must go to different devices)
        let (device2_pk, device2) = fixture.add_device(DeviceType::Hybrid, 110, true);
        let user = fixture.create_user(UserType::Multicast, device1_pk, "1.2.3.4");
        fixture.expect_create_subscribe_user(
            Pubkey::new_unique(),
            &user,
            mcast_group_pk,
            false,
            true,
        );
        fixture.expected_provisioning_request(
            UserType::Multicast,
            user.client_ip.to_string().as_str(),
            device1.public_ip.to_string().as_str(),
            Some(vec![]),
            Some(vec![mcast_group.multicast_ip.to_string()]),
        );

        // Save device1's latency for delayed availability test
        let latency_record_device1 = fixture.latencies.lock().unwrap()[0].clone();
        let latency_record_device2 = fixture.latencies.lock().unwrap()[1].clone();
        fixture.latencies.lock().unwrap().clear();
        let latencies = Arc::clone(&fixture.latencies);

        let command = ProvisioningCliCommand {
            dz_mode: DzMode::Multicast {
                mode: Some(MulticastMode::Subscriber),
                multicast_groups: vec!["test-group".to_string()],
                pub_groups: vec![],
                sub_groups: vec![],
            },
            client_ip: Some(user.client_ip.to_string()),
            device: None,
            verbose: false,
        };

        let coro1 = command.execute_with_service_controller(&fixture.client, &fixture.controller);
        let coro2 = tokio::task::spawn(async move {
            tokio::time::sleep(Duration::from_secs(2)).await;
            let mut latencies = latencies.lock().unwrap();
            latencies.push(latency_record_device1);
            latencies.push(latency_record_device2);
        });

        let (result1, _) = tokio::join!(coro1, coro2);

        assert!(result1.is_ok());

        println!("Test that adding an IBRL tunnel with an existing multicast succeeds");
        // IBRL user should go to a DIFFERENT device than the existing Multicast user
        // (concurrent tunnels from same client IP must go to different devices)
        let ibrl_user = fixture.create_user(UserType::IBRL, device2_pk, "1.2.3.4");
        fixture.expect_create_user(Pubkey::new_unique(), &ibrl_user);

        fixture.expected_provisioning_request(
            UserType::IBRL,
            ibrl_user.client_ip.to_string().as_str(),
            device2.public_ip.to_string().as_str(),
            None,
            None,
        );

        let command = ProvisioningCliCommand {
            dz_mode: DzMode::IBRL {
                tenant: Some("test-tenant".to_string()),
                allocate_addr: false,
            },
            client_ip: Some(ibrl_user.client_ip.to_string()),
            device: None,
            verbose: false,
        };

        let result = command
            .execute_with_service_controller(&fixture.client, &fixture.controller)
            .await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_connect_to_device_at_max_users() {
        let mut fixture = TestFixture::new();

        // Add a device with max_users = 0
        let (device_pk, mut device) = fixture.add_device(DeviceType::Hybrid, 100, true);
        device.max_users = 0;
        device.users_count = 0;

        // Update the device in the mock
        fixture
            .devices
            .lock()
            .unwrap()
            .insert(device_pk, device.clone());

        println!();

        let command = ProvisioningCliCommand {
            dz_mode: DzMode::IBRL {
                tenant: Some("test-tenant".to_string()),
                allocate_addr: false,
            },
            client_ip: Some("1.2.3.4".to_string()),
            device: Some(device.code.clone()), // Explicitly specify the device
            verbose: false,
        };

        let result = command
            .execute_with_service_controller(&fixture.client, &fixture.controller)
            .await;

        assert!(result.is_err());
        let err_msg = result.unwrap_err().to_string();
        assert!(
            err_msg.contains("Device is not accepting more users"),
            "Expected error about device not accepting users, got: {}",
            err_msg
        );
        assert!(
            !err_msg.contains("Device not found"),
            "Should not show 'Device not found' error when device exists but is full"
        );
    }

    #[tokio::test]
    async fn test_connect_to_device_at_capacity() {
        let mut fixture = TestFixture::new();

        // Add a device that's at capacity (users_count >= max_users)
        let (device_pk, mut device) = fixture.add_device(DeviceType::Hybrid, 100, true);
        device.max_users = 10;
        device.users_count = 10; // At capacity

        // Update the device in the mock
        fixture
            .devices
            .lock()
            .unwrap()
            .insert(device_pk, device.clone());

        println!();

        let command = ProvisioningCliCommand {
            dz_mode: DzMode::IBRL {
                tenant: Some("test-tenant".to_string()),
                allocate_addr: false,
            },
            client_ip: Some("1.2.3.4".to_string()),
            device: Some(device.code.clone()), // Explicitly specify the device
            verbose: false,
        };

        let result = command
            .execute_with_service_controller(&fixture.client, &fixture.controller)
            .await;

        assert!(result.is_err());
        let err_msg = result.unwrap_err().to_string();
        assert!(
            err_msg.contains("Device is not accepting more users"),
            "Expected error about device not accepting users, got: {}",
            err_msg
        );
    }

    #[tokio::test]
    async fn test_connect_to_nonexistent_device() {
        let mut fixture = TestFixture::new();

        fixture.add_device(DeviceType::Hybrid, 100, true); // Add a device, but we'll try to connect to a different one

        println!();

        let command = ProvisioningCliCommand {
            dz_mode: DzMode::IBRL {
                tenant: Some("test-tenant".to_string()),
                allocate_addr: false,
            },
            client_ip: Some("1.2.3.4".to_string()),
            device: Some("nonexistent-device".to_string()), // Device that doesn't exist
            verbose: false,
        };

        let result = command
            .execute_with_service_controller(&fixture.client, &fixture.controller)
            .await;

        assert!(result.is_err());
        let err_msg = result.unwrap_err().to_string();
        assert!(
            err_msg.contains("Device not found"),
            "Expected 'Device not found' error for nonexistent device, got: {}",
            err_msg
        );
    }

    #[tokio::test]
    async fn test_connect_command_ibrl_allocate_resolve_route_failure() {
        let mut fixture = TestFixture::new();

        let (_device1_pk, device1) = fixture.add_device(DeviceType::Hybrid, 100, true);

        fixture.expect_resolve_route_failure(device1.public_ip);

        println!();

        let command = ProvisioningCliCommand {
            dz_mode: DzMode::IBRL {
                tenant: Some("test-tenant".to_string()),
                allocate_addr: true,
            },
            client_ip: Some("1.2.3.4".to_string()),
            device: None,
            verbose: false,
        };

        let result = command
            .execute_with_service_controller(&fixture.client, &fixture.controller)
            .await;

        assert!(result.is_err());
        let err_msg = result.unwrap_err().to_string();
        assert!(
            err_msg.contains("Unable to resolve route"),
            "Expected error about unable to resolve route, got: {}",
            err_msg
        );
    }

    #[tokio::test]
    async fn test_connect_command_ibrl_allocate_resolve_route_error() {
        let mut fixture = TestFixture::new();

        let (_device1_pk, device1) = fixture.add_device(DeviceType::Hybrid, 100, true);

        fixture.expect_resolve_route_error(device1.public_ip);

        println!();

        let command = ProvisioningCliCommand {
            dz_mode: DzMode::IBRL {
                tenant: Some("test-tenant".to_string()),
                allocate_addr: true,
            },
            client_ip: Some("1.2.3.4".to_string()),
            device: None,
            verbose: false,
        };

        let result = command
            .execute_with_service_controller(&fixture.client, &fixture.controller)
            .await;

        assert!(result.is_err());
        let err_msg = result.unwrap_err().to_string();
        assert!(
            err_msg.contains("Failed to resolve route"),
            "Expected error about failed to resolve route, got: {}",
            err_msg
        );
    }

    #[tokio::test]
    async fn test_connect_command_ibrl_allocate_existing_user() {
        let mut fixture = TestFixture::new();

        let (device1_pk, device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
        let mut user = fixture.create_user(UserType::IBRLWithAllocatedIP, device1_pk, "1.2.3.4");
        user.status = UserStatus::Activated;
        fixture.add_user(&user);

        let resolved_src = Ipv4Addr::new(192, 168, 1, 102);
        fixture.expect_resolve_route(device1.public_ip, resolved_src);

        fixture.expected_provisioning_request_with_tunnel_src(
            UserType::IBRLWithAllocatedIP,
            user.client_ip.to_string().as_str(),
            resolved_src.to_string().as_str(),
            device1.public_ip.to_string().as_str(),
            None,
            None,
        );

        println!();

        let command = ProvisioningCliCommand {
            dz_mode: DzMode::IBRL {
                tenant: Some("test-tenant".to_string()),
                allocate_addr: true,
            },
            client_ip: Some(user.client_ip.to_string()),
            device: None,
            verbose: false,
        };

        let result = command
            .execute_with_service_controller(&fixture.client, &fixture.controller)
            .await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_connect_command_ibrl_allocate_existing_user_resolve_failure() {
        let mut fixture = TestFixture::new();

        let (device1_pk, device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
        let mut user = fixture.create_user(UserType::IBRLWithAllocatedIP, device1_pk, "1.2.3.4");
        user.status = UserStatus::Activated;
        fixture.add_user(&user);

        // Mock resolve_route to return None for existing user
        fixture.expect_resolve_route_failure(device1.public_ip);

        println!();

        let command = ProvisioningCliCommand {
            dz_mode: DzMode::IBRL {
                tenant: Some("test-tenant".to_string()),
                allocate_addr: true,
            },
            client_ip: Some(user.client_ip.to_string()),
            device: None,
            verbose: false,
        };

        let result = command
            .execute_with_service_controller(&fixture.client, &fixture.controller)
            .await;

        assert!(result.is_err());
        let err_msg = result.unwrap_err().to_string();
        assert!(
            err_msg.contains("Unable to resolve route"),
            "Expected error about unable to resolve route, got: {}",
            err_msg
        );
    }

    /// Test that adding multicast to an existing IBRL user creates a separate Multicast user
    ///
    /// This test verifies that when multicast is added to an IP that already has an IBRL user,
    /// the system creates a new Multicast user (enabling concurrent unicast + multicast tunnels).
    #[tokio::test]
    async fn test_add_multicast_to_existing_ibrl_user() {
        let mut fixture = TestFixture::new();

        let (mcast_group_pk, mcast_group) = fixture.add_multicast_group("test-group", "239.0.0.1");
        let (device1_pk, _device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
        // Add a second device for concurrent tunnels (IBRL + Multicast must go to different devices)
        let (device2_pk, device2) = fixture.add_device(DeviceType::Hybrid, 110, true);

        let ibrl_user = fixture.create_user(UserType::IBRL, device1_pk, "1.2.3.4");
        let _ibrl_user_pk = fixture.add_user(&ibrl_user);

        // Expect create_subscribe_user to be called to create a NEW Multicast user on a DIFFERENT device
        // (concurrent tunnels from same client IP must go to different devices)
        let mcast_user = fixture.create_user(UserType::Multicast, device2_pk, "1.2.3.4");
        fixture.expect_create_subscribe_user(
            Pubkey::new_unique(),
            &mcast_user,
            mcast_group_pk,
            false, // publisher
            true,  // subscriber
        );

        fixture.expected_provisioning_request(
            UserType::Multicast,
            mcast_user.client_ip.to_string().as_str(),
            device2.public_ip.to_string().as_str(),
            Some(vec![]),
            Some(vec![mcast_group.multicast_ip.to_string()]),
        );

        let command = ProvisioningCliCommand {
            dz_mode: DzMode::Multicast {
                mode: Some(MulticastMode::Subscriber),
                multicast_groups: vec!["test-group".to_string()],
                pub_groups: vec![],
                sub_groups: vec![],
            },
            client_ip: Some(ibrl_user.client_ip.to_string()),
            device: None,
            verbose: false,
        };

        let result = command
            .execute_with_service_controller(&fixture.client, &fixture.controller)
            .await;
        assert!(
            result.is_ok(),
            "Adding multicast to existing IBRL user should succeed by creating separate Multicast user: {:?}",
            result.err()
        );
    }

    /// Test that multiple user types per IP are properly isolated
    ///
    /// This test verifies that:
    /// 1. A Multicast user exists for an IP
    /// 2. An IBRL user can be created for the same IP (different UserType = different PDA)
    /// 3. The two users are independent
    #[tokio::test]
    async fn test_multiple_user_types_per_ip_isolation() {
        let mut fixture = TestFixture::new();

        let (mcast_group_pk, _mcast_group) = fixture.add_multicast_group("test-group", "239.0.0.1");
        let (device1_pk, _device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
        // Add a second device for concurrent tunnels (IBRL + Multicast must go to different devices)
        let (device2_pk, _device2) = fixture.add_device(DeviceType::Hybrid, 110, true);

        // Create a pure Multicast user and add it to the fixture (simulating existing user)
        let mut mcast_user = fixture.create_user(UserType::Multicast, device1_pk, "1.2.3.4");
        mcast_user.subscribers.push(mcast_group_pk);
        let mcast_user_pk = fixture.add_user(&mcast_user);

        // Create an IBRL user for the same IP on a DIFFERENT device
        // (concurrent tunnels from same client IP must go to different devices)
        let ibrl_user = fixture.create_user(UserType::IBRL, device2_pk, "1.2.3.4");
        fixture.expect_create_user(Pubkey::new_unique(), &ibrl_user);

        // Allow any provisioning call
        fixture
            .controller
            .expect_provisioning()
            .returning_st(move |_| {
                Ok(ProvisioningResponse {
                    status: "success".to_string(),
                    description: None,
                })
            });

        let command = ProvisioningCliCommand {
            dz_mode: DzMode::IBRL {
                tenant: Some("test-tenant".to_string()),
                allocate_addr: false,
            },
            client_ip: Some(ibrl_user.client_ip.to_string()),
            device: None,
            verbose: false,
        };

        let result = command
            .execute_with_service_controller(&fixture.client, &fixture.controller)
            .await;
        assert!(
            result.is_ok(),
            "IBRL user creation should succeed even with existing Multicast user for same IP: {:?}",
            result.err()
        );

        // Verify both users exist for the same IP
        let users = fixture.users.lock().unwrap();
        let users_for_ip: Vec<_> = users
            .values()
            .filter(|u| u.client_ip == ibrl_user.client_ip)
            .collect();
        assert_eq!(
            users_for_ip.len(),
            2,
            "Should have 2 users for the same IP (one IBRL, one Multicast), mcast_pk={}, users={:?}",
            mcast_user_pk,
            users_for_ip.iter().map(|u| u.user_type).collect::<Vec<_>>()
        );
    }
}
