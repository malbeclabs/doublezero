use doublezero_cli::helpers::get_public_ipv4;
use indicatif::ProgressBar;
use std::net::Ipv4Addr;

pub async fn look_for_ip(
    client_ip: &Option<String>,
    spinner: &ProgressBar,
) -> eyre::Result<(Ipv4Addr, String)> {
    let client_ip = match client_ip {
        Some(ip) => {
            spinner.println(format!("    Using Public IP: {ip}"));
            ip
        }
        None => &{
            spinner.set_message("Discovering your public IP...");

            match get_public_ipv4() {
                Ok(ip) => {
                    spinner.println(format!("Public IP detected: {ip} - If you want to use a different IP, you can specify it with `--client-ip x.x.x.x`"));
                    ip
                }
                Err(e) => {
                    eyre::bail!("Could not detect your public IP. Please provide the `--client-ip` argument. ({})", e.to_string());
                }
            }
        },
    };

    let ip: Ipv4Addr = client_ip
        .parse()
        .map_err(|_| eyre::eyre!("Invalid IPv4 address format: {}", client_ip))?;

    Ok((ip, client_ip.to_string()))
}
