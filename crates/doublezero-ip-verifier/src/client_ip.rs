//! Resolving the address a request actually came from.
//!
//! Everything else in this service is bookkeeping; this module is the security property. The proof
//! says "the DoubleZero verifier saw this payer originate traffic from this address", so an address
//! taken from anywhere the client can influence is worthless. The peer address of the TCP
//! connection is the only thing that cannot be spoofed off-path, and it stops being the client's
//! address only when a proxy we operate is in front of us — which is why forwarded headers are
//! honored for connections from configured proxy CIDRs and ignored for every other connection.

use ipnetwork::IpNetwork;
use std::net::{IpAddr, SocketAddr};

/// Why an address could not be resolved. Every variant means "do not sign anything": the service
/// fails closed rather than falling back to an address it is unsure about.
#[derive(thiserror::Error, Debug, PartialEq, Eq)]
pub enum ClientIpError {
    #[error("request arrived from a trusted proxy with no forwarded client address")]
    NoForwardedAddress,
    #[error("forwarded header entry {0:?} is not an IP address")]
    MalformedForwardedEntry(String),
    #[error("every hop in the forwarded chain is a trusted proxy, so no client address remains")]
    AllHopsTrusted,
}

/// Headers a proxy may use to carry the original client address, in the order they are consulted.
const X_FORWARDED_FOR: &str = "x-forwarded-for";
const FORWARDED: &str = "forwarded";

/// Where the resolved address came from, for logging and metrics. A deployment that expects every
/// request through a proxy but sees `Peer` is misconfigured, and that is worth being able to see.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AddressSource {
    /// The peer address of the connection, either because no proxies are configured or because
    /// this peer is not one of them.
    Peer,
    /// The last untrusted hop in `X-Forwarded-For`.
    XForwardedFor,
    /// The last untrusted hop in RFC 7239 `Forwarded`.
    Forwarded,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct ResolvedClientIp {
    pub addr: IpAddr,
    pub source: AddressSource,
}

/// Resolves the client address for a request.
///
/// With no trusted proxies configured, or for a peer outside every configured CIDR, the peer
/// address is used and forwarded headers are ignored entirely — a client that sends
/// `X-Forwarded-For` to a directly-reachable service gets a proof for its own address, not for
/// whatever it claimed.
///
/// For a peer inside a trusted CIDR, the forwarded chain is walked from the **right**, which is the
/// end our own proxies append to. The first hop that is not itself a trusted proxy is the client. A
/// chain the client prepended extra hops to is therefore ignored: those entries sit to the left of
/// the address the nearest trusted proxy observed. If the walk runs out of hops without finding an
/// untrusted one, or an entry does not parse, nothing is signed.
pub fn resolve_client_ip(
    peer: SocketAddr,
    headers: &axum::http::HeaderMap,
    trusted_proxies: &[IpNetwork],
) -> Result<ResolvedClientIp, ClientIpError> {
    let peer = peer.ip();

    if !is_trusted(peer, trusted_proxies) {
        return Ok(ResolvedClientIp {
            addr: peer,
            source: AddressSource::Peer,
        });
    }

    let (source, hops) = match collect_hops(headers, X_FORWARDED_FOR, parse_forwarded_for_entry)? {
        hops if !hops.is_empty() => (AddressSource::XForwardedFor, hops),
        _ => (
            AddressSource::Forwarded,
            collect_hops(headers, FORWARDED, parse_rfc7239_element)?,
        ),
    };

    if hops.is_empty() {
        return Err(ClientIpError::NoForwardedAddress);
    }

    hops.into_iter()
        .rev()
        .find(|hop| !is_trusted(*hop, trusted_proxies))
        .map(|addr| ResolvedClientIp { addr, source })
        .ok_or(ClientIpError::AllHopsTrusted)
}

fn is_trusted(addr: IpAddr, trusted_proxies: &[IpNetwork]) -> bool {
    trusted_proxies.iter().any(|network| network.contains(addr))
}

/// Flattens every instance of a header into one left-to-right list of hops. Repeated header lines
/// are equivalent to one comma-separated line, and proxies produce both shapes.
fn collect_hops(
    headers: &axum::http::HeaderMap,
    name: &str,
    parse: fn(&str) -> Result<IpAddr, ClientIpError>,
) -> Result<Vec<IpAddr>, ClientIpError> {
    let mut hops = Vec::new();

    for value in headers.get_all(name) {
        // A header that is not valid UTF-8, or an entry that is not an address, is a malformed
        // chain. Skipping the bad entry would silently shift which hop is treated as the client.
        let value = value
            .to_str()
            .map_err(|_| ClientIpError::MalformedForwardedEntry(format!("{value:?}")))?;

        for entry in value.split(',').map(str::trim).filter(|e| !e.is_empty()) {
            hops.push(parse(entry)?);
        }
    }

    Ok(hops)
}

/// Parses one `X-Forwarded-For` entry: a bare address, or an address with a port.
fn parse_forwarded_for_entry(entry: &str) -> Result<IpAddr, ClientIpError> {
    parse_host(entry).ok_or_else(|| ClientIpError::MalformedForwardedEntry(entry.to_string()))
}

/// Parses one RFC 7239 `Forwarded` element, taking its `for=` parameter and ignoring `proto`,
/// `host`, and `by`.
fn parse_rfc7239_element(element: &str) -> Result<IpAddr, ClientIpError> {
    element
        .split(';')
        .map(str::trim)
        .find_map(|param| {
            let (key, value) = param.split_once('=')?;
            key.trim().eq_ignore_ascii_case("for").then_some(value)
        })
        .and_then(|value| parse_host(value.trim().trim_matches('"')))
        .ok_or_else(|| ClientIpError::MalformedForwardedEntry(element.to_string()))
}

/// Parses an address that may carry a port, in any of the shapes proxies emit:
/// `1.2.3.4`, `1.2.3.4:443`, `2001:db8::1`, `[2001:db8::1]`, `[2001:db8::1]:443`.
fn parse_host(value: &str) -> Option<IpAddr> {
    // Unbracketed IPv6 parses directly and must be tried before any port stripping, or its own
    // colons would be read as a port separator.
    if let Ok(addr) = value.parse::<IpAddr>() {
        return Some(addr);
    }

    if let Some(rest) = value.strip_prefix('[') {
        let (addr, _port) = rest.split_once(']')?;
        return addr.parse().ok();
    }

    // Only an IPv4 address can be left, so a single colon is a port separator.
    let (addr, _port) = value.split_once(':')?;
    addr.parse::<std::net::Ipv4Addr>().ok().map(IpAddr::V4)
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::http::{HeaderMap, HeaderName, HeaderValue};
    use std::str::FromStr;

    const PROXY: &str = "198.18.0.1";
    const CLIENT: &str = "192.0.2.9";

    fn proxies(cidrs: &[&str]) -> Vec<IpNetwork> {
        cidrs
            .iter()
            .map(|c| IpNetwork::from_str(c).expect("test CIDR is valid"))
            .collect()
    }

    fn peer(addr: &str) -> SocketAddr {
        SocketAddr::new(addr.parse().expect("test peer is a valid address"), 54321)
    }

    fn headers(pairs: &[(&str, &str)]) -> HeaderMap {
        let mut headers = HeaderMap::new();
        for (name, value) in pairs {
            headers.append(
                HeaderName::from_str(name).expect("test header name is valid"),
                HeaderValue::from_str(value).expect("test header value is valid"),
            );
        }
        headers
    }

    fn resolve(
        peer_addr: &str,
        header_pairs: &[(&str, &str)],
        trusted: &[&str],
    ) -> Result<ResolvedClientIp, ClientIpError> {
        resolve_client_ip(peer(peer_addr), &headers(header_pairs), &proxies(trusted))
    }

    #[test]
    fn no_trusted_proxies_uses_the_peer_address() {
        let resolved = resolve(CLIENT, &[], &[]).unwrap();
        assert_eq!(resolved.addr, CLIENT.parse::<IpAddr>().unwrap());
        assert_eq!(resolved.source, AddressSource::Peer);
    }

    #[test]
    fn no_trusted_proxies_ignores_a_forwarded_header() {
        // The service is reachable directly, so a client claiming someone else's address gets a
        // proof for its own.
        let resolved = resolve(CLIENT, &[("x-forwarded-for", "203.0.113.1")], &[]).unwrap();
        assert_eq!(resolved.addr, CLIENT.parse::<IpAddr>().unwrap());
        assert_eq!(resolved.source, AddressSource::Peer);
    }

    #[test]
    fn an_untrusted_peer_sending_x_forwarded_for_is_ignored() {
        // Proxies are configured, but this connection did not come through one.
        let resolved = resolve(
            CLIENT,
            &[("x-forwarded-for", "203.0.113.1")],
            &["198.18.0.0/24"],
        )
        .unwrap();
        assert_eq!(resolved.addr, CLIENT.parse::<IpAddr>().unwrap());
        assert_eq!(resolved.source, AddressSource::Peer);
    }

    #[test]
    fn a_trusted_proxy_with_one_hop_yields_the_client() {
        let resolved = resolve(PROXY, &[("x-forwarded-for", CLIENT)], &["198.18.0.0/24"]).unwrap();
        assert_eq!(resolved.addr, CLIENT.parse::<IpAddr>().unwrap());
        assert_eq!(resolved.source, AddressSource::XForwardedFor);
    }

    #[test]
    fn a_spoofed_extra_hop_is_ignored() {
        // The client prepended an address it does not own; the proxy appended what it observed.
        let resolved = resolve(
            PROXY,
            &[("x-forwarded-for", &format!("203.0.113.1, {CLIENT}"))],
            &["198.18.0.0/24"],
        )
        .unwrap();
        assert_eq!(resolved.addr, CLIENT.parse::<IpAddr>().unwrap());
    }

    #[test]
    fn trailing_trusted_proxies_are_walked_past() {
        // Two of our own proxies in series: client, edge, internal.
        let resolved = resolve(
            "198.18.0.2",
            &[(
                "x-forwarded-for",
                &format!("{CLIENT}, 198.18.0.1, 198.18.0.3"),
            )],
            &["198.18.0.0/24"],
        )
        .unwrap();
        assert_eq!(resolved.addr, CLIENT.parse::<IpAddr>().unwrap());
    }

    #[test]
    fn repeated_headers_are_one_chain() {
        let resolved = resolve(
            PROXY,
            &[
                ("x-forwarded-for", "203.0.113.1"),
                ("x-forwarded-for", CLIENT),
            ],
            &["198.18.0.0/24"],
        )
        .unwrap();
        assert_eq!(resolved.addr, CLIENT.parse::<IpAddr>().unwrap());
    }

    #[test]
    fn entries_may_carry_ports() {
        for entry in [
            format!("{CLIENT}:443"),
            format!("[2001:db8::1]:443, {CLIENT}"),
        ] {
            let resolved =
                resolve(PROXY, &[("x-forwarded-for", &entry)], &["198.18.0.0/24"]).unwrap();
            assert_eq!(resolved.addr, CLIENT.parse::<IpAddr>().unwrap());
        }
    }

    #[test]
    fn an_ipv6_hop_resolves_bracketed_or_bare() {
        let expected: IpAddr = "2001:db8::1".parse().unwrap();

        for entry in ["2001:db8::1", "[2001:db8::1]", "[2001:db8::1]:443"] {
            let resolved =
                resolve(PROXY, &[("x-forwarded-for", entry)], &["198.18.0.0/24"]).unwrap();
            assert_eq!(resolved.addr, expected, "entry {entry}");
        }
    }

    #[test]
    fn a_trusted_proxy_with_no_forwarded_header_is_rejected() {
        // Signing here would attest the proxy's own address for whoever is behind it.
        assert_eq!(
            resolve(PROXY, &[], &["198.18.0.0/24"]),
            Err(ClientIpError::NoForwardedAddress)
        );
    }

    #[test]
    fn a_chain_of_only_trusted_hops_is_rejected() {
        assert_eq!(
            resolve(
                PROXY,
                &[("x-forwarded-for", "198.18.0.3, 198.18.0.4")],
                &["198.18.0.0/24"]
            ),
            Err(ClientIpError::AllHopsTrusted)
        );
    }

    #[test]
    fn a_malformed_entry_fails_closed() {
        assert_eq!(
            resolve(
                PROXY,
                &[("x-forwarded-for", &format!("unknown, {CLIENT}"))],
                &["198.18.0.0/24"]
            ),
            Err(ClientIpError::MalformedForwardedEntry(
                "unknown".to_string()
            ))
        );
    }

    #[test]
    fn rfc7239_forwarded_is_honored_when_x_forwarded_for_is_absent() {
        let resolved = resolve(
            PROXY,
            &[(
                "forwarded",
                &format!("for=203.0.113.1;proto=https, for=\"{CLIENT}:443\";proto=https"),
            )],
            &["198.18.0.0/24"],
        )
        .unwrap();
        assert_eq!(resolved.addr, CLIENT.parse::<IpAddr>().unwrap());
        assert_eq!(resolved.source, AddressSource::Forwarded);
    }

    #[test]
    fn x_forwarded_for_wins_over_forwarded() {
        // Both present: prefer the header our own proxies are configured to write, rather than
        // letting a client add the other one to change which chain is read.
        let resolved = resolve(
            PROXY,
            &[
                ("x-forwarded-for", CLIENT),
                ("forwarded", "for=203.0.113.1"),
            ],
            &["198.18.0.0/24"],
        )
        .unwrap();
        assert_eq!(resolved.addr, CLIENT.parse::<IpAddr>().unwrap());
        assert_eq!(resolved.source, AddressSource::XForwardedFor);
    }

    #[test]
    fn a_forwarded_element_without_a_for_parameter_fails_closed() {
        assert_eq!(
            resolve(
                PROXY,
                &[("forwarded", "proto=https;host=verify.doublezero.xyz")],
                &["198.18.0.0/24"]
            ),
            Err(ClientIpError::MalformedForwardedEntry(
                "proto=https;host=verify.doublezero.xyz".to_string()
            ))
        );
    }

    #[test]
    fn an_ipv6_peer_can_be_a_trusted_proxy() {
        let resolved = resolve(
            "2001:db8::2",
            &[("x-forwarded-for", CLIENT)],
            &["2001:db8::/64"],
        )
        .unwrap();
        assert_eq!(resolved.addr, CLIENT.parse::<IpAddr>().unwrap());
    }
}
