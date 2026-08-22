//! Checking this service's signing key against the one the program will accept.
//!
//! `GlobalState.ip_verifier_authority_pk` is the program's trust root for RFC-27 proofs. If the
//! wrong keypair is mounted, or the authority is rotated without this service being redeployed,
//! every proof it issues is still perfectly well-formed and still fails onchain — the service has no
//! way to notice from the signing path alone. So it asks the ledger who the authority is: at
//! startup, where a mismatch is fatal, and then periodically, where a mismatch takes the instance
//! out of rotation through `/health`.

use async_trait::async_trait;
use solana_program::pubkey::Pubkey;
use std::sync::{Arc, RwLock};
use tokio_util::sync::CancellationToken;
use tracing::{error, info, warn};

/// Reads `GlobalState.ip_verifier_authority_pk`. A trait so tests can drive the watcher without a
/// ledger.
#[async_trait]
pub trait AuthoritySource: Send + Sync {
    async fn ip_verifier_authority(&self) -> anyhow::Result<Pubkey>;
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AuthorityStatus {
    /// The ledger names this service's key. Proofs it issues can be accepted.
    Matches,
    /// The ledger names a different key. Every proof issued from here fails onchain.
    Mismatch { onchain: Pubkey },
    /// Nothing read yet, or the last read failed. Not treated as a failure: an RPC problem already
    /// shows up as a stale epoch, and refusing to serve on an unreadable `GlobalState` would turn a
    /// read timeout into an outage.
    Unknown,
}

impl AuthorityStatus {
    /// Whether the instance should keep answering requests.
    pub fn is_servable(&self) -> bool {
        !matches!(self, Self::Mismatch { .. })
    }

    pub fn as_str(&self) -> &'static str {
        match self {
            Self::Matches => "matches",
            Self::Mismatch { .. } => "mismatch",
            Self::Unknown => "unknown",
        }
    }
}

/// The last answer the ledger gave about the verifier authority.
pub struct AuthorityWatch {
    expected: Pubkey,
    status: RwLock<AuthorityStatus>,
}

impl AuthorityWatch {
    pub fn new(expected: Pubkey) -> Self {
        Self {
            expected,
            status: RwLock::new(AuthorityStatus::Unknown),
        }
    }

    pub fn status(&self) -> AuthorityStatus {
        *self
            .status
            .read()
            .expect("authority watch lock is never held across a panic")
    }

    /// Records what the ledger said, and returns the resulting status.
    pub fn observe(&self, onchain: Pubkey) -> AuthorityStatus {
        let status = if onchain == self.expected {
            AuthorityStatus::Matches
        } else {
            AuthorityStatus::Mismatch { onchain }
        };

        *self
            .status
            .write()
            .expect("authority watch lock is never held across a panic") = status;
        metrics::gauge!("doublezero_ip_verifier_authority_matches").set(
            if status == AuthorityStatus::Matches {
                1
            } else {
                0
            },
        );

        status
    }
}

/// Reads the authority once, before the service starts serving.
///
/// A mismatch is fatal: an instance that starts here would report healthy, count issued proofs, and
/// leave every user creation failing onchain with no signal on this side. An unreadable
/// `GlobalState` is only a warning — the ledger may be reachable a moment later, and the watcher
/// keeps trying.
pub async fn check_at_startup(
    watch: &AuthorityWatch,
    source: &dyn AuthoritySource,
) -> anyhow::Result<()> {
    match source.ip_verifier_authority().await {
        Ok(onchain) => match watch.observe(onchain) {
            AuthorityStatus::Matches => {
                info!("verifier key matches GlobalState.ip_verifier_authority_pk");
                Ok(())
            }
            AuthorityStatus::Mismatch { onchain } => Err(anyhow::anyhow!(
                "GlobalState.ip_verifier_authority_pk is {onchain}, not this service's key {}; \
                 every proof issued with this keypair would be rejected onchain",
                watch.expected
            )),
            AuthorityStatus::Unknown => unreachable!("observe never yields Unknown"),
        },
        Err(err) => {
            warn!(
                ?err,
                "could not read GlobalState.ip_verifier_authority_pk at startup; continuing and \
                 retrying in the background"
            );
            Ok(())
        }
    }
}

/// Re-reads the authority until cancelled, so a rotation that leaves this service behind takes it
/// out of rotation instead of silently breaking every connect.
pub async fn run_watcher(
    watch: Arc<AuthorityWatch>,
    source: Arc<dyn AuthoritySource>,
    interval: std::time::Duration,
    shutdown: CancellationToken,
) {
    loop {
        tokio::select! {
            biased;
            _ = shutdown.cancelled() => {
                info!("authority watcher shutting down");
                return;
            }
            _ = tokio::time::sleep(interval) => {}
        }

        match source.ip_verifier_authority().await {
            Ok(onchain) => {
                if let AuthorityStatus::Mismatch { onchain } = watch.observe(onchain) {
                    error!(
                        %onchain,
                        expected = %watch.expected,
                        "GlobalState.ip_verifier_authority_pk no longer names this service's key; \
                         refusing further requests"
                    );
                }
            }
            Err(err) => warn!(?err, "could not read GlobalState.ip_verifier_authority_pk"),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicU64, Ordering};

    struct StaticSource {
        authority: Pubkey,
    }

    #[async_trait]
    impl AuthoritySource for StaticSource {
        async fn ip_verifier_authority(&self) -> anyhow::Result<Pubkey> {
            Ok(self.authority)
        }
    }

    struct UnreadableSource {
        calls: AtomicU64,
    }

    #[async_trait]
    impl AuthoritySource for UnreadableSource {
        async fn ip_verifier_authority(&self) -> anyhow::Result<Pubkey> {
            self.calls.fetch_add(1, Ordering::SeqCst);
            Err(anyhow::anyhow!("rpc down"))
        }
    }

    #[tokio::test]
    async fn a_matching_authority_starts_and_is_servable() {
        let key = Pubkey::new_unique();
        let watch = AuthorityWatch::new(key);

        check_at_startup(&watch, &StaticSource { authority: key })
            .await
            .expect("a matching authority starts");

        assert_eq!(watch.status(), AuthorityStatus::Matches);
        assert!(watch.status().is_servable());
    }

    #[tokio::test]
    async fn a_mismatched_authority_refuses_to_start() {
        let onchain = Pubkey::new_unique();
        let watch = AuthorityWatch::new(Pubkey::new_unique());

        let err = check_at_startup(&watch, &StaticSource { authority: onchain })
            .await
            .expect_err("a mismatched authority is fatal");

        assert!(
            err.to_string().contains(&onchain.to_string()),
            "the error names the onchain key: {err}"
        );
        assert_eq!(watch.status(), AuthorityStatus::Mismatch { onchain });
        assert!(!watch.status().is_servable());
    }

    #[tokio::test]
    async fn an_unreadable_globalstate_starts_but_stays_unknown() {
        let watch = AuthorityWatch::new(Pubkey::new_unique());

        check_at_startup(
            &watch,
            &UnreadableSource {
                calls: AtomicU64::new(0),
            },
        )
        .await
        .expect("an unreadable GlobalState is not fatal");

        assert_eq!(watch.status(), AuthorityStatus::Unknown);
        assert!(
            watch.status().is_servable(),
            "an RPC problem must not be an outage"
        );
    }

    #[tokio::test(start_paused = true)]
    async fn a_rotation_after_startup_stops_the_instance_being_servable() {
        let key = Pubkey::new_unique();
        let watch = Arc::new(AuthorityWatch::new(key));
        watch.observe(key);
        assert!(watch.status().is_servable());

        let rotated = Pubkey::new_unique();
        let shutdown = CancellationToken::new();
        let handle = tokio::spawn(run_watcher(
            watch.clone(),
            Arc::new(StaticSource { authority: rotated }),
            std::time::Duration::from_secs(10),
            shutdown.clone(),
        ));

        tokio::time::sleep(std::time::Duration::from_secs(15)).await;
        shutdown.cancel();
        handle.await.expect("watcher does not panic");

        assert_eq!(
            watch.status(),
            AuthorityStatus::Mismatch { onchain: rotated }
        );
        assert!(!watch.status().is_servable());
    }
}
