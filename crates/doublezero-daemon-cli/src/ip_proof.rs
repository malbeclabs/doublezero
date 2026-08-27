//! RFC-27 IP ownership proof retrieval for `connect`.
//!
//! `client_ip` is a plain argument to user creation: nothing onchain attests that the caller can
//! originate traffic from it. RFC-27 closes that with a proof signed by a DoubleZero-operated
//! verifier, and this is where `connect` obtains one.
//!
//! **The service, not the host, decides the address.** The verifier signs the source address it
//! observes the request originate from and refuses to accept a caller-supplied one — the request
//! body has no `client_ip` field at all. So the proof it returns is the authoritative value, and
//! the daemon's own discovery (`resolve_client_ip`, ultimately `ifconfig.me` inside
//! `doublezerod`) is a convenience for display and pre-flight checks. Where the two disagree,
//! `connect` stops rather than guessing.
//!
//! Retrieval is deliberately best-effort. A host with no reachable verifier, or behind CGNAT,
//! still connects: creation proceeds without a proof and the program decides, which succeeds
//! while `require-ip-ownership-proof` is clear and fails cleanly once it is set. The one hard
//! failure is a proof for an address that is not the one being provisioned, because attaching it
//! would guarantee an onchain rejection and ignoring it would bind an address nobody proved.

use std::{
    net::{Ipv4Addr, SocketAddr},
    str::FromStr,
    thread,
    time::Duration,
};

use doublezero_ip_proof::IpOwnershipProof;
use doublezero_sdk::UserType;
use mockall::automock;
use serde::Deserialize;
use solana_sdk::{pubkey::Pubkey, signature::Signature};
use tracing::debug;

/// How long to wait on one attempt before giving up and letting the program decide. Short on
/// purpose: `connect` is interactive, and the fallback is a working connection while the feature
/// flag is clear. [`MAX_ATTEMPTS`] of these plus the delay between them is the whole budget.
const REQUEST_TIMEOUT: Duration = Duration::from_millis(2500);

/// The whole connect budget, so a second DNS answer is actually reached. hyper tries the resolved
/// addresses in sequence, but its happy-eyeballs timer only crosses address families, not two A
/// records — so without a connect timeout a blackholed address (SYN dropped, which is how a host
/// that is down or firewalled usually fails) burns the entire request budget and its siblings are
/// never tried. Redundant A records are then a coin flip rather than redundancy.
///
/// Note this is a *total*, not a per-address value: hyper-util divides it by the number of
/// resolved addresses (`connect/http.rs`, `ConnectingTcpRemote::new`), so one A record gets the
/// full second and eight get 125ms each — less than a single cross-ocean RTT, which would abandon
/// healthy hosts. The value is sized for the one-address case that holds today. Whether a fleet
/// behind this name should raise it, or sit behind a single load-balancer or anycast VIP so it
/// stays one address, belongs with the deployment work in #4199.
const CONNECT_TIMEOUT: Duration = Duration::from_secs(1);

/// How long the binding probe will wait for the verifier's name to resolve. It needs a budget of
/// its own because it is the one piece of network work that happens *outside* the request: the
/// binding has to be decided before the client is built, so the probe's `getaddrinfo` runs ahead
/// of [`REQUEST_TIMEOUT`] rather than inside it. Unbounded, a host with a stuck resolver would sit
/// here for the `resolv.conf` budget — commonly 5s per attempt, more than the whole invocation is
/// supposed to take — before any of the other timeouts had a chance to apply.
const RESOLVE_TIMEOUT: Duration = Duration::from_secs(1);

/// Attempts before giving up. `POST /v1/proof` is idempotent — it signs the address it observes
/// and holds no per-request state — so a retry cannot double anything. Only a transport error or
/// a 5xx is retried: a decline (`rate_limited` above all) and a malformed body are answers that
/// will not change.
///
/// What the second attempt buys is a transient blip, not failover. A transport error leaves no
/// pooled connection, so that retry does redial and can land on a different resolved address; a
/// 5xx came from a host that is up, whose keep-alive connection the pool will hand straight back,
/// and which hyper would try first even on a fresh dial. Reaching a *different* backend is
/// therefore a load balancer's or an anycast VIP's job, in front of the fleet — see #4199 — not
/// something a deeper retry here could add.
const MAX_ATTEMPTS: u32 = 2;

/// Between attempts. Short, for the same reason the timeout is.
const RETRY_DELAY: Duration = Duration::from_millis(250);

/// Why no proof is available. Every variant is a reason to *continue* without one — the program
/// is the enforcement point, and refusing to connect here would break every host in an
/// environment whose flag is still clear. They are separated so the operator learns which one
/// happened, because the remedies are completely different.
#[derive(Debug, thiserror::Error)]
pub enum IpProofError {
    /// No verifier is deployed for this environment and none was configured (#4199).
    #[error("no IP ownership verification service is configured for this environment")]
    NotConfigured,

    /// The service could not be reached: DNS, connect, TLS, or timeout.
    #[error("could not reach the IP ownership verification service at {url}: {detail}")]
    Unreachable { url: String, detail: String },

    /// The service answered and declined. `reason` is its stable machine-readable code — notably
    /// `not_globally_routable` for a CGNAT or RFC-1918 source, and `rate_limited`.
    #[error(
        "the IP ownership verification service declined to issue a proof ({reason}): {message}"
    )]
    Declined { reason: String, message: String },

    /// The service answered with something this client cannot turn into a proof. A version skew
    /// or a captive portal in the path both land here.
    #[error("could not read the proof the verification service returned: {0}")]
    Malformed(String),
}

/// Obtains an RFC-27 proof for the calling host.
#[automock]
pub trait IpProofClient: Send + Sync {
    /// Request a proof binding `payer`, the address the service observes, and `user_type`.
    ///
    /// `source_addr` is the address the tunnel will use; the implementation binds the outbound
    /// request to it where it can, so a multi-homed host proves the address it will actually
    /// originate tunnel traffic from rather than whichever one the routing table prefers.
    fn request_proof(
        &self,
        payer: Pubkey,
        user_type: UserType,
        source_addr: Ipv4Addr,
    ) -> Result<IpOwnershipProof, IpProofError>;
}

/// Mirrors the service's `ProofResponse`. Deliberately a separate type from `IpOwnershipProof`:
/// the wire form carries base58 strings, and a field the program does not understand must fail
/// here rather than be silently coerced.
#[derive(Debug, Deserialize)]
struct ProofResponse {
    version: u8,
    payer: String,
    client_ip: Ipv4Addr,
    epoch: u64,
    user_type: u8,
    signature: String,
}

/// Mirrors the service's `ErrorResponse`.
#[derive(Debug, Deserialize)]
struct ErrorResponse {
    error: String,
    message: String,
}

/// The real client. `None` for `base_url` models an environment with no verifier, so the caller
/// does not have to special-case its own configuration.
pub struct HttpIpProofClient {
    base_url: Option<String>,
}

impl HttpIpProofClient {
    pub fn new(base_url: Option<String>) -> Self {
        Self { base_url }
    }

    /// Blocking on purpose: it sits alongside `LedgerClient`'s blocking RPC calls in the same
    /// `connect` code path, and one short request per invocation does not justify a second
    /// async HTTP stack in this crate.
    ///
    /// `binding` decides whether the request is pinned to the tunnel's source address; see
    /// [`probe_source_binding`] for why that is decided here rather than left to reqwest.
    fn build(
        binding: SourceBinding,
        source_addr: Ipv4Addr,
        url: &str,
    ) -> Result<reqwest::blocking::Client, IpProofError> {
        // Never through a proxy, whatever HTTP_PROXY and friends say. reqwest honours the system
        // proxy by default, and a proxied request would terminate at the proxy: the verifier's
        // peer address would be the proxy's, the source binding below would apply only to the
        // hop to the proxy, and the proof would name an address this host cannot originate from.
        // `connect` would then abort on the mismatch, on a host where it used to work. A
        // proxy-only host instead reports "unreachable" and continues without a proof, which is
        // the non-fatal path.
        let mut builder = reqwest::blocking::Client::builder()
            .no_proxy()
            .timeout(REQUEST_TIMEOUT)
            .connect_timeout(CONNECT_TIMEOUT);

        if binding == SourceBinding::Bind {
            builder = builder.local_address(Some(std::net::IpAddr::V4(source_addr)));
        }

        builder.build().map_err(|e| IpProofError::Unreachable {
            url: url.to_string(),
            detail: format!("could not build an HTTP client: {e}"),
        })
    }
}

/// Whether the verification request should be pinned to the tunnel's source address.
#[derive(Copy, Clone, Debug, Eq, PartialEq)]
enum SourceBinding {
    /// The address is assigned to this host and is a legal source toward the verifier.
    Bind,
    /// The address is on no local interface. The ordinary NATed host: `client_ip` is the NAT's
    /// public address, which is what the service will observe over the default egress anyway.
    NotLocal,
    /// The host owns the address but cannot reach the verifier from it. The request and the
    /// tunnel would leave by different paths, so the proof would name the wrong address.
    Unroutable,
    /// The verifier's name did not resolve. The request itself will report that, with a better
    /// message than a binding probe could.
    Unresolved,
}

/// Decides whether `source_addr` can actually originate the request to `url`.
///
/// This has to be probed rather than attempted, because reqwest's `local_address` is only stored
/// in the connector config — the `bind()` happens per connection inside hyper, so `build()`
/// succeeds for an address that is assigned to nothing and the failure surfaces later as an
/// unremarkable connect error.
///
/// A UDP socket answers both halves of the question without sending a packet: `bind` asks the
/// kernel whether the address is assigned to this host, and `connect` is a pure route lookup
/// asking whether it is a legal source for that destination. The second half matters as much as
/// the first — a non-loopback source toward `127.0.0.1` fails with `EINVAL`, which is exactly the
/// localnet default.
fn probe_source_binding(source_addr: Ipv4Addr, url: &str) -> SourceBinding {
    let Ok(socket) = std::net::UdpSocket::bind((source_addr, 0)) else {
        return SourceBinding::NotLocal;
    };
    // Parsing is done here rather than on the worker: it touches no network, and a URL that is
    // not a URL should fail instantly instead of costing a thread.
    let Ok(parsed) = reqwest::Url::parse(url) else {
        return SourceBinding::Unresolved;
    };
    let Some(addrs) = resolve_within(RESOLVE_TIMEOUT, move || parsed.socket_addrs(|| None).ok())
    else {
        return SourceBinding::Unresolved;
    };
    if addrs.iter().any(|addr| socket.connect(addr).is_ok()) {
        SourceBinding::Bind
    } else {
        SourceBinding::Unroutable
    }
}

/// Runs `resolve` with a deadline, giving up rather than blocking past it.
///
/// `getaddrinfo` is blocking and uncancellable, and `std` has no async resolver — one short
/// lookup per invocation does not justify pulling one in — so the only way to bound it is to run
/// it somewhere abandonable. Giving up returns `None`, which the caller reads as "do not bind":
/// the request then proceeds unbound and resolves the name itself, where [`REQUEST_TIMEOUT`]
/// covers it and the failure carries a better message than the probe could produce.
///
/// The abandoned thread is not a leak. It ends when the lookup returns, and its `send` into a
/// dropped receiver simply fails.
///
/// Split out from [`probe_source_binding`] so the deadline can be tested with a slow closure
/// rather than a resolver that has to be made to hang.
fn resolve_within<F>(deadline: Duration, resolve: F) -> Option<Vec<SocketAddr>>
where
    F: FnOnce() -> Option<Vec<SocketAddr>> + Send + 'static,
{
    let (tx, rx) = std::sync::mpsc::channel();
    thread::spawn(move || {
        let _ = tx.send(resolve());
    });
    rx.recv_timeout(deadline).ok().flatten()
}

impl IpProofClient for HttpIpProofClient {
    fn request_proof(
        &self,
        payer: Pubkey,
        user_type: UserType,
        source_addr: Ipv4Addr,
    ) -> Result<IpOwnershipProof, IpProofError> {
        let base_url = self
            .base_url
            .as_deref()
            .ok_or(IpProofError::NotConfigured)?;
        let url = format!("{}/v1/proof", base_url.trim_end_matches('/'));

        // Bind the request to the address the tunnel will use, so a multi-homed host whose
        // operator set --client-ip on the daemon proves that address rather than whichever one
        // the default route prefers. Where the address cannot originate the request, fall back
        // to the default egress: on a NATed host the service observes the NAT's public address,
        // which is the same address the daemon discovered.
        let binding = probe_source_binding(source_addr, &url);
        match binding {
            SourceBinding::Unroutable => tracing::warn!(
                %source_addr,
                %url,
                "this host cannot reach the verification service from the address being \
                 provisioned; the request will use the default egress and the service will \
                 observe a different address"
            ),
            other => debug!(%source_addr, %url, binding = ?other, "verification request source"),
        }

        let client = Self::build(binding, source_addr, &url)?;
        debug!(%url, %payer, "requesting an IP ownership proof");

        // The client is built once and reused, so a retry re-resolves through the same pool
        // rather than rebuilding a socket binding that already succeeded or already failed.
        let mut attempt = 1;
        loop {
            match attempt_request(&client, &url, payer, user_type) {
                Ok(proof) => return Ok(proof),
                Err((err, retryable)) => {
                    if !retryable || attempt == MAX_ATTEMPTS {
                        return Err(err);
                    }
                    debug!(
                        attempt,
                        error = %err,
                        "the verification request failed; trying once more"
                    );
                    thread::sleep(RETRY_DELAY);
                    attempt += 1;
                }
            }
        }
    }
}

/// One request. The flag says whether the failure is worth another attempt — see
/// [`MAX_ATTEMPTS`] for why only some are.
fn attempt_request(
    client: &reqwest::blocking::Client,
    url: &str,
    payer: Pubkey,
    user_type: UserType,
) -> Result<IpOwnershipProof, (IpProofError, bool)> {
    let unreachable = |e: reqwest::Error| {
        (
            IpProofError::Unreachable {
                url: url.to_string(),
                detail: e.to_string(),
            },
            true,
        )
    };

    let response = client
        .post(url)
        .json(&serde_json::json!({
            "payer": payer.to_string(),
            "user_type": user_type as u8,
        }))
        .send()
        .map_err(unreachable)?;

    let status = response.status();
    let body = response.text().map_err(unreachable)?;

    if !status.is_success() {
        return Err(classify_failure(status, &body));
    }

    let parsed: ProofResponse = serde_json::from_str(&body).map_err(|e| {
        (
            IpProofError::Malformed(format!("{e} (body: {})", body.trim())),
            false,
        )
    })?;
    proof_from_response(parsed, payer, user_type).map_err(|e| (e, false))
}

/// Turns a non-success response into the reason the operator sees, and says whether another
/// attempt could change it. A 5xx could land on a healthy host behind the same name; a 4xx is
/// the service's considered answer, and retrying `rate_limited` would only make it worse.
fn classify_failure(status: reqwest::StatusCode, body: &str) -> (IpProofError, bool) {
    // The service's own reason string, when it sent one. A proxy or captive portal in the path
    // will not have, so fall back to the status and whatever body arrived.
    let err = match serde_json::from_str::<ErrorResponse>(body) {
        Ok(err) => IpProofError::Declined {
            reason: err.error,
            message: err.message,
        },
        Err(_) => IpProofError::Declined {
            reason: format!("http_{}", status.as_u16()),
            message: body.trim().chars().take(200).collect(),
        },
    };
    (err, status.is_server_error())
}

/// Converts the wire form into the struct the instruction carries, rejecting anything the
/// program would reject anyway.
///
/// `requested_payer` and `requested_user_type` are what this client asked for. The proof binds
/// both, and the program checks both, so a proof that names something else is one the transaction
/// would pay for and then be refused. Catching it here turns that into the clean "continuing
/// without a proof" path instead.
fn proof_from_response(
    parsed: ProofResponse,
    requested_payer: Pubkey,
    requested_user_type: UserType,
) -> Result<IpOwnershipProof, IpProofError> {
    // A verifier rolled forward to a layout this client does not know signs a message this
    // client cannot reason about. Refuse it rather than forward a proof the program will reject.
    if !doublezero_ip_proof::is_supported_version(parsed.version) {
        return Err(IpProofError::Malformed(format!(
            "proof layout version {} is not supported by this client (supported: {:?})",
            parsed.version,
            doublezero_ip_proof::SUPPORTED_IP_PROOF_VERSIONS,
        )));
    }

    let payer = Pubkey::from_str(&parsed.payer)
        .map_err(|e| IpProofError::Malformed(format!("payer '{}': {e}", parsed.payer)))?;
    if payer != requested_payer {
        return Err(IpProofError::Malformed(format!(
            "proof names payer {payer}, but {requested_payer} was requested"
        )));
    }
    if parsed.user_type != requested_user_type as u8 {
        return Err(IpProofError::Malformed(format!(
            "proof names user type {}, but {} ({}) was requested",
            parsed.user_type, requested_user_type, requested_user_type as u8
        )));
    }
    let signature = Signature::from_str(&parsed.signature)
        .map_err(|e| IpProofError::Malformed(format!("signature '{}': {e}", parsed.signature)))?;
    let signature: [u8; 64] = signature.as_ref().try_into().map_err(|_| {
        IpProofError::Malformed(format!("signature '{}' is not 64 bytes", parsed.signature))
    })?;

    Ok(IpOwnershipProof {
        version: parsed.version,
        payer,
        client_ip: parsed.client_ip,
        epoch: parsed.epoch,
        user_type: parsed.user_type,
        signature,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    /// The binding probe has to answer for a real socket, because the thing it is standing in
    /// for — reqwest's per-connection `bind()` — is not observable at build time.
    #[test]
    fn probe_binds_a_local_address_toward_a_reachable_destination() {
        // A loopback source toward a loopback destination is the one pairing guaranteed to be
        // legal on every host a test runs on.
        assert_eq!(
            probe_source_binding(Ipv4Addr::LOCALHOST, "http://127.0.0.1:8080"),
            SourceBinding::Bind
        );
    }

    #[test]
    fn probe_skips_an_address_on_no_local_interface() {
        // The NATed host: `client_ip` is the NAT's public address, assigned to nothing here.
        assert_eq!(
            probe_source_binding(Ipv4Addr::new(203, 0, 113, 7), "http://127.0.0.1:8080"),
            SourceBinding::NotLocal
        );
    }

    #[test]
    fn probe_skips_a_local_address_that_cannot_reach_the_destination() {
        // The mirror of the localnet default, which is the case this arm exists for: a source
        // the host owns but which is not a legal source for the route to the destination. No
        // packet is sent and no name is resolved — the kernel rejects the route lookup itself.
        assert_eq!(
            probe_source_binding(Ipv4Addr::LOCALHOST, "http://8.8.8.8:80"),
            SourceBinding::Unroutable
        );
    }

    /// The deadline is the whole point of the split: a resolver that hangs must not hold up an
    /// interactive `connect`, and giving up has to be prompt rather than eventual.
    #[test]
    fn resolve_within_gives_up_on_a_lookup_that_outruns_its_deadline() {
        let started = std::time::Instant::now();
        let resolved = resolve_within(Duration::from_millis(50), || {
            thread::sleep(Duration::from_secs(30));
            Some(vec![SocketAddr::from(([127, 0, 0, 1], 8080))])
        });

        assert!(
            resolved.is_none(),
            "a lookup past the deadline must be abandoned"
        );
        // The call returns on the deadline, not when the lookup eventually finishes. A generous
        // bound: the point is that it is nowhere near the 30s the closure sleeps for.
        assert!(
            started.elapsed() < Duration::from_secs(5),
            "gave up only after {:?}",
            started.elapsed()
        );
    }

    #[test]
    fn resolve_within_returns_a_lookup_that_beats_its_deadline() {
        let addr = SocketAddr::from(([127, 0, 0, 1], 8080));
        assert_eq!(
            resolve_within(Duration::from_secs(30), move || Some(vec![addr])),
            Some(vec![addr])
        );
    }

    #[test]
    fn probe_defers_to_the_request_when_the_url_is_unusable() {
        // Nothing to bind toward. The request itself reports this better than the probe could.
        assert_eq!(
            probe_source_binding(Ipv4Addr::LOCALHOST, "not a url"),
            SourceBinding::Unresolved
        );
    }

    fn response() -> ProofResponse {
        // A fixed payer, so a test can rebuild the same response and compare against it.
        ProofResponse {
            version: 1,
            payer: Pubkey::from([9u8; 32]).to_string(),
            client_ip: Ipv4Addr::new(203, 0, 113, 7),
            epoch: 931,
            user_type: UserType::IBRL as u8,
            signature: Signature::from([7u8; 64]).to_string(),
        }
    }

    /// The payer and user type `response()` was issued for, so a test only has to name the field
    /// it is deliberately breaking.
    fn requested() -> (Pubkey, UserType) {
        (Pubkey::from([9u8; 32]), UserType::IBRL)
    }

    fn parse(wire: ProofResponse) -> Result<IpOwnershipProof, IpProofError> {
        let (payer, user_type) = requested();
        proof_from_response(wire, payer, user_type)
    }

    #[test]
    fn test_proof_from_response_round_trips_the_wire_form() {
        let wire = response();
        let proof = parse(response()).expect("a well-formed response must parse");

        assert_eq!(proof.version, 1);
        assert_eq!(proof.payer.to_string(), wire.payer);
        assert_eq!(proof.client_ip, Ipv4Addr::new(203, 0, 113, 7));
        assert_eq!(proof.epoch, 931);
        assert_eq!(proof.user_type, UserType::IBRL as u8);
        assert_eq!(proof.signature, [7u8; 64]);
    }

    #[test]
    fn test_proof_from_response_rejects_a_bad_signature() {
        let err = parse(ProofResponse {
            signature: "not-base58!".to_string(),
            ..response()
        })
        .expect_err("a signature that is not base58 must not become a proof");
        assert!(matches!(err, IpProofError::Malformed(_)), "{err}");
    }

    #[test]
    fn test_proof_from_response_rejects_a_bad_payer() {
        let err = parse(ProofResponse {
            payer: "nope".to_string(),
            ..response()
        })
        .expect_err("a payer that is not a pubkey must not become a proof");
        assert!(matches!(err, IpProofError::Malformed(_)), "{err}");
    }

    /// A verifier rolled forward to a layout this client does not know. Refusing here is the
    /// difference between "continuing without a proof" and a user creation that is paid for and
    /// then refused onchain.
    #[test]
    fn test_proof_from_response_rejects_an_unsupported_layout_version() {
        let err = parse(ProofResponse {
            version: 2,
            ..response()
        })
        .expect_err("an unknown layout version must not become a proof");
        assert!(matches!(err, IpProofError::Malformed(_)), "{err}");
        assert!(err.to_string().contains("version 2"), "{err}");
    }

    #[test]
    fn test_proof_from_response_rejects_a_proof_for_another_payer() {
        let err = parse(ProofResponse {
            payer: Pubkey::from([3u8; 32]).to_string(),
            ..response()
        })
        .expect_err("a proof naming another payer must not become a proof");
        assert!(matches!(err, IpProofError::Malformed(_)), "{err}");
    }

    #[test]
    fn test_proof_from_response_rejects_a_proof_for_another_user_type() {
        let err = parse(ProofResponse {
            user_type: UserType::Multicast as u8,
            ..response()
        })
        .expect_err("a proof naming another user type must not become a proof");
        assert!(matches!(err, IpProofError::Malformed(_)), "{err}");
    }

    /// Serves one canned response per connection, in order, and reports how many connections it
    /// actually saw. `Connection: close` on every response, so each attempt is its own
    /// connection and the count is the attempt count. Note that is the test making the count
    /// legible, not what a real server does: against a keep-alive server the retry after a 5xx
    /// reuses the pooled connection, which is why [`MAX_ATTEMPTS`] claims a transient blip
    /// rather than failover.
    fn canned_server(responses: Vec<String>) -> (String, std::thread::JoinHandle<usize>) {
        let listener = std::net::TcpListener::bind("127.0.0.1:0").expect("bind a test listener");
        let url = format!("http://{}", listener.local_addr().expect("a local address"));

        let handle = std::thread::spawn(move || {
            let mut served = 0;
            for response in responses {
                let Ok((mut stream, _)) = listener.accept() else {
                    break;
                };
                // Drain the request head so the client is not answered mid-write, then reply.
                // The body is not inspected: what is under test is the retry, not the request.
                let mut buf = [0u8; 4096];
                let _ = std::io::Read::read(&mut stream, &mut buf);
                let _ = std::io::Write::write_all(&mut stream, response.as_bytes());
                served += 1;
            }
            served
        });

        (url, handle)
    }

    fn http(status: &str, body: &str) -> String {
        format!(
            "HTTP/1.1 {status}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
            body.len()
        )
    }

    /// The retry has to actually happen: a verifier fleet behind one name is the reason it
    /// exists, and a loop that gave up after the first 5xx would look identical in every other
    /// test.
    #[test]
    fn test_a_server_error_is_retried_and_the_second_attempt_is_used() {
        let wire = response();
        let body = format!(
            r#"{{"version":{},"payer":"{}","client_ip":"{}","epoch":{},"user_type":{},"signature":"{}"}}"#,
            wire.version, wire.payer, wire.client_ip, wire.epoch, wire.user_type, wire.signature
        );
        let (url, server) = canned_server(vec![
            http(
                "503 Service Unavailable",
                r#"{"error":"unavailable","message":"restarting"}"#,
            ),
            http("200 OK", &body),
        ]);

        let proof = HttpIpProofClient::new(Some(url))
            .request_proof(
                Pubkey::from([9u8; 32]),
                UserType::IBRL,
                Ipv4Addr::new(127, 0, 0, 1),
            )
            .expect("the second attempt must produce the proof");

        assert_eq!(proof.client_ip, wire.client_ip);
        assert_eq!(proof.epoch, wire.epoch);
        assert_eq!(server.join().expect("the server thread"), 2);
    }

    /// The mirror image: a decline must be reported from the first attempt, without a second
    /// request. `rate_limited` is the case where a retry would do harm.
    #[test]
    fn test_a_decline_is_not_retried_over_the_wire() {
        // One response only. A second attempt would find the listener gone and report
        // Unreachable instead, so the assertion below is what pins the single attempt.
        let (url, server) = canned_server(vec![http(
            "429 Too Many Requests",
            r#"{"error":"rate_limited","message":"try again in 60s"}"#,
        )]);

        let err = HttpIpProofClient::new(Some(url))
            .request_proof(
                Pubkey::from([9u8; 32]),
                UserType::IBRL,
                Ipv4Addr::new(127, 0, 0, 1),
            )
            .expect_err("a rate-limited request cannot produce a proof");

        assert!(err.to_string().contains("rate_limited"), "{err}");
        assert_eq!(server.join().expect("the server thread"), 1);
    }

    /// A 5xx is worth a second attempt, because the name may resolve to more than one host and
    /// the next one may be healthy.
    #[test]
    fn test_a_server_error_is_retryable() {
        let (err, retryable) = classify_failure(
            reqwest::StatusCode::SERVICE_UNAVAILABLE,
            "upstream is restarting",
        );
        assert!(retryable, "{err}");
        assert!(matches!(err, IpProofError::Declined { .. }), "{err}");
    }

    /// Retrying a rate limit only makes it worse, and the operator needs the service's own
    /// reason rather than a second identical refusal.
    #[test]
    fn test_a_decline_is_not_retryable() {
        let (err, retryable) = classify_failure(
            reqwest::StatusCode::TOO_MANY_REQUESTS,
            r#"{"error":"rate_limited","message":"try again in 60s"}"#,
        );
        assert!(!retryable, "{err}");
        match err {
            IpProofError::Declined { reason, message } => {
                assert_eq!(reason, "rate_limited");
                assert_eq!(message, "try again in 60s");
            }
            other => panic!("expected a decline carrying the service's reason, got {other}"),
        }
    }

    /// A CGNAT source is the refusal operators will actually hit, and it must not be retried.
    #[test]
    fn test_a_non_routable_source_is_not_retryable() {
        let (err, retryable) = classify_failure(
            reqwest::StatusCode::BAD_REQUEST,
            r#"{"error":"not_globally_routable","message":"100.64.0.1 is not globally routable"}"#,
        );
        assert!(!retryable, "{err}");
        assert!(err.to_string().contains("not_globally_routable"), "{err}");
    }

    /// An environment with no verifier must be distinguishable from one whose verifier is down:
    /// the first is expected during rollout, the second is worth investigating.
    #[test]
    fn test_no_configured_url_reports_not_configured() {
        let err = HttpIpProofClient::new(None)
            .request_proof(
                Pubkey::new_unique(),
                UserType::IBRL,
                Ipv4Addr::new(203, 0, 113, 7),
            )
            .expect_err("an unconfigured client cannot produce a proof");
        assert!(matches!(err, IpProofError::NotConfigured), "{err}");
    }
}
