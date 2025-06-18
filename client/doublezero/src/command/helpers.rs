use doublezero_cli::helpers::get_public_ipv4;
use doublezero_sdk::{ipv4_parse, IpV4};
use indicatif::ProgressBar;

pub async fn look_for_ip(
    client_ip: &Option<String>,
    spinner: &ProgressBar,
) -> eyre::Result<(IpV4, String)> {
    let client_ip = match client_ip {
        Some(ip) => {
            spinner.println(format!("    Using Public IP: {ip}"));
            ip
        }
        None => &{
            spinner.set_message("Searching for Public IP...");

            match get_public_ipv4() {
                Ok(ip) => {
                    spinner.println(format!("Public IP: {ip} (If you want to specify a particular address, use the argument --client-ip x.x.x.x)"));
                    ip
                }
                Err(e) => {
                    eyre::bail!("Error getting public ip. Please provide it using the `--client-ip` argument. ({})", e.to_string());
                }
            }
        },
    };

    let ip = ipv4_parse(client_ip)
        .map_err(|_| eyre::eyre!("Invalid IPv4 address format: {}", client_ip))?;

    Ok((ip, client_ip.to_string()))
}
