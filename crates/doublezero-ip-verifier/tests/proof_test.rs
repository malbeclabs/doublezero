//! End-to-end tests against a running instance of the service.
//!
//! Each test binds the real router on an ephemeral loopback port and talks to it over HTTP, so the
//! peer-address plumbing — the part that decides what gets signed — is exercised for real rather
//! than mocked out.

use doublezero_ip_proof::{
    signed_message_for, test_vectors, verify, IpOwnershipProof, IP_PROOF_VERSION,
};
use doublezero_ip_verifier::{
    authority::AuthorityWatch,
    client_ip::ForwardedHeader,
    epoch::EpochCache,
    rate_limit::RateLimiter,
    server::{router, AppState, RequestLimits},
};
use ipnetwork::IpNetwork;
use serde_json::json;
use solana_keypair::Keypair;
use solana_program::pubkey::Pubkey;
use solana_signature::Signature;
use solana_signer::Signer;
use std::{
    net::{Ipv4Addr, SocketAddr},
    str::FromStr,
    sync::Arc,
    time::Duration,
};

const EPOCH: u64 = 931;

struct Service {
    base_url: String,
    verifier_pubkey: Pubkey,
}

/// Whether the ledger names this service's key as the verifier authority.
enum Authority {
    Matches,
    /// `GlobalState` names someone else — a rotation this instance was not redeployed for.
    Mismatch,
}

/// Starts the service on loopback. `trusted_proxies` covers 127.0.0.0/8 in the forwarded-header
/// tests, which is the only way a test client can present itself as a proxy.
async fn start(trusted_proxies: &[&str], epoch: Option<u64>, burst: u32) -> Service {
    start_with(
        trusted_proxies,
        epoch,
        burst,
        ForwardedHeader::XForwardedFor,
        Authority::Matches,
    )
    .await
}

async fn start_with(
    trusted_proxies: &[&str],
    epoch: Option<u64>,
    burst: u32,
    forwarded_header: ForwardedHeader,
    authority: Authority,
) -> Service {
    let verifier = Arc::new(Keypair::new());
    let verifier_pubkey = verifier.pubkey();

    let cache = Arc::new(EpochCache::new(Duration::from_secs(3600)));
    if let Some(epoch) = epoch {
        cache.store(epoch);
    }

    let watch = Arc::new(AuthorityWatch::new(verifier_pubkey));
    watch.observe(match authority {
        Authority::Matches => verifier_pubkey,
        Authority::Mismatch => Pubkey::new_unique(),
    });

    let state = AppState::new(
        verifier,
        cache,
        watch,
        Arc::new(RateLimiter::new(burst, 60, 1024)),
        trusted_proxies
            .iter()
            .map(|cidr| IpNetwork::from_str(cidr).expect("test CIDR is valid"))
            .collect(),
        forwarded_header,
    );

    let listener = tokio::net::TcpListener::bind("127.0.0.1:0")
        .await
        .expect("bind loopback");
    let addr = listener.local_addr().expect("local addr");

    tokio::spawn(async move {
        axum::serve(
            listener,
            router(
                state,
                RequestLimits {
                    max_body_bytes: 1024,
                    timeout: Duration::from_secs(5),
                },
            )
            .into_make_service_with_connect_info::<SocketAddr>(),
        )
        .await
        .expect("server runs");
    });

    Service {
        base_url: format!("http://{addr}"),
        verifier_pubkey,
    }
}

impl Service {
    async fn request_proof(&self, payer: &Pubkey, user_type: u8) -> reqwest::Response {
        self.request_proof_with_headers(payer, user_type, &[]).await
    }

    async fn request_proof_with_headers(
        &self,
        payer: &Pubkey,
        user_type: u8,
        headers: &[(&str, &str)],
    ) -> reqwest::Response {
        let mut request = reqwest::Client::new()
            .post(format!("{}/v1/proof", self.base_url))
            .json(&json!({ "payer": payer.to_string(), "user_type": user_type }));

        for (name, value) in headers {
            request = request.header(*name, *value);
        }

        request.send().await.expect("request reaches the service")
    }
}

/// Rebuilds the proof from the JSON body, the way a client would before putting it in a
/// transaction.
async fn proof_from(response: reqwest::Response) -> IpOwnershipProof {
    let body: serde_json::Value = response.json().await.expect("response is JSON");

    IpOwnershipProof {
        version: body["version"].as_u64().expect("version") as u8,
        payer: Pubkey::from_str(body["payer"].as_str().expect("payer")).expect("payer is base58"),
        client_ip: body["client_ip"]
            .as_str()
            .expect("client_ip")
            .parse()
            .expect("client_ip is an address"),
        epoch: body["epoch"].as_u64().expect("epoch"),
        user_type: body["user_type"].as_u64().expect("user_type") as u8,
        signature: Signature::from_str(body["signature"].as_str().expect("signature"))
            .expect("signature is base58")
            .into(),
    }
}

#[tokio::test]
async fn a_proof_is_issued_for_the_observed_source_address() {
    // 127.0.0.1 is not globally routable, so a request straight off loopback is refused. Presenting
    // as a trusted proxy is how the test supplies a routable client address.
    let service = start(&["127.0.0.0/8"], Some(EPOCH), 5).await;
    let payer = Pubkey::new_unique();

    let response = service
        .request_proof_with_headers(&payer, 3, &[("x-forwarded-for", "198.18.0.42")])
        .await;
    assert_eq!(response.status(), 200);

    let proof = proof_from(response).await;
    assert_eq!(proof.version, IP_PROOF_VERSION);
    assert_eq!(proof.payer, payer);
    assert_eq!(proof.client_ip, Ipv4Addr::new(198, 18, 0, 42));
    assert_eq!(proof.epoch, EPOCH);
    assert_eq!(proof.user_type, 3);

    // The signature verifies, and it covers exactly the bytes the program reconstructs from the
    // instruction arguments — the program builds this same `signed_message_for` and hands it to the
    // Ed25519 precompile.
    assert_eq!(verify(&proof, &service.verifier_pubkey), Ok(()));
    assert_eq!(
        proof.signed_message(),
        signed_message_for(
            IP_PROOF_VERSION,
            &payer,
            &Ipv4Addr::new(198, 18, 0, 42),
            EPOCH,
            3
        )
    );
}

#[tokio::test]
async fn a_client_supplied_client_ip_is_rejected_rather_than_ignored() {
    let service = start(&["127.0.0.0/8"], Some(EPOCH), 5).await;

    let response = reqwest::Client::new()
        .post(format!("{}/v1/proof", service.base_url))
        .header("x-forwarded-for", "198.18.0.42")
        .json(&json!({
            "payer": Pubkey::new_unique().to_string(),
            "user_type": 0,
            "client_ip": "198.18.0.42",
        }))
        .send()
        .await
        .expect("request reaches the service");

    assert_eq!(response.status(), 400);
    let body: serde_json::Value = response.json().await.expect("response is JSON");
    assert_eq!(body["error"], "invalid_request");
}

#[tokio::test]
async fn a_loopback_source_is_refused_as_not_globally_routable() {
    let service = start(&[], Some(EPOCH), 5).await;

    let response = service.request_proof(&Pubkey::new_unique(), 0).await;
    assert_eq!(response.status(), 400);

    let body: serde_json::Value = response.json().await.expect("response is JSON");
    assert_eq!(body["error"], "not_globally_routable");
}

#[tokio::test]
async fn a_private_forwarded_source_is_refused() {
    let service = start(&["127.0.0.0/8"], Some(EPOCH), 5).await;

    for address in ["10.0.0.7", "100.64.0.7", "192.168.1.7", "203.0.113.7"] {
        let response = service
            .request_proof_with_headers(&Pubkey::new_unique(), 0, &[("x-forwarded-for", address)])
            .await;
        assert_eq!(response.status(), 400, "{address}");

        let body: serde_json::Value = response.json().await.expect("response is JSON");
        assert_eq!(body["error"], "not_globally_routable", "{address}");
    }
}

#[tokio::test]
async fn an_ipv6_source_is_refused_with_a_clear_error() {
    let service = start(&["127.0.0.0/8"], Some(EPOCH), 5).await;

    let response = service
        .request_proof_with_headers(
            &Pubkey::new_unique(),
            0,
            &[("x-forwarded-for", "2001:db8::1")],
        )
        .await;
    assert_eq!(response.status(), 400);

    let body: serde_json::Value = response.json().await.expect("response is JSON");
    assert_eq!(body["error"], "ipv6_unsupported");
    assert!(
        body["message"]
            .as_str()
            .expect("message")
            .contains("IPv4 address"),
        "message names the layout limitation: {}",
        body["message"]
    );
}

#[tokio::test]
async fn a_forwarded_header_from_an_untrusted_peer_does_not_move_the_address() {
    // No trusted proxies: the header is ignored, so the request is judged on loopback and refused
    // rather than signing the address the client claimed.
    let service = start(&[], Some(EPOCH), 5).await;

    let response = service
        .request_proof_with_headers(
            &Pubkey::new_unique(),
            0,
            &[("x-forwarded-for", "198.18.0.42")],
        )
        .await;
    assert_eq!(response.status(), 400);

    let body: serde_json::Value = response.json().await.expect("response is JSON");
    assert_eq!(body["error"], "not_globally_routable");
    assert!(
        body["message"].as_str().expect("message").contains("127."),
        "the refusal names the peer address, not the claimed one: {}",
        body["message"]
    );
}

#[tokio::test]
async fn a_spoofed_hop_ahead_of_the_proxy_hop_is_ignored() {
    let service = start(&["127.0.0.0/8"], Some(EPOCH), 5).await;

    let response = service
        .request_proof_with_headers(
            &Pubkey::new_unique(),
            0,
            &[("x-forwarded-for", "198.18.0.99, 198.18.0.42")],
        )
        .await;
    assert_eq!(response.status(), 200);

    let proof = proof_from(response).await;
    assert_eq!(proof.client_ip, Ipv4Addr::new(198, 18, 0, 42));
}

#[tokio::test]
async fn an_unfetched_epoch_fails_closed() {
    let service = start(&["127.0.0.0/8"], None, 5).await;

    let response = service
        .request_proof_with_headers(
            &Pubkey::new_unique(),
            0,
            &[("x-forwarded-for", "198.18.0.42")],
        )
        .await;
    assert_eq!(response.status(), 503);

    let body: serde_json::Value = response.json().await.expect("response is JSON");
    assert_eq!(body["error"], "epoch_unavailable");
}

#[tokio::test]
async fn health_reports_readiness_from_the_epoch_cache() {
    let unready = start(&[], None, 5).await;
    let response = reqwest::get(format!("{}/health", unready.base_url))
        .await
        .expect("health request");
    assert_eq!(response.status(), 503);

    let ready = start(&[], Some(EPOCH), 5).await;
    let response = reqwest::get(format!("{}/health", ready.base_url))
        .await
        .expect("health request");
    assert_eq!(response.status(), 200);

    let body: serde_json::Value = response.json().await.expect("response is JSON");
    assert_eq!(body["status"], "ok");
    assert_eq!(body["epoch"], EPOCH);
    assert_eq!(body["verifier_key"], "matches");
}

#[tokio::test]
async fn requests_past_the_burst_are_rate_limited() {
    let service = start(&["127.0.0.0/8"], Some(EPOCH), 2).await;
    let headers = [("x-forwarded-for", "198.18.0.42")];

    for _ in 0..2 {
        let response = service
            .request_proof_with_headers(&Pubkey::new_unique(), 0, &headers)
            .await;
        assert_eq!(response.status(), 200);
    }

    let response = service
        .request_proof_with_headers(&Pubkey::new_unique(), 0, &headers)
        .await;
    assert_eq!(response.status(), 429);

    let body: serde_json::Value = response.json().await.expect("response is JSON");
    assert_eq!(body["error"], "rate_limited");

    // The limit is per source address, so a different forwarded client is unaffected.
    let response = service
        .request_proof_with_headers(
            &Pubkey::new_unique(),
            0,
            &[("x-forwarded-for", "198.18.0.43")],
        )
        .await;
    assert_eq!(response.status(), 200);
}

#[tokio::test]
async fn an_oversized_body_is_rejected() {
    let service = start(&["127.0.0.0/8"], Some(EPOCH), 5).await;

    let response = reqwest::Client::new()
        .post(format!("{}/v1/proof", service.base_url))
        .header("content-type", "application/json")
        .body("x".repeat(4096))
        .send()
        .await
        .expect("request reaches the service");

    assert_eq!(response.status(), 413);
}

#[tokio::test]
async fn issued_signatures_match_the_committed_test_vectors() {
    // The vectors use documentation addresses, which `is_global` refuses, so they cannot be driven
    // through the HTTP path. What is checked here is the layer the handler calls: the same key, the
    // same fields, the same bytes on the wire as the program and the CLI expect.
    let keypair = Keypair::try_from(
        hex::decode(test_vectors::VERIFIER_KEYPAIR_HEX)
            .expect("vector keypair is hex")
            .as_slice(),
    )
    .expect("vector keypair is a valid ed25519 keypair");
    assert_eq!(keypair.pubkey(), test_vectors::verifier_pubkey());

    for vector in test_vectors::ALL {
        let proof = doublezero_ip_proof::sign(
            &keypair,
            &vector.payer(),
            &vector.client_ip(),
            vector.epoch,
            vector.user_type,
        );

        assert_eq!(proof.signature, vector.signature(), "{}", vector.name);
        assert_eq!(
            proof.signed_message(),
            vector.signed_message(),
            "{}",
            vector.name
        );
    }
}

#[tokio::test]
async fn a_rotated_verifier_authority_takes_the_instance_out_of_service() {
    // The key still signs perfectly well; the program just will not accept it any more. Failing
    // here beats handing back a proof that dies onchain where the client cannot see why.
    let service = start_with(
        &["127.0.0.0/8"],
        Some(EPOCH),
        5,
        ForwardedHeader::XForwardedFor,
        Authority::Mismatch,
    )
    .await;

    let response = service
        .request_proof_with_headers(
            &Pubkey::new_unique(),
            0,
            &[("x-forwarded-for", "198.18.0.42")],
        )
        .await;
    assert_eq!(response.status(), 503);

    let body: serde_json::Value = response.json().await.expect("response is JSON");
    assert_eq!(body["error"], "verifier_key_mismatch");

    let response = reqwest::get(format!("{}/health", service.base_url))
        .await
        .expect("health request");
    assert_eq!(response.status(), 503);

    let body: serde_json::Value = response.json().await.expect("response is JSON");
    assert_eq!(body["verifier_key"], "mismatch");
}

#[tokio::test]
async fn only_the_configured_forwarded_header_is_read() {
    // A proxy writing RFC 7239 `Forwarded` while passing the client's `X-Forwarded-For` through:
    // the claimed address must not win.
    let service = start_with(
        &["127.0.0.0/8"],
        Some(EPOCH),
        5,
        ForwardedHeader::Forwarded,
        Authority::Matches,
    )
    .await;

    let response = service
        .request_proof_with_headers(
            &Pubkey::new_unique(),
            0,
            &[
                ("x-forwarded-for", "198.18.0.99"),
                ("forwarded", "for=198.18.0.42"),
            ],
        )
        .await;
    assert_eq!(response.status(), 200);

    let proof = proof_from(response).await;
    assert_eq!(proof.client_ip, Ipv4Addr::new(198, 18, 0, 42));
}

#[tokio::test]
async fn an_unparsable_hop_left_of_the_client_hop_is_tolerated() {
    // nginx's `$proxy_add_x_forwarded_for` concatenates whatever the client sent, and `unknown` is
    // a real value clients emit. The hop the proxy observed is still trustworthy.
    let service = start(&["127.0.0.0/8"], Some(EPOCH), 5).await;

    let response = service
        .request_proof_with_headers(
            &Pubkey::new_unique(),
            0,
            &[("x-forwarded-for", "unknown, 198.18.0.42")],
        )
        .await;
    assert_eq!(response.status(), 200);

    let proof = proof_from(response).await;
    assert_eq!(proof.client_ip, Ipv4Addr::new(198, 18, 0, 42));
}

#[tokio::test]
async fn an_unresolvable_chain_is_a_client_error_and_is_rate_limited() {
    let service = start(&["127.0.0.0/8"], Some(EPOCH), 2).await;
    let headers = [("x-forwarded-for", "unknown")];

    for _ in 0..2 {
        let response = service
            .request_proof_with_headers(&Pubkey::new_unique(), 0, &headers)
            .await;
        // 4xx, not 5xx: a stranger's header must not page anyone.
        assert_eq!(response.status(), 400);

        let body: serde_json::Value = response.json().await.expect("response is JSON");
        assert_eq!(body["error"], "invalid_forwarded_header");
    }

    // Charged against the peer address, so the flood is metered even with no client address to
    // charge it to.
    let response = service
        .request_proof_with_headers(&Pubkey::new_unique(), 0, &headers)
        .await;
    assert_eq!(response.status(), 429);
}
