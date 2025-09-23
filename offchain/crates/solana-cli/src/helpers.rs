use std::{
    io::{Read, Write},
    net::{SocketAddr, TcpStream, ToSocketAddrs},
    time::Duration,
};

use anyhow::{Context, bail};

pub fn get_public_ipv4() -> anyhow::Result<String> {
    // Resolve the host `ifconfig.me` to IPv4 addresses
    let socket_addr = "ifconfig.me:80"
        .to_socket_addrs()?
        .find(|addr| matches!(addr, SocketAddr::V4(_)))
        .context("Failed to resolve an IPv4 address")?;

    // Establish a connection to the IPv4 address with a short timeout to avoid hanging CLI calls.
    let mut stream = TcpStream::connect_timeout(&socket_addr, Duration::from_secs(5))?;
    stream.set_read_timeout(Some(Duration::from_secs(5)))?;

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

    bail!("Failed to extract the IP from the response")
}
