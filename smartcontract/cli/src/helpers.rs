use std::io::{Read, Write};
use std::str;

use std::str::FromStr;
use std::time::Duration;
use colored::Colorize;
use indicatif::{ProgressBar, ProgressStyle};
use solana_sdk::pubkey::Pubkey;
use std::net::{TcpStream, ToSocketAddrs};

pub fn parse_pubkey(input: &str) -> Option<Pubkey> {
    if input.len() < 43 || input.len() > 44 {
        return None;
    }

    match Pubkey::from_str(input) {
        Ok(pk) => Some(pk),
        Err(_) => None,
    }
}

pub fn print_error(e: eyre::Report) {
    eprintln!("\n{}: {:?}\n", "Error".red().bold(), e);
}

pub fn get_public_ipv4() -> Result<String, Box<dyn std::error::Error>> {
    // Resolve the host `ifconfig.me` to IPv4 addresses
    let addrs = "ifconfig.me:80"
        .to_socket_addrs()?
        .filter_map(|addr| match addr {
            std::net::SocketAddr::V4(ipv4) => Some(ipv4),
            _ => None,
        })
        .next()
        .ok_or("Failed to resolve an IPv4 address")?;

    // Establish a connection to the IPv4 address
    let mut stream = TcpStream::connect(addrs)?;

    // Send an HTTP GET request to retrieve only IPv4
    let request = "GET /ip HTTP/1.1\r\nHost: ifconfig.me\r\nConnection: close\r\n\r\n";
    stream.write_all(request.as_bytes())?;

    // Read the response from the server
    let mut response = Vec::new();
    stream.read_to_end(&mut response)?;

    // Convert the response to text and find the body of the response
    let response_text = str::from_utf8(&response)?;

    // The IP will be in the body after the HTTP headers
    if let Some(body_start) = response_text.find("\r\n\r\n") {
        let ip = &response_text[body_start + 4..].trim();

        return Ok(ip.to_string());
    }

    Err("Failed to extract the IP from the response".into())
}


pub fn init_command() -> ProgressBar {
    let spinner = ProgressBar::new_spinner();

    spinner.set_style(
        ProgressStyle::default_spinner()
            .template("{spinner:.green}  {msg}")
            .expect("Failed to set template")
            .tick_strings(&["-", "\\", "|", "/"]),
    );
    spinner.enable_steady_tick(Duration::from_millis(100));

    spinner.println("DoubleZero Service Provisioning");

    spinner
}