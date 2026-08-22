//! Resolving the address a request actually came from.
//!
//! Everything else in this service is bookkeeping; this module is the security property. The proof
//! says "the DoubleZero verifier saw this payer originate traffic from this address", so an address
//! taken from anywhere the client can influence is worthless. The peer address of the TCP
//! connection is the only thing that cannot be spoofed off-path, and it stops being the client's
//! address only when a proxy we operate is in front of us — which is why forwarded headers are
//! honored for connections from configured proxy CIDRs and ignored for every other connection.

use ipnetwork::IpNetwork;
use std::net::{IpAddr, Ipv6Addr, SocketAddr};

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

/// Which header the trusted proxy in front of this service writes the client address into.
///
/// Exactly one is read, and it is named in configuration rather than guessed. Consulting both would
/// be one-directional protection at best: a proxy configured for RFC 7239 `Forwarded` that does not
/// also set or strip `X-Forwarded-For` passes the client's own `X-Forwarded-For` through untouched
/// — nginx and HAProxy forward unknown request headers by default — so whichever header "wins" by
/// hardcoded preference can be the one the client wrote.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default, clap::ValueEnum)]
pub enum ForwardedHeader {
    /// The de-facto `X-Forwarded-For`.
    #[default]
    XForwardedFor,
    /// RFC 7239 `Forwarded`, reading the `for=` parameter of each element.
    Forwarded,
}

impl ForwardedHeader {
    fn header_name(&self) -> &'static str {
        match self {
            Self::XForwardedFor => "x-forwarded-for",
            Self::Forwarded => "forwarded",
        }
    }

    /// Parses one entry of this header into an address.
    fn parse_entry(&self, entry: &str) -> Result<IpAddr, ClientIpError> {
        let parsed = match self {
            // A bare address, optionally with a port.
            Self::XForwardedFor => parse_host(entry),
            // An RFC 7239 element: take its `for=` parameter, ignore `proto`, `host`, and `by`.
            Self::Forwarded => entry
                .split(';')
                .map(str::trim)
                .find_map(|param| {
                    let (key, value) = param.split_once('=')?;
                    key.trim().eq_ignore_ascii_case("for").then_some(value)
                })
                .and_then(|value| parse_host(value.trim().trim_matches('"'))),
        };

        parsed
            .map(normalize)
            .ok_or_else(|| ClientIpError::MalformedForwardedEntry(entry.to_string()))
    }
}

/// Where the resolved address came from, for logging and metrics. A deployment that expects every
/// request through a proxy but sees `Peer` is misconfigured, and that is worth being able to see.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AddressSource {
    /// The peer address of the connection, either because no proxies are configured or because
    /// this peer is not one of them.
    Peer,
    /// The last untrusted hop of the configured forwarded header.
    Forwarded(ForwardedHeader),
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
/// For a peer inside a trusted CIDR, the configured header's chain is walked from the **right**,
/// which is the end our own proxies append to. The first hop that is not itself a trusted proxy is
/// the client. A chain the client prepended extra hops to is therefore ignored: those entries sit
/// to the left of the address the nearest trusted proxy observed, and are never even parsed.
///
/// Nothing is signed if the header is absent, if every hop is a trusted proxy, or if an entry at or
/// to the right of the first untrusted hop does not parse.
pub fn resolve_client_ip(
    peer: SocketAddr,
    headers: &axum::http::HeaderMap,
    trusted_proxies: &[IpNetwork],
    forwarded_header: ForwardedHeader,
) -> Result<ResolvedClientIp, ClientIpError> {
    let peer = normalize(peer.ip());

    if !is_trusted(peer, trusted_proxies) {
        return Ok(ResolvedClientIp {
            addr: peer,
            source: AddressSource::Peer,
        });
    }

    let entries = raw_entries(headers, forwarded_header.header_name())?;
    if entries.is_empty() {
        return Err(ClientIpError::NoForwardedAddress);
    }

    // Parsing happens lazily, right to left, so a junk entry only matters if it could be the client.
    // `X-Forwarded-For: unknown` is a real convention (as is RFC 7239 `for=unknown`), and nginx's
    // `$proxy_add_x_forwarded_for` concatenates whatever the client sent — so an unparsable entry to
    // the left of the hop our proxy observed must not turn a legitimate request into a failure.
    for entry in entries.iter().rev() {
        let addr = forwarded_header.parse_entry(entry)?;
        if !is_trusted(addr, trusted_proxies) {
            return Ok(ResolvedClientIp {
                addr,
                source: AddressSource::Forwarded(forwarded_header),
            });
        }
    }

    Err(ClientIpError::AllHopsTrusted)
}

fn is_trusted(addr: IpAddr, trusted_proxies: &[IpNetwork]) -> bool {
    trusted_proxies.iter().any(|network| network.contains(addr))
}

/// Collapses an IPv4-mapped IPv6 address to the IPv4 address it carries.
///
/// A dual-stack listener (`--listen-addr [::]:8080`, the usual container default) reports every
/// IPv4 client as `::ffff:a.b.c.d`. Left alone, those requests fail the IPv4-only check while IPv4
/// `--trusted-proxy` CIDRs silently stop matching the proxies they name. Some proxies emit the
/// mapped form in forwarded headers too, so hops get the same treatment.
fn normalize(addr: IpAddr) -> IpAddr {
    match addr {
        IpAddr::V6(v6) => Ipv6Addr::to_ipv4_mapped(&v6)
            .map(IpAddr::V4)
            .unwrap_or(IpAddr::V6(v6)),
        v4 => v4,
    }
}

/// Flattens every instance of a header into one left-to-right list of entries, still unparsed.
/// Repeated header lines are equivalent to one comma-separated line, and proxies produce both
/// shapes.
fn raw_entries<'h>(
    headers: &'h axum::http::HeaderMap,
    name: &str,
) -> Result<Vec<&'h str>, ClientIpError> {
    let mut entries = Vec::new();

    for value in headers.get_all(name) {
        // A header that is not valid UTF-8 cannot be split into entries at all, so there is no
        // rightmost hop to trust.
        let value = value
            .to_str()
            .map_err(|_| ClientIpError::MalformedForwardedEntry(format!("{value:?}")))?;

        entries.extend(value.split(',').map(str::trim).filter(|e| !e.is_empty()));
    }

    Ok(entries)
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
    const PROXY_CIDR: &str = "198.18.0.0/24";

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

    /// Resolves with `X-Forwarded-For` configured, which is the default.
    fn resolve(
        peer_addr: &str,
        header_pairs: &[(&str, &str)],
        trusted: &[&str],
    ) -> Result<ResolvedClientIp, ClientIpError> {
        resolve_with(
            peer_addr,
            header_pairs,
            trusted,
            ForwardedHeader::XForwardedFor,
        )
    }

    fn resolve_with(
        peer_addr: &str,
        header_pairs: &[(&str, &str)],
        trusted: &[&str],
        forwarded_header: ForwardedHeader,
    ) -> Result<ResolvedClientIp, ClientIpError> {
        resolve_client_ip(
            peer(peer_addr),
            &headers(header_pairs),
            &proxies(trusted),
            forwarded_header,
        )
    }

    fn client() -> IpAddr {
        CLIENT.parse().expect("test client address is valid")
    }

    #[test]
    fn no_trusted_proxies_uses_the_peer_address() {
        let resolved = resolve(CLIENT, &[], &[]).unwrap();
        assert_eq!(resolved.addr, client());
        assert_eq!(resolved.source, AddressSource::Peer);
    }

    #[test]
    fn no_trusted_proxies_ignores_a_forwarded_header() {
        // The service is reachable directly, so a client claiming someone else's address gets a
        // proof for its own.
        let resolved = resolve(CLIENT, &[("x-forwarded-for", "203.0.113.1")], &[]).unwrap();
        assert_eq!(resolved.addr, client());
        assert_eq!(resolved.source, AddressSource::Peer);
    }

    #[test]
    fn an_untrusted_peer_sending_x_forwarded_for_is_ignored() {
        // Proxies are configured, but this connection did not come through one.
        let resolved =
            resolve(CLIENT, &[("x-forwarded-for", "203.0.113.1")], &[PROXY_CIDR]).unwrap();
        assert_eq!(resolved.addr, client());
        assert_eq!(resolved.source, AddressSource::Peer);
    }

    #[test]
    fn a_trusted_proxy_with_one_hop_yields_the_client() {
        let resolved = resolve(PROXY, &[("x-forwarded-for", CLIENT)], &[PROXY_CIDR]).unwrap();
        assert_eq!(resolved.addr, client());
        assert_eq!(
            resolved.source,
            AddressSource::Forwarded(ForwardedHeader::XForwardedFor)
        );
    }

    #[test]
    fn a_spoofed_extra_hop_is_ignored() {
        // The client prepended an address it does not own; the proxy appended what it observed.
        let resolved = resolve(
            PROXY,
            &[("x-forwarded-for", &format!("203.0.113.1, {CLIENT}"))],
            &[PROXY_CIDR],
        )
        .unwrap();
        assert_eq!(resolved.addr, client());
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
            &[PROXY_CIDR],
        )
        .unwrap();
        assert_eq!(resolved.addr, client());
    }

    #[test]
    fn repeated_headers_are_one_chain() {
        let resolved = resolve(
            PROXY,
            &[
                ("x-forwarded-for", "203.0.113.1"),
                ("x-forwarded-for", CLIENT),
            ],
            &[PROXY_CIDR],
        )
        .unwrap();
        assert_eq!(resolved.addr, client());
    }

    #[test]
    fn entries_may_carry_ports() {
        for entry in [
            format!("{CLIENT}:443"),
            format!("[2001:db8::1]:443, {CLIENT}"),
        ] {
            let resolved = resolve(PROXY, &[("x-forwarded-for", &entry)], &[PROXY_CIDR]).unwrap();
            assert_eq!(resolved.addr, client());
        }
    }

    #[test]
    fn an_ipv6_hop_resolves_bracketed_or_bare() {
        let expected: IpAddr = "2001:db8::1".parse().unwrap();

        for entry in ["2001:db8::1", "[2001:db8::1]", "[2001:db8::1]:443"] {
            let resolved = resolve(PROXY, &[("x-forwarded-for", entry)], &[PROXY_CIDR]).unwrap();
            assert_eq!(resolved.addr, expected, "entry {entry}");
        }
    }

    #[test]
    fn a_trusted_proxy_with_no_forwarded_header_is_rejected() {
        // Signing here would attest the proxy's own address for whoever is behind it.
        assert_eq!(
            resolve(PROXY, &[], &[PROXY_CIDR]),
            Err(ClientIpError::NoForwardedAddress)
        );
    }

    #[test]
    fn a_chain_of_only_trusted_hops_is_rejected() {
        assert_eq!(
            resolve(
                PROXY,
                &[("x-forwarded-for", "198.18.0.3, 198.18.0.4")],
                &[PROXY_CIDR]
            ),
            Err(ClientIpError::AllHopsTrusted)
        );
    }

    #[test]
    fn a_malformed_entry_left_of_the_client_hop_is_never_parsed() {
        // `$proxy_add_x_forwarded_for` concatenates whatever the client sent, and `unknown` is a
        // widely emitted value. The rightmost hop is what our proxy observed, so the request stands.
        let resolved = resolve(
            PROXY,
            &[("x-forwarded-for", &format!("unknown, {CLIENT}"))],
            &[PROXY_CIDR],
        )
        .unwrap();
        assert_eq!(resolved.addr, client());
    }

    #[test]
    fn a_malformed_entry_at_the_client_hop_fails_closed() {
        assert_eq!(
            resolve(PROXY, &[("x-forwarded-for", "unknown")], &[PROXY_CIDR]),
            Err(ClientIpError::MalformedForwardedEntry(
                "unknown".to_string()
            ))
        );
    }

    #[test]
    fn a_malformed_entry_right_of_a_trusted_hop_fails_closed() {
        // Walking right to left, this junk sits where the client address should be.
        assert_eq!(
            resolve(
                PROXY,
                &[("x-forwarded-for", &format!("{CLIENT}, unknown, 198.18.0.3"))],
                &[PROXY_CIDR]
            ),
            Err(ClientIpError::MalformedForwardedEntry(
                "unknown".to_string()
            ))
        );
    }

    #[test]
    fn only_the_configured_header_is_read() {
        // A proxy that writes `Forwarded` while passing the client's `X-Forwarded-For` through is
        // the case that makes consulting both headers unsafe.
        let resolved = resolve_with(
            PROXY,
            &[
                ("x-forwarded-for", "203.0.113.1"),
                ("forwarded", &format!("for={CLIENT}")),
            ],
            &[PROXY_CIDR],
            ForwardedHeader::Forwarded,
        )
        .unwrap();
        assert_eq!(resolved.addr, client());
        assert_eq!(
            resolved.source,
            AddressSource::Forwarded(ForwardedHeader::Forwarded)
        );

        // And the mirror image: with `X-Forwarded-For` configured, `Forwarded` is not consulted at
        // all, not even as a fallback when the configured header is missing.
        assert_eq!(
            resolve(
                PROXY,
                &[("forwarded", &format!("for={CLIENT}"))],
                &[PROXY_CIDR]
            ),
            Err(ClientIpError::NoForwardedAddress)
        );
    }

    #[test]
    fn rfc7239_elements_are_parsed_with_their_parameters() {
        let resolved = resolve_with(
            PROXY,
            &[(
                "forwarded",
                &format!("for=203.0.113.1;proto=https, for=\"{CLIENT}:443\";proto=https"),
            )],
            &[PROXY_CIDR],
            ForwardedHeader::Forwarded,
        )
        .unwrap();
        assert_eq!(resolved.addr, client());
    }

    #[test]
    fn a_forwarded_element_without_a_for_parameter_fails_closed() {
        assert_eq!(
            resolve_with(
                PROXY,
                &[("forwarded", "proto=https;host=verify.doublezero.xyz")],
                &[PROXY_CIDR],
                ForwardedHeader::Forwarded,
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
        assert_eq!(resolved.addr, client());
    }

    #[test]
    fn an_ipv4_mapped_peer_is_collapsed_to_its_ipv4_address() {
        // What a dual-stack `[::]` listener reports for every IPv4 client.
        let resolved = resolve(&format!("::ffff:{CLIENT}"), &[], &[]).unwrap();
        assert_eq!(resolved.addr, client());
        assert_eq!(resolved.source, AddressSource::Peer);
    }

    #[test]
    fn an_ipv4_mapped_peer_still_matches_an_ipv4_proxy_cidr() {
        let resolved = resolve(
            &format!("::ffff:{PROXY}"),
            &[("x-forwarded-for", CLIENT)],
            &[PROXY_CIDR],
        )
        .unwrap();
        assert_eq!(resolved.addr, client());
    }

    #[test]
    fn an_ipv4_mapped_hop_is_collapsed_and_trust_checked_as_ipv4() {
        let resolved = resolve(
            PROXY,
            &[("x-forwarded-for", &format!("::ffff:{CLIENT}"))],
            &[PROXY_CIDR],
        )
        .unwrap();
        assert_eq!(resolved.addr, client());

        // A mapped form of a trusted proxy is still a trusted proxy, so it is walked past.
        let resolved = resolve(
            PROXY,
            &[("x-forwarded-for", &format!("{CLIENT}, ::ffff:198.18.0.3"))],
            &[PROXY_CIDR],
        )
        .unwrap();
        assert_eq!(resolved.addr, client());
    }
}
