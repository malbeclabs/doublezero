//! Local validation for a caller-supplied `--client-ip`.
//!
//! `connect --client-ip` lets an operator name the address the tunnel must use instead of the
//! one the daemon discovered. Nothing onchain attests that address, so before it is used the
//! CLI checks the two things it can check locally: that the address is one DoubleZero will
//! accept at all, and that this host actually holds it.
//!
//! The second check is what makes the flag safe to honor. An address assigned to an up
//! interface is one the kernel will let this host source from, so a caller cannot pin an
//! address belonging to somebody else and have a tunnel built toward it. It is a possession
//! check, not a proof of ownership: it says the host holds the address now, which is what the
//! local configuration needs, and says nothing about who holds it on the public internet.
//! RFC-27 proofs remain the answer to that question.

use std::net::{IpAddr, Ipv4Addr};

use doublezero_serviceability::helper::is_global;
use nix::{ifaddrs::getifaddrs, net::if_::InterfaceFlags};

/// Why a `--client-ip` value cannot be used.
#[derive(Debug, PartialEq, Eq)]
pub enum ClientIpRejection {
    /// Not a globally routable address — loopback, RFC 1918, CGNAT, documentation, multicast
    /// and the other reserved ranges the serviceability program refuses.
    NotGlobal,
    /// Globally routable, but not assigned to any interface that is up on this host.
    NotLocallyAssigned,
}

impl std::fmt::Display for ClientIpRejection {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::NotGlobal => write!(
                f,
                "it is not a globally routable address (loopback, private, CGNAT, documentation \
                 and other reserved ranges cannot be used as a DoubleZero client IP)"
            ),
            Self::NotLocallyAssigned => write!(
                f,
                "it is not assigned to any interface that is up on this host, so the tunnel \
                 could not be sourced from it"
            ),
        }
    }
}

/// Check a caller-supplied client IP against this host.
///
/// `is_global` is the same predicate the serviceability program applies when it accepts a
/// `client_ip`, so a value that passes here is one the ledger will also accept — the CLI
/// rejects it early and by name instead of letting the operator discover it as a program
/// error several steps later.
pub fn validate_client_ip(ip: Ipv4Addr) -> Result<(), ClientIpRejection> {
    // Tests describe a host's interfaces instead of depending on the machine running them;
    // see [`test_support`]. Absent an override this is a no-op.
    #[cfg(test)]
    if let Some(f) = test_support::override_fn() {
        return f(ip);
    }

    if !is_global(ip) {
        return Err(ClientIpRejection::NotGlobal);
    }
    match local_ipv4_addrs() {
        Ok(addrs) if addrs.contains(&ip) => Ok(()),
        Ok(_) => Err(ClientIpRejection::NotLocallyAssigned),
        // Enumeration is best-effort: if the host will not tell us its interfaces we cannot
        // prove the address is absent, and failing closed here would block an operator on a
        // platform quirk. The daemon repeats this check before it touches local config, and
        // it is the one that must hold.
        Err(err) => {
            tracing::debug!(
                "could not enumerate local interfaces ({err}); leaving the client IP check to \
                 the daemon"
            );
            Ok(())
        }
    }
}

/// Every IPv4 address assigned to an interface that is both up and running.
///
/// `IFF_UP` alone is the administrative state; an interface can be up with no carrier. Both
/// flags together are what the kernel uses to mean "usable right now", which is the question
/// that matters for a tunnel source.
fn local_ipv4_addrs() -> nix::Result<Vec<Ipv4Addr>> {
    let mut out = Vec::new();
    for ifaddr in getifaddrs()? {
        if !ifaddr.flags.contains(InterfaceFlags::IFF_UP)
            || !ifaddr.flags.contains(InterfaceFlags::IFF_RUNNING)
        {
            continue;
        }
        let Some(storage) = ifaddr.address else {
            continue;
        };
        if let Some(sin) = storage.as_sockaddr_in() {
            if let IpAddr::V4(v4) = IpAddr::from(sin.ip()) {
                out.push(v4);
            }
        }
    }
    Ok(out)
}

/// Lets a test stand in for this host's interfaces while exercising code paths that reach
/// [`validate_client_ip`] indirectly, such as `connect --client-ip`.
#[cfg(test)]
pub mod test_support {
    use super::{ClientIpRejection, Ipv4Addr};
    use std::cell::Cell;

    type ValidateFn = fn(Ipv4Addr) -> Result<(), ClientIpRejection>;

    thread_local! {
        static OVERRIDE: Cell<Option<ValidateFn>> = const { Cell::new(None) };
    }

    pub(super) fn override_fn() -> Option<ValidateFn> {
        OVERRIDE.with(|o| o.get())
    }

    /// Restores the previous override when dropped, so one test's host cannot leak into the
    /// next one sharing the thread.
    pub struct Guard(Option<ValidateFn>);

    impl Drop for Guard {
        fn drop(&mut self) {
            OVERRIDE.with(|o| o.set(self.0));
        }
    }

    /// Pretend this host holds every address.
    pub fn with_host_holding_any_address() -> Guard {
        Guard(OVERRIDE.with(|o| o.replace(Some(|_| Ok(())))))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn rejects_non_global_addresses() {
        for ip in [
            Ipv4Addr::new(127, 0, 0, 1),
            Ipv4Addr::new(10, 0, 0, 1),
            Ipv4Addr::new(192, 168, 1, 1),
            Ipv4Addr::new(100, 64, 0, 1),
            Ipv4Addr::new(169, 254, 1, 1),
            Ipv4Addr::new(224, 0, 0, 1),
        ] {
            assert_eq!(
                validate_client_ip(ip),
                Err(ClientIpRejection::NotGlobal),
                "expected {ip} to be rejected as non-global"
            );
        }
    }

    // A globally routable address this host does not hold must be refused. 198.51.100.0/24 is
    // TEST-NET-2, which is itself non-global, so use a routable address no host legitimately
    // owns locally.
    #[test]
    fn rejects_global_address_not_on_this_host() {
        assert_eq!(
            validate_client_ip(Ipv4Addr::new(8, 8, 8, 8)),
            Err(ClientIpRejection::NotLocallyAssigned)
        );
    }

    // Whatever the host's own interfaces are, enumeration must agree with itself: every address
    // it reports has to pass the assignment half of the check.
    //
    // Deliberately not filtered to globally routable addresses. CI and developer machines are
    // NAT'd, so every address they enumerate is private and the filtered loop had no iterations
    // at all — a test structurally incapable of failing where it runs. The assignment half is
    // what this guards, so a non-global address must be rejected for *that* reason and no other.
    #[test]
    fn accepts_addresses_this_host_actually_holds() {
        let addrs = local_ipv4_addrs().expect("enumerating local interfaces");
        assert!(
            !addrs.is_empty(),
            "a host with no enumerable IPv4 address cannot exercise this"
        );
        for ip in addrs {
            let expected = if is_global(ip) {
                Ok(())
            } else {
                Err(ClientIpRejection::NotGlobal)
            };
            assert_eq!(
                validate_client_ip(ip),
                expected,
                "the locally assigned {ip} must never be rejected as unassigned"
            );
        }
    }

    #[test]
    fn loopback_is_enumerated_but_still_rejected() {
        // Guards the ordering: the global check runs first, so a locally assigned loopback
        // address is refused for the right reason rather than admitted for being local.
        assert_eq!(
            validate_client_ip(Ipv4Addr::LOCALHOST),
            Err(ClientIpRejection::NotGlobal)
        );
    }
}
