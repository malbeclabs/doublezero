use super::helpers::look_for_ip;
use crate::{
    dzd_latency::best_latency,
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
    helpers::init_command,
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
        /// Allocate a new address for the user
        #[arg(short, long, default_value_t = false)]
        allocate_addr: bool,
    },
    //EdgeFiltering,
    /// Provision a user in Multicast mode
    Multicast {
        /// Multicast mode: Publisher or Subscriber
        #[arg(value_enum)]
        mode: MulticastMode,

        /// Multicast group code(s)
        #[arg(required = true, num_args = 1..)]
        multicast_groups: Vec<String>,
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

        match self.parse_dz_mode() {
            (UserType::IBRL, _, _) => {
                self.execute_ibrl(client, controller, UserType::IBRL, client_ip, &spinner)
                    .await
            }
            (UserType::IBRLWithAllocatedIP, _, _) => {
                self.execute_ibrl(
                    client,
                    controller,
                    UserType::IBRLWithAllocatedIP,
                    client_ip,
                    &spinner,
                )
                .await
            }
            (UserType::Multicast, Some(multicast_mode), Some(multicast_groups)) => {
                self.execute_multicast(
                    client,
                    controller,
                    multicast_mode,
                    multicast_groups,
                    client_ip,
                    &spinner,
                )
                .await
            }
            _ => eyre::bail!("DzMode not supported"),
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
        spinner: &ProgressBar,
    ) -> eyre::Result<()> {
        // Look for user
        let (user_pubkey, user, tunnel_src) = self
            .find_or_create_user(client, controller, &client_ip, spinner, user_type)
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
        multicast_mode: &MulticastMode,
        multicast_groups: &Vec<String>,
        client_ip: Ipv4Addr,
        spinner: &ProgressBar,
    ) -> eyre::Result<()> {
        let mcast_groups = client.list_multicastgroup(ListMulticastGroupCommand)?;

        // Resolve all group codes to pubkeys
        let mut mcast_group_pks = Vec::new();
        for group_code in multicast_groups {
            let (pk, _) = mcast_groups
                .iter()
                .find(|(_, g)| g.code == *group_code)
                .ok_or_else(|| eyre::eyre!("Multicast group not found: {}", group_code))?;
            if mcast_group_pks.contains(pk) {
                eyre::bail!("Duplicate multicast group: {}", group_code);
            }
            mcast_group_pks.push(*pk);
        }

        // Look for user and subscribe to all groups
        let (user_pubkey, user) = self
            .find_or_create_user_and_subscribe(
                client,
                controller,
                &client_ip,
                spinner,
                multicast_mode,
                &mcast_group_pks,
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

        // Check user status
        match user.status {
            UserStatus::Activated => {
                // User is activated
                self.user_activated(
                    client,
                    controller,
                    &user,
                    &client_ip,
                    spinner,
                    UserType::Multicast,
                    Some(mcast_pub_groups),
                    Some(mcast_sub_groups),
                )
                .await?
            }
            UserStatus::Rejected => {
                // User is rejected
                self.user_rejected(client, &user_pubkey, spinner).await?;
            }
            _ => eyre::bail!("User status not expected"),
        }

        // Finish
        Ok(())
    }

    fn parse_dz_mode(&self) -> (UserType, Option<&MulticastMode>, Option<&Vec<String>>) {
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
                multicast_groups,
            } => (UserType::Multicast, Some(mode), Some(multicast_groups)),
        }
    }

    async fn find_or_create_device<T: ServiceController>(
        &self,
        client: &dyn CliCommand,
        controller: &T,
        devices: &HashMap<Pubkey, Device>,
        spinner: &ProgressBar,
    ) -> eyre::Result<(Pubkey, Device)> {
        spinner.set_message("Searching for the nearest device...");

        let device_pk = match self.device.as_ref() {
            Some(device) => match device.parse::<Pubkey>() {
                Ok(pubkey) => pubkey,
                Err(_) => {
                    let (pubkey, _) = devices
                        .iter()
                        .find(|(_, d)| d.code == *device)
                        .ok_or(eyre::eyre!("Device not found"))?;
                    *pubkey
                }
            },
            None => {
                let latency = best_latency(controller, devices, true, Some(spinner), None).await?;
                spinner.set_message("Reading device account...");
                Pubkey::from_str(&latency.device_pk)
                    .map_err(|_| eyre::eyre!("Unable to parse pubkey"))?
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

        Ok((device_pk, device))
    }

    async fn find_or_create_user<T: ServiceController>(
        &self,
        client: &dyn CliCommand,
        controller: &T,
        client_ip: &Ipv4Addr,
        spinner: &ProgressBar,
        user_type: UserType,
    ) -> eyre::Result<(Pubkey, User, Ipv4Addr)> {
        spinner.set_message("Searching for user account...");
        spinner.inc(1);

        let users = client.list_user(ListUserCommand)?;
        let mut devices = client.list_device(ListDeviceCommand)?;

        // Only filter devices if auto-selecting; keep all if user specified a device
        if self.device.is_none() {
            devices.retain(|_, d| d.is_device_eligible_for_provisioning());
        }

        let matched_users = users
            .iter()
            .filter(|(_, u)| u.client_ip == *client_ip)
            .collect::<Vec<_>>();

        if matched_users.len() > 1 {
            // invariant, this indicates a bug that should never happen
            panic!("❌ Multiple tunnels found for the same IP address. This should not happen.");
        }

        let mut tunnel_src = *client_ip;

        let user_pubkey = match matched_users.first() {
            Some((pubkey, user)) => {
                if user.user_type != UserType::IBRL
                    && user.user_type != UserType::IBRLWithAllocatedIP
                {
                    spinner.println(format!(
                        "❌  User with IP {} already exists with type {:?}. Expected type {:?}. Only one tunnel currently supported.",
                        client_ip,
                        user.user_type,
                        user_type
                    ));
                    eyre::bail!("User with different type already exists. Only one tunnel currently supported.");
                }

                spinner.println(format!("    An account already exists Pubkey: {pubkey}"));

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

                **pubkey
            }
            None => {
                spinner.println("    Creating user account...");

                let (device_pk, device) = self
                    .find_or_create_device(client, controller, &devices, spinner)
                    .await?;

                spinner.println(format!("    Device selected: {} ", device.code));
                spinner.inc(1);

                if user_type == UserType::IBRLWithAllocatedIP {
                    tunnel_src = resolve_tunnel_src(controller, &device).await?;
                }

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
        multicast_mode: &MulticastMode,
        mcast_group_pks: &[Pubkey],
    ) -> eyre::Result<(Pubkey, User)> {
        spinner.set_message("Searching for user account...");
        spinner.inc(1);

        let users = client.list_user(ListUserCommand)?;
        let mut devices = client.list_device(ListDeviceCommand)?;

        // Only filter devices if auto-selecting; keep all if user specified a device
        if self.device.is_none() {
            devices.retain(|_, d| d.is_device_eligible_for_provisioning());
        }

        let matched_users = users
            .iter()
            .filter(|(_, u)| u.client_ip == *client_ip)
            .collect::<Vec<_>>();

        if matched_users.len() > 1 {
            // invariant, this indicates a bug that should never happen
            panic!("Multiple tunnels found for the same IP address. This should not happen.");
        }

        let (publisher, subscriber) = match multicast_mode {
            MulticastMode::Publisher => (true, false),
            MulticastMode::Subscriber => (false, true),
        };

        let user_pubkey = match matched_users.first() {
            Some((user_pk, user)) => {
                if user.user_type != UserType::Multicast {
                    spinner.println(format!(
                        "❌  User with IP {} already exists with type {:?}. Expected type {:?}. Only one tunnel currently supported.",
                        client_ip,
                        user.user_type,
                        UserType::Multicast
                    ));
                    eyre::bail!("User with different type already exists. Only one tunnel currently supported.");
                }

                // Subscribe to any groups not already subscribed
                let existing_groups = match multicast_mode {
                    MulticastMode::Publisher => &user.publishers,
                    MulticastMode::Subscriber => &user.subscribers,
                };

                for mcast_group_pk in mcast_group_pks {
                    if !existing_groups.contains(mcast_group_pk) {
                        spinner.println(format!("    Subscribing to group: {mcast_group_pk}"));
                        client.subscribe_multicastgroup(SubscribeMulticastGroupCommand {
                            user_pk: **user_pk,
                            group_pk: *mcast_group_pk,
                            client_ip: *client_ip,
                            publisher,
                            subscriber,
                        })?;
                    }
                }

                **user_pk
            }
            None => {
                spinner.println(format!("    Creating an account for the IP: {client_ip}"));

                let (device_pk, device) = self
                    .find_or_create_device(client, controller, &devices, spinner)
                    .await?;

                spinner.println(format!(
                    "    The Device has been selected: {} ",
                    device.code
                ));
                spinner.inc(1);

                // Create user with first group
                let first_group_pk = mcast_group_pks
                    .first()
                    .ok_or_else(|| eyre::eyre!("At least one multicast group is required"))?;

                let res = client.create_subscribe_user(CreateSubscribeUserCommand {
                    user_type: UserType::Multicast,
                    device_pk,
                    cyoa_type: UserCYOA::GREOverDIA,
                    client_ip: *client_ip,
                    mgroup_pk: *first_group_pk,
                    publisher,
                    subscriber,
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
                if mcast_group_pks.len() > 1 {
                    self.poll_for_user_activated(client, &user_pk, spinner)?;
                }

                // Subscribe to remaining groups
                for mcast_group_pk in mcast_group_pks.iter().skip(1) {
                    spinner.println(format!("    Subscribing to group: {mcast_group_pk}"));
                    client.subscribe_multicastgroup(SubscribeMulticastGroupCommand {
                        user_pk,
                        group_pk: *mcast_group_pk,
                        client_ip: *client_ip,
                        publisher,
                        subscriber,
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

        // Tunnel provisioning
        let tunnel_dst = device.public_ip.to_string();
        let tunnel_net = user.tunnel_net.to_string();
        let doublezero_ip = &user.dz_ip;
        let doublezero_prefixes: Vec<String> =
            prefixes.into_iter().map(|net| net.to_string()).collect();

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
                tunnel_dst,
                tunnel_net,
                doublezero_ip: doublezero_ip.to_string(),
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
    }

    impl TestFixture {
        pub fn new() -> Self {
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
                dz_prefixes: "10.0.0.0/24".parse().unwrap(),
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

        pub fn create_user(
            &mut self,
            user_type: UserType,
            device_pk: Pubkey,
            client_ip: &str,
        ) -> User {
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
            let expected_create_user_command = CreateUserCommand {
                user_type: user.user_type,
                device_pk: user.device_pk,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: user.client_ip,
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

        pub fn expect_subscribe_multicastgroup(
            &mut self,
            user_pk: Pubkey,
            user: &User,
            mcast_group_pk: Pubkey,
            publisher: bool,
            subscriber: bool,
        ) {
            let expected_command = SubscribeMulticastGroupCommand {
                user_pk,
                group_pk: mcast_group_pk,
                client_ip: user.client_ip,
                publisher,
                subscriber,
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
                .expect_subscribe_multicastgroup()
                .times(1)
                .with(predicate::eq(expected_command))
                .returning_st(move |_| {
                    thread::sleep(Duration::from_secs(1));
                    users.lock().unwrap().insert(user_pk, user.clone());
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
                doublezero_prefixes: vec!["10.0.0.0/24".to_string()],
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

        let (device1_pk, device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
        let user = fixture.create_user(UserType::IBRL, device1_pk, "1.2.3.4");
        fixture.expect_create_user(Pubkey::new_unique(), &user);
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

        println!("Test that adding an IBRL tunnel with an existing multicast fails");
        // Test that adding an IBRL tunnel with an existing multicast fails
        (_, _) = fixture.add_multicast_group("test-group", "239.0.0.1");

        let command = ProvisioningCliCommand {
            dz_mode: DzMode::Multicast {
                mode: MulticastMode::Publisher,
                multicast_groups: vec!["test-group".to_string()],
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
            .contains("Only one tunnel currently supported"));
    }

    #[tokio::test]
    async fn test_connect_command_ibrl_edge() {
        let mut fixture = TestFixture::new();

        let (device1_pk, device1) = fixture.add_device(DeviceType::Edge, 100, true);
        let user = fixture.create_user(UserType::IBRL, device1_pk, "1.2.3.4");
        fixture.expect_create_user(Pubkey::new_unique(), &user);
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

        println!("Test that adding an IBRL tunnel with an existing multicast fails");
        // Test that adding an IBRL tunnel with an existing multicast fails
        (_, _) = fixture.add_multicast_group("test-group", "239.0.0.1");

        let command = ProvisioningCliCommand {
            dz_mode: DzMode::Multicast {
                mode: MulticastMode::Publisher,
                multicast_groups: vec!["test-group".to_string()],
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
            .contains("Only one tunnel currently supported"));
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

        let (device1_pk, device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
        let user = fixture.create_user(UserType::IBRLWithAllocatedIP, device1_pk, "1.2.3.4");
        fixture.expect_create_user(Pubkey::new_unique(), &user);

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

        let (device1_pk, device1) = fixture.add_device(DeviceType::Edge, 100, true);
        let user = fixture.create_user(UserType::IBRLWithAllocatedIP, device1_pk, "1.2.3.4");
        fixture.expect_create_user(Pubkey::new_unique(), &user);

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
                mode: MulticastMode::Publisher,
                multicast_groups: vec!["test-group".to_string()],
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
            &user,
            mcast_group2_pk,
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
                mode: multicast_mode,
                multicast_groups: vec!["test-group2".to_string()],
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
                mode: MulticastMode::Publisher,
                // Pass the same group twice — should error
                multicast_groups: vec!["test-group".to_string(), "test-group".to_string()],
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
            .contains("Duplicate multicast group"));
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
                mode: multicast_mode,
                multicast_groups: vec!["test-group".to_string()],
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
                mode: MulticastMode::Subscriber,
                multicast_groups: vec!["test-group".to_string()],
            },
            client_ip: Some(user.client_ip.to_string()),
            device: None,
            verbose: false,
        };

        let result = command
            .execute_with_service_controller(&fixture.client, &fixture.controller)
            .await;
        assert!(result.is_ok());

        // Test that adding an IBRL tunnel with an existing multicast fails
        let command = ProvisioningCliCommand {
            dz_mode: DzMode::IBRL {
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
        assert!(result
            .unwrap_err()
            .to_string()
            .contains("Only one tunnel currently supported"));
    }

    #[tokio::test]
    async fn test_connect_command_delayed_latencies() {
        let mut fixture = TestFixture::new();

        let (mcast_group_pk, mcast_group) = fixture.add_multicast_group("test-group", "239.0.0.1");
        (_, _) = fixture.add_multicast_group("test-group2", "239.0.0.2");
        let (device1_pk, device1) = fixture.add_device(DeviceType::Hybrid, 100, true);
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

        let latency_record = fixture.latencies.lock().unwrap()[0].clone();
        fixture.latencies.lock().unwrap().clear();
        let latencies = Arc::clone(&fixture.latencies);

        let command = ProvisioningCliCommand {
            dz_mode: DzMode::Multicast {
                mode: MulticastMode::Subscriber,
                multicast_groups: vec!["test-group".to_string()],
            },
            client_ip: Some(user.client_ip.to_string()),
            device: None,
            verbose: false,
        };

        let coro1 = command.execute_with_service_controller(&fixture.client, &fixture.controller);
        let coro2 = tokio::task::spawn(async move {
            tokio::time::sleep(Duration::from_secs(2)).await;
            latencies.lock().unwrap().push(latency_record);
        });

        let (result1, _) = tokio::join!(coro1, coro2);

        assert!(result1.is_ok());

        println!("Test that adding an IBRL tunnel with an existing multicast fails");
        // Test that adding an IBRL tunnel with an existing multicast fails
        let command = ProvisioningCliCommand {
            dz_mode: DzMode::IBRL {
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
        assert!(result
            .unwrap_err()
            .to_string()
            .contains("Only one tunnel currently supported"));
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
}
