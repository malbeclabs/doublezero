//! Safety mechanisms: watchdog timer and circuit breaker
use dashmap::DashMap;
use futures::future::AbortHandle;
use std::{
    sync::{
        atomic::{AtomicUsize, Ordering},
        Arc,
    },
    time::{Duration, Instant},
};
use tokio::sync::RwLock;
use tracing::{error, info, warn};

/// Watchdog timer for detecting stuck jobs
pub struct Watchdog {
    active_jobs: Arc<DashMap<String, WatchdogEntry>>,
    check_interval: Duration,
}

struct WatchdogEntry {
    job_id: String,
    started_at: Instant,
    timeout: Duration,
    abort_handle: AbortHandle,
}

impl Watchdog {
    pub fn new(check_interval: Duration) -> Self {
        Self {
            active_jobs: Arc::new(DashMap::new()),
            check_interval,
        }
    }

    pub fn register(&self, job_id: String, timeout: Option<Duration>, abort_handle: AbortHandle) {
        let entry = WatchdogEntry {
            job_id: job_id.clone(),
            started_at: Instant::now(),
            timeout: timeout.unwrap_or(Duration::from_secs(60)),
            abort_handle,
        };
        self.active_jobs.insert(job_id, entry);
    }

    pub fn unregister(&self, job_id: &str) {
        self.active_jobs.remove(job_id);
    }

    pub async fn run(&self, shutdown: tokio_util::sync::CancellationToken) {
        let mut interval = tokio::time::interval(self.check_interval);

        loop {
            if shutdown.is_cancelled() {
                info!("Watchdog shutting down");
                break;
            }

            interval.tick().await;
            self.check_timeouts().await;
        }
    }

    async fn check_timeouts(&self) {
        let now = Instant::now();
        let mut timed_out = Vec::new();

        for entry in self.active_jobs.iter() {
            let elapsed = now.duration_since(entry.started_at);
            if elapsed > entry.timeout {
                warn!("Job '{}' timed out after {:?}", entry.job_id, elapsed);
                timed_out.push(entry.key().clone());
                entry.abort_handle.abort();
                metrics::counter!("scheduler_watchdog_timeouts").increment(1);
            }
        }

        // Remove timed out jobs
        for job_id in timed_out {
            self.active_jobs.remove(&job_id);
        }
    }
}

/// Circuit breaker for failing jobs
pub struct CircuitBreaker {
    states: Arc<DashMap<String, CircuitState>>,
    config: CircuitBreakerConfig,
}

struct CircuitState {
    consecutive_failures: AtomicUsize,
    last_failure_time: RwLock<Option<Instant>>,
    state: RwLock<BreakerState>,
}

#[derive(Debug, Clone, Copy)]
enum BreakerState {
    Closed,
    Open,
    HalfOpen,
}

pub struct CircuitBreakerConfig {
    pub max_consecutive_failures: usize,
    pub cooldown_duration: Duration,
    pub half_open_test_interval: Duration,
}

impl Default for CircuitBreakerConfig {
    fn default() -> Self {
        Self {
            max_consecutive_failures: 5,
            cooldown_duration: Duration::from_secs(600), // 10 minutes
            half_open_test_interval: Duration::from_secs(30),
        }
    }
}

impl CircuitBreaker {
    pub fn new(config: CircuitBreakerConfig) -> Self {
        Self {
            states: Arc::new(DashMap::new()),
            config,
        }
    }

    pub fn can_execute(&self, job_id: &str) -> bool {
        let state = self
            .states
            .entry(job_id.to_string())
            .or_insert_with(|| CircuitState {
                consecutive_failures: AtomicUsize::new(0),
                last_failure_time: RwLock::new(None),
                state: RwLock::new(BreakerState::Closed),
            });

        // Check current state
        let current_state = *state.state.blocking_read();
        match current_state {
            BreakerState::Closed => true,
            BreakerState::Open => {
                // Check if cooldown period has passed
                if let Some(last_failure) = *state.last_failure_time.blocking_read() {
                    if last_failure.elapsed() > self.config.cooldown_duration {
                        // Transition to half-open
                        *state.state.blocking_write() = BreakerState::HalfOpen;
                        info!(
                            "Circuit breaker for '{}' transitioning to half-open",
                            job_id
                        );
                        true
                    } else {
                        false
                    }
                } else {
                    false
                }
            }
            BreakerState::HalfOpen => true, // Allow one test execution
        }
    }

    pub fn record_success(&self, job_id: &str) {
        if let Some(state) = self.states.get(job_id) {
            state.consecutive_failures.store(0, Ordering::Relaxed);

            let current_state = *state.state.blocking_read();
            if matches!(current_state, BreakerState::HalfOpen) {
                *state.state.blocking_write() = BreakerState::Closed;
                info!(
                    "Circuit breaker for '{}' closed after successful test",
                    job_id
                );
            }

            metrics::counter!("scheduler_circuit_breaker_successes").increment(1);
        }
    }

    pub fn record_failure(&self, job_id: &str) {
        let state = self
            .states
            .entry(job_id.to_string())
            .or_insert_with(|| CircuitState {
                consecutive_failures: AtomicUsize::new(0),
                last_failure_time: RwLock::new(None),
                state: RwLock::new(BreakerState::Closed),
            });

        let failures = state.consecutive_failures.fetch_add(1, Ordering::Relaxed) + 1;
        *state.last_failure_time.blocking_write() = Some(Instant::now());

        if failures >= self.config.max_consecutive_failures {
            *state.state.blocking_write() = BreakerState::Open;
            error!(
                "Circuit breaker for '{}' opened after {} consecutive failures",
                job_id, failures
            );
            metrics::counter!("scheduler_circuit_breaker_opens").increment(1);
        }

        metrics::counter!("scheduler_circuit_breaker_failures").increment(1);
    }

    pub async fn run_monitoring(&self, shutdown: tokio_util::sync::CancellationToken) {
        let mut interval = tokio::time::interval(Duration::from_secs(30));

        loop {
            if shutdown.is_cancelled() {
                info!("Circuit breaker monitoring shutting down");
                break;
            }

            interval.tick().await;
            self.check_states().await;
        }
    }

    async fn check_states(&self) {
        for entry in self.states.iter() {
            let job_id = entry.key();
            let state = entry.value();

            let current_state = *state.state.read().await;
            let failures = state.consecutive_failures.load(Ordering::Relaxed);

            metrics::gauge!("scheduler_circuit_breaker_state", "job_id" => job_id.clone()).set(
                match current_state {
                    BreakerState::Closed => 0.0,
                    BreakerState::HalfOpen => 0.5,
                    BreakerState::Open => 1.0,
                },
            );

            metrics::gauge!("scheduler_circuit_breaker_failures_count", "job_id" => job_id.clone())
                .set(failures as f64);
        }
    }
}
