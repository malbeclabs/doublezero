//! HTTP surface: `POST /v1/proof`, plus `GET /health`.
//!
//! Built on axum. The workspace had no Rust HTTP server framework before this crate; axum was
//! chosen over hand-rolling on hyper so that the body-size cap, the request timeout, and — most
//! importantly — the connection peer address all come from reviewed middleware rather than from
//! this crate. The peer address is the whole security property (see [`crate::client_ip`]), and it is
//! not somewhere to be inventing plumbing.

use crate::{
    client_ip::{resolve_client_ip, ClientIpError, ResolvedClientIp},
    epoch::{EpochCache, EpochError},
    rate_limit::RateLimiter,
};
use axum::{
    body::Bytes,
    extract::{ConnectInfo, DefaultBodyLimit, State},
    http::{HeaderMap, StatusCode},
    response::{IntoResponse, Response},
    routing::{get, post},
    Json, Router,
};
use doublezero_ip_proof::{sign, IpOwnershipProof};
use doublezero_serviceability::helper::is_global;
use ipnetwork::IpNetwork;
use serde::{Deserialize, Serialize};
use solana_keypair::Keypair;
use solana_program::pubkey::Pubkey;
use solana_signature::Signature;
use std::{
    net::{IpAddr, Ipv4Addr, SocketAddr},
    str::FromStr,
    sync::Arc,
    time::Duration,
};
use tower_http::{limit::RequestBodyLimitLayer, timeout::TimeoutLayer, trace::TraceLayer};
use tracing::{info, warn};

/// Everything a request needs. Cheap to clone: each field is behind an `Arc`.
#[derive(Clone)]
pub struct AppState {
    /// The signing key. Only ever used to sign; never logged, never serialized, never returned.
    verifier: Arc<Keypair>,
    epoch: Arc<EpochCache>,
    limiter: Arc<RateLimiter>,
    trusted_proxies: Arc<Vec<IpNetwork>>,
}

impl AppState {
    pub fn new(
        verifier: Arc<Keypair>,
        epoch: Arc<EpochCache>,
        limiter: Arc<RateLimiter>,
        trusted_proxies: Vec<IpNetwork>,
    ) -> Self {
        Self {
            verifier,
            epoch,
            limiter,
            trusted_proxies: Arc::new(trusted_proxies),
        }
    }
}

/// Caps applied to every request.
#[derive(Debug, Clone, Copy)]
pub struct RequestLimits {
    pub max_body_bytes: usize,
    pub timeout: Duration,
}

/// A proof request. `client_ip` is deliberately absent, and `deny_unknown_fields` makes a client
/// that tries to send one fail loudly instead of having the field quietly ignored — a caller under
/// the impression it can name its own address should learn otherwise immediately.
#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct ProofRequest {
    /// Base58 pubkey of the account that will pay for and own the user being created.
    pub payer: String,
    /// Serviceability `UserType` discriminant the proof authorizes. Client-supplied and not a trust
    /// boundary: the program checks it against the user actually being created, and the proof binds
    /// it so a proof for one connection type cannot be replayed into another.
    pub user_type: u8,
}

/// Mirrors `IpOwnershipProof` field for field, with base58 pubkey and signature.
///
/// The verifier public key is *not* included. A client must take it from `GlobalState` onchain —
/// the same place the program reads it — so that a compromised or impersonated service cannot talk
/// a client into building an Ed25519 instruction naming a key the program will not accept.
#[derive(Debug, Serialize)]
pub struct ProofResponse {
    pub version: u8,
    pub payer: String,
    pub client_ip: Ipv4Addr,
    pub epoch: u64,
    pub user_type: u8,
    pub signature: String,
}

impl From<IpOwnershipProof> for ProofResponse {
    fn from(proof: IpOwnershipProof) -> Self {
        Self {
            version: proof.version,
            payer: proof.payer.to_string(),
            client_ip: proof.client_ip,
            epoch: proof.epoch,
            user_type: proof.user_type,
            signature: Signature::from(proof.signature).to_string(),
        }
    }
}

#[derive(Debug, Serialize)]
pub struct ErrorResponse {
    /// Stable machine-readable reason, also the value of the `reason` metric label.
    pub error: &'static str,
    pub message: String,
}

/// A refusal to issue a proof. Every variant carries the stable reason string a client and a
/// dashboard can both key off.
#[derive(thiserror::Error, Debug)]
pub enum ProofError {
    #[error("{0}")]
    InvalidRequest(String),
    #[error("rate limit exceeded for {0}")]
    RateLimited(IpAddr),
    #[error("IPv6 source addresses are not supported: the proof layout carries an IPv4 address")]
    Ipv6Unsupported,
    #[error("{0} is not a globally routable address")]
    NotGloballyRoutable(Ipv4Addr),
    #[error("{0}")]
    ClientIpUnresolved(#[from] ClientIpError),
    #[error("{0}")]
    EpochUnavailable(#[from] EpochError),
}

impl ProofError {
    fn reason(&self) -> &'static str {
        match self {
            Self::InvalidRequest(_) => "invalid_request",
            Self::RateLimited(_) => "rate_limited",
            Self::Ipv6Unsupported => "ipv6_unsupported",
            Self::NotGloballyRoutable(_) => "not_globally_routable",
            Self::ClientIpUnresolved(_) => "client_ip_unresolved",
            Self::EpochUnavailable(_) => "epoch_unavailable",
        }
    }

    fn status(&self) -> StatusCode {
        match self {
            Self::InvalidRequest(_) | Self::Ipv6Unsupported | Self::NotGloballyRoutable(_) => {
                StatusCode::BAD_REQUEST
            }
            Self::RateLimited(_) => StatusCode::TOO_MANY_REQUESTS,
            // A malformed forwarded header is the client's fault only if it reached us directly,
            // and it cannot have: headers are read solely for connections from a trusted proxy. So
            // an unresolvable chain means our own proxy configuration is wrong.
            Self::ClientIpUnresolved(_) => StatusCode::INTERNAL_SERVER_ERROR,
            Self::EpochUnavailable(_) => StatusCode::SERVICE_UNAVAILABLE,
        }
    }
}

impl IntoResponse for ProofError {
    fn into_response(self) -> Response {
        let reason = self.reason();
        metrics::counter!("doublezero_ip_verifier_proofs_refused_total", "reason" => reason)
            .increment(1);
        warn!(reason, error = %self, "refused to issue an IP ownership proof");

        (
            self.status(),
            Json(ErrorResponse {
                error: reason,
                message: self.to_string(),
            }),
        )
            .into_response()
    }
}

/// Builds the router. Callers serve it with `into_make_service_with_connect_info::<SocketAddr>()`,
/// without which the peer address extractor is not available and every request would fail.
pub fn router(state: AppState, limits: RequestLimits) -> Router {
    Router::new()
        .route("/v1/proof", post(issue_proof))
        .route("/health", get(health))
        .layer(TraceLayer::new_for_http())
        .layer(TimeoutLayer::with_status_code(
            StatusCode::REQUEST_TIMEOUT,
            limits.timeout,
        ))
        // `DefaultBodyLimit::disable()` hands the cap to the explicit layer below so that one
        // number governs it, rather than two limits with the tighter one winning silently.
        .layer(DefaultBodyLimit::disable())
        .layer(RequestBodyLimitLayer::new(limits.max_body_bytes))
        .with_state(state)
}

async fn health(State(state): State<AppState>) -> Response {
    // Readiness, not just liveness: an instance that cannot name the current epoch cannot issue a
    // usable proof, and should be taken out of rotation rather than left answering requests.
    match state.epoch.current() {
        Ok(epoch) => (
            StatusCode::OK,
            Json(serde_json::json!({ "status": "ok", "epoch": epoch })),
        )
            .into_response(),
        Err(err) => (
            StatusCode::SERVICE_UNAVAILABLE,
            Json(serde_json::json!({ "status": "unavailable", "reason": err.to_string() })),
        )
            .into_response(),
    }
}

/// The body is taken as bytes rather than through the `Json` extractor so that the rate limit is
/// charged before any parsing work, and so a bad body produces this module's error shape.
async fn issue_proof(
    State(state): State<AppState>,
    ConnectInfo(peer): ConnectInfo<SocketAddr>,
    headers: HeaderMap,
    body: Bytes,
) -> Result<Json<ProofResponse>, ProofError> {
    let ResolvedClientIp { addr, source } =
        resolve_client_ip(peer, &headers, &state.trusted_proxies)?;

    if !state.limiter.check(addr) {
        return Err(ProofError::RateLimited(addr));
    }

    // IPv4 only until the proof layout gains an address family: an IPv6 client has no
    // representation in a v1 proof, and mapping it to something IPv4-shaped would attest an address
    // the client does not have.
    let client_ip = match addr {
        IpAddr::V4(addr) => addr,
        IpAddr::V6(_) => return Err(ProofError::Ipv6Unsupported),
    };

    // The program's own predicate. Signing an address it would reject produces a proof that can
    // only ever fail onchain, and a service that hands one out has told the client the wrong thing.
    if !is_global(client_ip) {
        return Err(ProofError::NotGloballyRoutable(client_ip));
    }

    let request: ProofRequest = serde_json::from_slice(&body)
        .map_err(|err| ProofError::InvalidRequest(format!("invalid request body: {err}")))?;
    let payer = Pubkey::from_str(&request.payer)
        .map_err(|err| ProofError::InvalidRequest(format!("invalid payer pubkey: {err}")))?;

    let epoch = state.epoch.current()?;
    let proof = sign(
        &state.verifier,
        &payer,
        &client_ip,
        epoch,
        request.user_type,
    );

    metrics::counter!("doublezero_ip_verifier_proofs_issued_total").increment(1);
    info!(
        %payer,
        %client_ip,
        epoch,
        user_type = request.user_type,
        address_source = ?source,
        peer = %peer.ip(),
        "issued an IP ownership proof"
    );
    Ok(Json(proof.into()))
}
