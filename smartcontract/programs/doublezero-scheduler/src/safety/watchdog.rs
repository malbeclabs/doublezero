//! Watchdog for monitoring and killing stuck jobs

use crate::{error::ErrorContext, ErrorCategory};
use dashmap::DashMap;
use std::sync::Arc;
use std::time::{Duration, Instant};
use tokio::task::AbortHandle;
use tokio_util::sync::CancellationToken;
use tracing::{error, info, warn};

/// Default constants
const WATCHDOG_CHECK_INTERVAL: Duration = Duration::from_secs(5);
const DEFAULT_JOB_TIMEOUT: Duration = Duration::from_secs(60);

/// Monitors running jobs and kills stuck ones
pub struct Watchdog {
    jobs: Arc<DashMap<String, RunningJob>>,
    check_interval: Duration,
}

struct RunningJob {
    job_id: String,
    started_at: Instant,
    timeout: Duration,
    abort_handle: AbortHandle,
}

impl Watchdog {
    pub fn new() -> Self {
        Self {
            jobs: Arc::new(DashMap::new()),
            check_interval: WATCHDOG_CHECK_INTERVAL,
        }
    }

    /// Register a job for monitoring
    pub fn register(&self, job_id: String, timeout: Option<Duration>, abort_handle: AbortHandle) {
        let timeout = timeout.unwrap_or(DEFAULT_JOB_TIMEOUT);

        let job = RunningJob {
            job_id: job_id.clone(),
            started_at: Instant::now(),
            timeout,
            abort_handle,
        };

        self.jobs.insert(job_id, job);
    }

    /// Unregister a job (called on successful completion)
    pub fn unregister(&self, job_id: &str) {
        self.jobs.remove(job_id);
    }

    /// Monitor loop that checks for stuck jobs
    pub async fn monitor_loop(&self, shutdown: CancellationToken) {
        info!("Watchdog started");

        loop {
            tokio::select! {
                _ = tokio::time::sleep(self.check_interval) => {
                    self.check_jobs().await;
                }
                _ = shutdown.cancelled() => {
                    info!("Watchdog shutting down");
                    break;
                }
            }
        }
    }

    async fn check_jobs(&self) {
        let now = Instant::now();
        let mut killed = Vec::new();

        for entry in self.jobs.iter() {
            let (id, job) = entry.pair();
            let elapsed = now.duration_since(job.started_at);

            if elapsed > job.timeout {
                warn!(
                    "Killing stuck job '{}' (timeout: {:?}, elapsed: {:?})",
                    id, job.timeout, elapsed
                );

                job.abort_handle.abort();
                killed.push(id.clone());

                metrics::counter!("scheduler_watchdog_kills").increment(1);
            }
        }

        // Remove killed jobs
        for id in killed {
            self.jobs.remove(&id);
        }
    }

    /// Get count of currently monitored jobs
    pub fn active_jobs(&self) -> usize {
        self.jobs.len()
    }
}

impl Default for Watchdog {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tokio::time;

    #[tokio::test]
    async fn test_watchdog_timeout() {
        let watchdog = Watchdog::new();

        // Create a task that will be killed
        let (handle, _) = tokio::task::spawn(async {
            tokio::time::sleep(Duration::from_secs(10)).await;
        })
        .abort_handle();

        // Register with 100ms timeout
        watchdog.register(
            "test_job".to_string(),
            Some(Duration::from_millis(100)),
            handle,
        );

        assert_eq!(watchdog.active_jobs(), 1);

        // Wait for watchdog to kill it
        time::sleep(Duration::from_millis(200)).await;
        watchdog.check_jobs().await;

        assert_eq!(watchdog.active_jobs(), 0);
    }
}

