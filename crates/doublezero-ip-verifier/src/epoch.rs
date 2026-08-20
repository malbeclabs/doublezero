//! The DoubleZero ledger epoch a proof is issued against.
//!
//! The program accepts `clock.epoch` or `clock.epoch - 1`, so a proof signed with an epoch the
//! service has held too long is a proof that will be rejected onchain — or, worse, one that
//! silently narrows to a single valid epoch of slack. The cache therefore has a hard age limit: past
//! it, requests are refused rather than served with a number the service cannot vouch for.

use async_trait::async_trait;
use std::{
    sync::RwLock,
    time::{Duration, Instant},
};
use tokio_util::sync::CancellationToken;
use tracing::{info, warn};

/// Where the current epoch comes from. A trait so tests can drive the cache without an RPC node;
/// [`crate::ledger::Ledger`] is the implementation the service runs with.
#[async_trait]
pub trait EpochSource: Send + Sync {
    async fn current_epoch(&self) -> anyhow::Result<u64>;
}

#[derive(thiserror::Error, Debug, PartialEq, Eq)]
pub enum EpochError {
    #[error("the ledger epoch has not been fetched yet")]
    Unavailable,
    #[error("the cached ledger epoch is {age_secs}s old, past the {max_age_secs}s limit")]
    Stale { age_secs: u64, max_age_secs: u64 },
}

#[derive(Debug, Clone, Copy)]
struct Snapshot {
    epoch: u64,
    fetched_at: Instant,
}

/// The last epoch read from the ledger, with the time it was read.
pub struct EpochCache {
    snapshot: RwLock<Option<Snapshot>>,
    max_age: Duration,
}

impl EpochCache {
    pub fn new(max_age: Duration) -> Self {
        Self {
            snapshot: RwLock::new(None),
            max_age,
        }
    }

    /// The epoch to sign with, or why there isn't one. Fails closed: an epoch older than `max_age`
    /// is not returned at all, because signing it would issue proofs the program may already
    /// consider stale.
    pub fn current(&self) -> Result<u64, EpochError> {
        self.current_at(Instant::now())
    }

    fn current_at(&self, now: Instant) -> Result<u64, EpochError> {
        let snapshot = self
            .snapshot
            .read()
            .expect("epoch cache lock is never held across a panic")
            .ok_or(EpochError::Unavailable)?;

        let age = now.saturating_duration_since(snapshot.fetched_at);
        if age > self.max_age {
            return Err(EpochError::Stale {
                age_secs: age.as_secs(),
                max_age_secs: self.max_age.as_secs(),
            });
        }

        Ok(snapshot.epoch)
    }

    fn store_at(&self, epoch: u64, now: Instant) {
        *self
            .snapshot
            .write()
            .expect("epoch cache lock is never held across a panic") = Some(Snapshot {
            epoch,
            fetched_at: now,
        });
        metrics::gauge!("doublezero_ip_verifier_ledger_epoch").set(epoch as f64);
    }

    pub fn store(&self, epoch: u64) {
        self.store_at(epoch, Instant::now());
    }
}

/// Refreshes the cache in the background until cancelled.
///
/// A failed refresh is logged and retried on the next tick; it does not clear the cache, because a
/// recent epoch stays usable across a brief RPC outage. Once the cache ages out, `current()` starts
/// refusing on its own — the staleness limit is the only thing that decides that, so a refresh loop
/// that dies quietly cannot leave the service signing forever.
pub async fn run_refresher(
    cache: std::sync::Arc<EpochCache>,
    source: std::sync::Arc<dyn EpochSource>,
    interval: Duration,
    shutdown: CancellationToken,
) {
    loop {
        match source.current_epoch().await {
            Ok(epoch) => {
                cache.store(epoch);
                metrics::counter!("doublezero_ip_verifier_epoch_refresh_total", "result" => "ok")
                    .increment(1);
            }
            Err(err) => {
                warn!(?err, "failed to refresh the ledger epoch");
                metrics::counter!("doublezero_ip_verifier_epoch_refresh_total", "result" => "error")
                    .increment(1);
            }
        }

        tokio::select! {
            biased;
            _ = shutdown.cancelled() => {
                info!("epoch refresher shutting down");
                return;
            }
            _ = tokio::time::sleep(interval) => {}
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::{
        atomic::{AtomicU64, Ordering},
        Arc,
    };

    #[test]
    fn an_unfetched_cache_has_no_epoch() {
        let cache = EpochCache::new(Duration::from_secs(60));
        assert_eq!(cache.current(), Err(EpochError::Unavailable));
    }

    #[test]
    fn a_fresh_epoch_is_returned() {
        let cache = EpochCache::new(Duration::from_secs(60));
        cache.store(931);
        assert_eq!(cache.current(), Ok(931));
    }

    #[test]
    fn an_epoch_past_the_age_limit_is_refused() {
        let cache = EpochCache::new(Duration::from_secs(60));
        let stored_at = Instant::now();
        cache.store_at(931, stored_at);

        assert_eq!(
            cache.current_at(stored_at + Duration::from_secs(60)),
            Ok(931)
        );
        assert_eq!(
            cache.current_at(stored_at + Duration::from_secs(61)),
            Err(EpochError::Stale {
                age_secs: 61,
                max_age_secs: 60
            })
        );
    }

    #[test]
    fn a_refresh_makes_a_stale_cache_usable_again() {
        let cache = EpochCache::new(Duration::from_secs(60));
        let start = Instant::now();
        cache.store_at(931, start);
        assert!(cache.current_at(start + Duration::from_secs(120)).is_err());

        cache.store_at(932, start + Duration::from_secs(120));
        assert_eq!(cache.current_at(start + Duration::from_secs(121)), Ok(932));
    }

    struct FailingSource {
        calls: AtomicU64,
    }

    #[async_trait]
    impl EpochSource for FailingSource {
        async fn current_epoch(&self) -> anyhow::Result<u64> {
            self.calls.fetch_add(1, Ordering::SeqCst);
            Err(anyhow::anyhow!("rpc down"))
        }
    }

    #[tokio::test(start_paused = true)]
    async fn a_failed_refresh_leaves_the_previous_epoch_in_place() {
        let cache = Arc::new(EpochCache::new(Duration::from_secs(3600)));
        cache.store(931);

        let source = Arc::new(FailingSource {
            calls: AtomicU64::new(0),
        });
        let shutdown = CancellationToken::new();
        let handle = tokio::spawn(run_refresher(
            cache.clone(),
            source.clone(),
            Duration::from_secs(10),
            shutdown.clone(),
        ));

        tokio::time::sleep(Duration::from_secs(25)).await;
        shutdown.cancel();
        handle.await.expect("refresher does not panic");

        assert!(source.calls.load(Ordering::SeqCst) >= 2, "refresh retries");
        assert_eq!(cache.current(), Ok(931));
    }
}
