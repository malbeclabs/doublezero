use colored::Colorize;
use eyre;
use indicatif::ProgressBar;
use std::str::FromStr;

use clap::{Args, ValueEnum};
use doublezero_sdk::{
    ipv4_parse, ipv4_to_string, networkv4_to_string, DZClient, IpV4, NetworkV4,
    ProvisioningRequest, ServiceController, User, UserCYOA, UserStatus, UserType,
};

use doublezero_sdk::commands::device::get::GetDeviceCommand;
use doublezero_sdk::commands::device::list::ListDeviceCommand;
use doublezero_sdk::commands::user::create::CreateUserCommand;
use doublezero_sdk::commands::user::get::GetUserCommand;
use doublezero_sdk::commands::user::list::ListUserCommand;

use solana_sdk::pubkey::Pubkey;

use crate::{
    helpers::get_public_ipv4,
    requirements::{
        check_requirements, CHECK_BALANCE, CHECK_DOUBLEZEROD, CHECK_ID_JSON, CHECK_USER_ALLOWLIST,
    },
};

use super::helpers::init_command;

#[derive(Clone, Debug, ValueEnum)]
pub enum DzMode {
    IBRL,
    //EdgeFiltering,
    //Multicast,
}

#[derive(Args, Debug)]
pub struct ProvisioningArgs {
    #[arg(value_enum)]
    pub dz_mode: DzMode,
    #[arg(long)]
    pub device: Option<String>,
    #[arg(long)]
    pub client_ip: Option<String>,
    #[arg(short, long, default_value_t = false)]
    pub allocate_addr: bool,
    #[arg(short, long, default_value_t = false)]
    pub verbose: bool,
}

impl ProvisioningArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {
        let spinner = init_command();
        let controller = ServiceController::new(None);

        // Check requirements
        check_requirements(
            client,
            Some(&spinner),
            CHECK_ID_JSON | CHECK_BALANCE | CHECK_USER_ALLOWLIST | CHECK_DOUBLEZEROD,
        )?;
        // Get public IP
        let client_ip = self.look_for_ip(&spinner).await?;

        // Look for user
        let (user_pubkey, user) = self.look_for_user(client, &controller, &client_ip, &spinner).await?;

        // Check user status
        match user.status {
            UserStatus::Activated => {
                // User is activated
                self.user_activated(client, &controller, &user, &client_ip, &spinner)
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

    fn get_user_type(&self) -> UserType {
        match self.dz_mode {
            DzMode::IBRL => {
                if self.allocate_addr {
                    UserType::IBRLWithAllocatedIP
                } else {
                    UserType::IBRL
                }
            } //DzMode::EdgeFiltering => UserType::EdgeFiltering,
              //DzMode::Multicast => UserType::Multicast,
        }
    }

    pub  async fn look_for_ip(&self, spinner: &ProgressBar) -> eyre::Result<IpV4> {
        spinner.println("🔗  Start Provisioning User...");
        spinner.set_prefix("1/4 Public IP");

        // Get public IP from command line or from the internet
        let client_ip = match self.client_ip.as_ref() {
            Some(ip) => {
                spinner.println(format!("    Using Public IP: {}", ip));

                ip
            }
            None => &{
                spinner.set_message("Searching for Public IP...");

                match get_public_ipv4() {
                    Ok(ip) => {
                        spinner.println(format!("    Get your Public IP: {} (If you want to specify a particular address, use the argument --client-ip x.x.x.x)", ip));
                        ip
                    }
                    Err(e) => {
                        return Err(eyre::eyre!("I couldn't retrieve your Public IP. Please provide it using the `--client-ip` argument. ({})", e.to_string()));
                    }
                }
            },
        };

        spinner.println(format!("🔍  Provisioning User for IP: {}", client_ip));

        let client_ip = ipv4_parse(client_ip);
        Ok(client_ip)
    }

    #[allow(unused_assignments)]
    pub async fn look_for_user(
        &self,
        client: &DZClient,
        controller: &ServiceController,
        client_ip: &IpV4,
        spinner: &ProgressBar,
    ) -> eyre::Result<(Pubkey, User)> {
        spinner.set_message("Searching for user account...");
        spinner.set_prefix("2/4 User");

        let users = ListUserCommand {}.execute(client)?;
        let devices = ListDeviceCommand {}.execute(client)?;

        let mut user_pubkey: Option<Pubkey> = None;

        match users.iter().find(|(_, u)| u.client_ip == *client_ip) {
            Some((pubkey, _user)) => {
                spinner.println(format!("    An account already exists Pubkey: {}", pubkey));

                user_pubkey = Some(*pubkey);
            }
            None => {
                spinner.println(format!(
                    "    Creating an account for the IP: {}",
                    ipv4_to_string(client_ip)
                ));

                let device_pk = match self.device.as_ref() {
                    Some(device) => match device.parse::<Pubkey>() {
                        Ok(pubkey) => pubkey,
                        Err(_) => {
                            spinner.set_message("Searching for device account...");
                            let (pubkey, _) = devices
                                .iter()
                                .find(|(_, d)| d.code == *device)
                                .expect("Device not found");
                            *pubkey
                        }
                    },
                    None => {
                        spinner.set_message("Reading latency stats...");
                        let mut latencies =
                            controller.latency().await.expect("Could not get latency");
                        latencies.retain(|l| l.reachable);
                        latencies.sort_by(|a, b| a.avg_latency_ns.cmp(&b.avg_latency_ns));

                        spinner.set_message("Searching for device account...");
                        Pubkey::from_str(&latencies.first().expect("No devices found").device_pk)
                            .expect("Unable to parse pubkey")
                    }
                };

                let (_, device) = GetDeviceCommand {
                    pubkey_or_code: device_pk.to_string(),
                }
                .execute(client)
                .expect("Unable to get device");

                spinner.println(format!(
                    "    The Device has been selected: {} ",
                    device.code
                ));
                spinner.set_prefix("🔗 [3/4] User");

                let res = CreateUserCommand {
                    user_type: self.get_user_type(),
                    device_pk,
                    cyoa_type: UserCYOA::GREOverDIA,
                    client_ip: *client_ip,
                }
                .execute(client);

                match res {
                    Ok((_, pubkey)) => {
                        spinner.set_message("User created");

                        user_pubkey = Some(pubkey);
                    }
                    Err(e) => {
                        spinner.finish_with_message("Error creating user");
                        spinner.println(format!("\n{}: {:?}\n", "Error".red().bold(), e));

                        return Err(eyre::eyre!("Error creating user"));
                    }
                };

                spinner.set_message("User created");
            }
        };

        if user_pubkey.is_none() {
            spinner.finish_with_message("Error creating user");
            return Err(eyre::eyre!("Error creating user"));
        }

        let (_, mut user) = GetUserCommand {
            pubkey: user_pubkey.unwrap(),
        }
        .execute(client)
        .expect("User not found");
        while user.status != UserStatus::Activated && user.status != UserStatus::Rejected {
            spinner.set_message("Waiting for user activation...");
            std::thread::sleep(std::time::Duration::from_secs(5));
            let (_, updated_user) = GetUserCommand {
                pubkey: user_pubkey.unwrap(),
            }
            .execute(client)
            .expect("User not found");
            user = updated_user.clone();
        }

        Ok((user_pubkey.unwrap(), user.clone()))
    }

    async fn user_activated(
        &self,
        client: &DZClient,
        controller: &ServiceController,
        user: &User,
        client_ip: &IpV4,
        spinner: &ProgressBar,
    ) -> eyre::Result<()> {
        spinner.println(format!(
            "    User activated with dz_ip: {}",
            ipv4_to_string(&user.dz_ip)
        ));

        spinner.set_prefix("3/4 Device");
        spinner.set_message("Reading devices...");

        let devices = ListDeviceCommand {}.execute(client)?;
        let prefixes = devices
            .values()
            .flat_map(|device| device.dz_prefixes.clone())
            .collect::<Vec<NetworkV4>>();

        spinner.set_message("Getting global-config...");
        let (_, config) = client.get_globalconfig().expect("Unable to get config");

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
                "➤   Provisioning Local Tunnel for IP: {}\n\ttunnel_src: {}\n\ttunnel_dst: {}\n\ttunnel_net: {}\n\tdoublezero_ip: {}\n\tdoublezero_prefixes: {:?}\n\tlocal_asn: {}\n\tremote_asn: {}\n",
                ipv4_to_string(client_ip),
                tunnel_src,
                tunnel_dst,
                tunnel_net,
                doublezero_ip,
                doublezero_prefixes,
                config.local_asn,
                config.remote_asn,
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
                user_type: self.get_user_type().to_string(),
            })
            .await
        {
            Ok(res) => {
                spinner.println(format!("Provisioning: status: {}", res.status));
                spinner.finish_with_message("User Provisioned");
            }
            Err(e) => {
                spinner.finish_with_message("Error provisioning user");
                spinner.println(format!("\n{}: {:?}\n", "Error".red().bold(), e));
            }
        };

        Ok(())
    }

    async fn user_rejected(
        &self,
        client: &DZClient,
        user_pubkey: &Pubkey,
        spinner: &ProgressBar,
    ) -> eyre::Result<()> {
        spinner.println(format!("    {}", "User rejected".red()));

        spinner.set_message("Reading logs...");
        std::thread::sleep(std::time::Duration::from_secs(10));
        let msgs = client.get_logs(user_pubkey).expect("Unable to get logs");

        for mut msg in msgs {
            if msg.starts_with("Program log: Error: ") {
                spinner.println(format!("    {}", msg.split_off(20).red()));
            }
        }

        Ok(())
    }
}
