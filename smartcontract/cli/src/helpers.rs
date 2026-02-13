use std::{
    io::{Read, Write},
    str,
};

use indicatif::{ProgressBar, ProgressStyle};
use solana_sdk::pubkey::Pubkey;
use std::{
    net::{Ipv4Addr, TcpStream, ToSocketAddrs, UdpSocket},
    str::FromStr,
    time::Duration,
};

pub fn parse_pubkey(input: &str) -> Option<Pubkey> {
    if input.len() < 43 || input.len() > 44 {
        return None;
    }

    Pubkey::from_str(input).ok()
}

pub fn print_error(e: eyre::Report) {
    eprintln!("\nError: {e:?}\n");
}

pub fn get_public_ipv4() -> Result<String, Box<dyn std::error::Error>> {
    // Try the HTTP approach first; fall back to the local route-table approach.
    get_public_ipv4_via_http().or_else(|_| get_public_ipv4_via_route())
}

fn get_public_ipv4_via_http() -> Result<String, Box<dyn std::error::Error>> {
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
        let ip = response_text[body_start + 4..].trim();
        ip.parse::<Ipv4Addr>()
            .map(|_| ip.to_string())
            .map_err(|_| "Failed to parse IPv4 address from response".into())
    } else {
        Err("Failed to extract the IP from the response".into())
    }
}

fn get_public_ipv4_via_route() -> Result<String, Box<dyn std::error::Error>> {
    // Open a UDP socket; connecting doesn't send traffic but lets the OS
    // pick the outbound interface/address via the local routing table.
    let socket = UdpSocket::bind("0.0.0.0:0")?;
    socket.connect("1.1.1.1:80")?;

    let local_addr = socket.local_addr()?;
    let ip = match local_addr {
        std::net::SocketAddr::V4(addr) => *addr.ip(),
        _ => return Err("Local address is not IPv4".into()),
    };

    if is_public_ipv4(ip) {
        Ok(ip.to_string())
    } else {
        Err(format!("Local address {ip} is not a public IPv4 address").into())
    }
}

/// Returns `true` when `ip` is a globally routable (public) IPv4 address.
///
/// Every IANA-reserved, private, loopback, link-local, multicast, and
/// documentation/test range is rejected.
pub fn is_public_ipv4(ip: Ipv4Addr) -> bool {
    let octets = ip.octets();

    // 0.0.0.0/8 — "this network" (RFC 1122)
    if octets[0] == 0 {
        return false;
    }
    // 10.0.0.0/8 — RFC 1918 private
    if octets[0] == 10 {
        return false;
    }
    // 100.64.0.0/10 — CGNAT (RFC 6598)
    if octets[0] == 100 && (octets[1] & 0xC0) == 64 {
        return false;
    }
    // 127.0.0.0/8 — loopback
    if octets[0] == 127 {
        return false;
    }
    // 169.254.0.0/16 — link-local
    if octets[0] == 169 && octets[1] == 254 {
        return false;
    }
    // 172.16.0.0/12 — RFC 1918 private
    if octets[0] == 172 && (octets[1] & 0xF0) == 16 {
        return false;
    }
    // 192.0.0.0/24 — IETF protocol assignments (RFC 6890)
    if octets[0] == 192 && octets[1] == 0 && octets[2] == 0 {
        return false;
    }
    // 192.0.2.0/24 — TEST-NET-1 (RFC 5737)
    if octets[0] == 192 && octets[1] == 0 && octets[2] == 2 {
        return false;
    }
    // 192.168.0.0/16 — RFC 1918 private
    if octets[0] == 192 && octets[1] == 168 {
        return false;
    }
    // 198.18.0.0/15 — benchmarking (RFC 2544)
    if octets[0] == 198 && (octets[1] & 0xFE) == 18 {
        return false;
    }
    // 198.51.100.0/24 — TEST-NET-2 (RFC 5737)
    if octets[0] == 198 && octets[1] == 51 && octets[2] == 100 {
        return false;
    }
    // 203.0.113.0/24 — TEST-NET-3 (RFC 5737)
    if octets[0] == 203 && octets[1] == 0 && octets[2] == 113 {
        return false;
    }
    // 224.0.0.0/4 — multicast
    if (octets[0] & 0xF0) == 224 {
        return false;
    }
    // 240.0.0.0/4 — reserved/future (includes 255.255.255.255)
    if (octets[0] & 0xF0) == 240 {
        return false;
    }

    true
}

pub fn init_command(len: u64) -> ProgressBar {
    let spinner = ProgressBar::new(len);

    spinner.set_style(
        ProgressStyle::default_spinner()
            .template("{spinner:.green} [{elapsed_precise}] [{bar:40.cyan/blue}] {pos}/{len} {msg}")
            .expect("Failed to set template")
            .progress_chars("#>-")
            .tick_strings(&["-", "\\", "|", "/"]),
    );
    spinner.enable_steady_tick(Duration::from_millis(100));

    spinner.println("DoubleZero Service Provisioning");

    spinner
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::net::Ipv4Addr;

    // ── Valid public addresses ──────────────────────────────────────────

    #[test]
    fn public_addresses_are_accepted() {
        let public = [
            Ipv4Addr::new(1, 0, 0, 1),
            Ipv4Addr::new(8, 8, 8, 8),
            Ipv4Addr::new(44, 0, 0, 1),
            Ipv4Addr::new(100, 63, 255, 255), // just below CGNAT range
            Ipv4Addr::new(100, 128, 0, 0),    // just above CGNAT range
            Ipv4Addr::new(172, 15, 255, 255), // just below 172.16/12
            Ipv4Addr::new(172, 32, 0, 0),     // just above 172.31.255.255
            Ipv4Addr::new(192, 1, 0, 0),      // just above 192.0.x ranges
            Ipv4Addr::new(192, 167, 255, 255), // just below 192.168/16
            Ipv4Addr::new(198, 17, 255, 255), // just below 198.18/15
            Ipv4Addr::new(198, 20, 0, 0),     // just above 198.19.255.255
            Ipv4Addr::new(223, 255, 255, 255), // last address before multicast
        ];
        for ip in &public {
            assert!(is_public_ipv4(*ip), "{ip} should be public");
        }
    }

    // ── 0.0.0.0/8 — "this network" ────────────────────────────────────

    #[test]
    fn this_network_range_rejected() {
        assert!(!is_public_ipv4(Ipv4Addr::new(0, 0, 0, 0)));
        assert!(!is_public_ipv4(Ipv4Addr::new(0, 0, 0, 1)));
        assert!(!is_public_ipv4(Ipv4Addr::new(0, 255, 255, 255)));
    }

    // ── 10.0.0.0/8 — RFC 1918 private ─────────────────────────────────

    #[test]
    fn rfc1918_10_range_rejected() {
        assert!(!is_public_ipv4(Ipv4Addr::new(10, 0, 0, 0)));
        assert!(!is_public_ipv4(Ipv4Addr::new(10, 0, 0, 1)));
        assert!(!is_public_ipv4(Ipv4Addr::new(10, 255, 255, 255)));
    }

    // ── 100.64.0.0/10 — CGNAT (RFC 6598) ──────────────────────────────

    #[test]
    fn cgnat_range_rejected() {
        assert!(!is_public_ipv4(Ipv4Addr::new(100, 64, 0, 0)));
        assert!(!is_public_ipv4(Ipv4Addr::new(100, 64, 0, 1)));
        assert!(!is_public_ipv4(Ipv4Addr::new(100, 100, 100, 100)));
        assert!(!is_public_ipv4(Ipv4Addr::new(100, 127, 255, 255)));
    }

    // ── 127.0.0.0/8 — loopback ────────────────────────────────────────

    #[test]
    fn loopback_range_rejected() {
        assert!(!is_public_ipv4(Ipv4Addr::new(127, 0, 0, 0)));
        assert!(!is_public_ipv4(Ipv4Addr::new(127, 0, 0, 1)));
        assert!(!is_public_ipv4(Ipv4Addr::new(127, 255, 255, 255)));
    }

    // ── 169.254.0.0/16 — link-local ───────────────────────────────────

    #[test]
    fn link_local_range_rejected() {
        assert!(!is_public_ipv4(Ipv4Addr::new(169, 254, 0, 0)));
        assert!(!is_public_ipv4(Ipv4Addr::new(169, 254, 0, 1)));
        assert!(!is_public_ipv4(Ipv4Addr::new(169, 254, 255, 255)));
    }

    // ── 172.16.0.0/12 — RFC 1918 private ──────────────────────────────

    #[test]
    fn rfc1918_172_range_rejected() {
        assert!(!is_public_ipv4(Ipv4Addr::new(172, 16, 0, 0)));
        assert!(!is_public_ipv4(Ipv4Addr::new(172, 16, 0, 1)));
        assert!(!is_public_ipv4(Ipv4Addr::new(172, 24, 0, 0)));
        assert!(!is_public_ipv4(Ipv4Addr::new(172, 31, 255, 255)));
    }

    // ── 192.0.0.0/24 — IETF protocol assignments (RFC 6890) ──────────

    #[test]
    fn ietf_protocol_assignments_rejected() {
        assert!(!is_public_ipv4(Ipv4Addr::new(192, 0, 0, 0)));
        assert!(!is_public_ipv4(Ipv4Addr::new(192, 0, 0, 1)));
        assert!(!is_public_ipv4(Ipv4Addr::new(192, 0, 0, 255)));
    }

    // ── 192.0.2.0/24 — TEST-NET-1 (RFC 5737) ─────────────────────────

    #[test]
    fn test_net_1_rejected() {
        assert!(!is_public_ipv4(Ipv4Addr::new(192, 0, 2, 0)));
        assert!(!is_public_ipv4(Ipv4Addr::new(192, 0, 2, 1)));
        assert!(!is_public_ipv4(Ipv4Addr::new(192, 0, 2, 255)));
    }

    // ── 192.168.0.0/16 — RFC 1918 private ─────────────────────────────

    #[test]
    fn rfc1918_192_168_range_rejected() {
        assert!(!is_public_ipv4(Ipv4Addr::new(192, 168, 0, 0)));
        assert!(!is_public_ipv4(Ipv4Addr::new(192, 168, 0, 1)));
        assert!(!is_public_ipv4(Ipv4Addr::new(192, 168, 255, 255)));
    }

    // ── 198.18.0.0/15 — benchmarking (RFC 2544) ──────────────────────

    #[test]
    fn benchmarking_range_rejected() {
        assert!(!is_public_ipv4(Ipv4Addr::new(198, 18, 0, 0)));
        assert!(!is_public_ipv4(Ipv4Addr::new(198, 18, 0, 1)));
        assert!(!is_public_ipv4(Ipv4Addr::new(198, 19, 255, 255)));
    }

    // ── 198.51.100.0/24 — TEST-NET-2 (RFC 5737) ──────────────────────

    #[test]
    fn test_net_2_rejected() {
        assert!(!is_public_ipv4(Ipv4Addr::new(198, 51, 100, 0)));
        assert!(!is_public_ipv4(Ipv4Addr::new(198, 51, 100, 1)));
        assert!(!is_public_ipv4(Ipv4Addr::new(198, 51, 100, 255)));
    }

    // ── 203.0.113.0/24 — TEST-NET-3 (RFC 5737) ──────────────────────

    #[test]
    fn test_net_3_rejected() {
        assert!(!is_public_ipv4(Ipv4Addr::new(203, 0, 113, 0)));
        assert!(!is_public_ipv4(Ipv4Addr::new(203, 0, 113, 1)));
        assert!(!is_public_ipv4(Ipv4Addr::new(203, 0, 113, 255)));
    }

    // ── 224.0.0.0/4 — multicast ───────────────────────────────────────

    #[test]
    fn multicast_range_rejected() {
        assert!(!is_public_ipv4(Ipv4Addr::new(224, 0, 0, 0)));
        assert!(!is_public_ipv4(Ipv4Addr::new(224, 0, 0, 1)));
        assert!(!is_public_ipv4(Ipv4Addr::new(239, 255, 255, 255)));
    }

    // ── 240.0.0.0/4 — reserved/future ─────────────────────────────────

    #[test]
    fn reserved_future_range_rejected() {
        assert!(!is_public_ipv4(Ipv4Addr::new(240, 0, 0, 0)));
        assert!(!is_public_ipv4(Ipv4Addr::new(240, 0, 0, 1)));
        assert!(!is_public_ipv4(Ipv4Addr::new(255, 255, 255, 255)));
    }
}
