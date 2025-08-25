//! Circuit breaker pattern to prevent thrashing on failing jobs

use dashmap::DashMap;
use std::sync::Arc;
use std::time::{Duration, Instant};
use tracing::{error, info, warn};

/// Default constants
const MAX_CONSECUTIVE_FAILURES: u8 = 5;
const COOLDOWN_MINUTES: u16 = 10;

/// Circuit breaker for preventing repeated failures
pub struct CircuitBreaker {
    states: Arc<DashMap<String, BreakerState>>,
    max_failures: u8,
    cooldown: Duration,
}

#[derive(Debug, Clone)]
struct BreakerState {
    consecutive_failures: u8,
    last_failure: Option<Instant>,
    status: BreakerStatus,
}

#[derive(Debug, Clone, Copy, PartialEq)]
enum BreakerStatus {
    /// Normal operation
    Closed,
    /// Circuit broken, cooling down until instant
    Open(Instant),
    /// Testing recovery with one attempt
    HalfOpen,
}

impl Default for BreakerState {
    fn default() -> Self {
        Self {
            consecutive_failures: 0,
            last_failure: None,
            status: BreakerStatus::Closed,
        }
    }
}

impl CircuitBreaker {
    pub fn new() -> Self {
        Self {
            states: Arc::new(DashMap::new()),
            max_failures: MAX_CONSECUTIVE_FAILURES,
            cooldown: Duration::from_secs(COOLDOWN_MINUTES as u64 * 60),
        }
    }

    /// Check if a job can execute
    pub fn can_execute(&self, job_id: &str) -> bool {
        let mut state = self.states.entry(job_id.to_string()).or_default();

        match state.status {
            BreakerStatus::Closed => true,
            BreakerStatus::Open(until) => {
                if Instant::now() >= until {
                    // Try recovery
                    info!(
                        "Circuit breaker entering half-open state for job '{}'",
                        job_id
                    );
                    state.status = BreakerStatus::HalfOpen;
                    true
                } else {
                    false
                }
            }
            BreakerStatus::HalfOpen => true,
        }
    }

    /// Record a successful execution
    pub fn record_success(&self, job_id: &str) {
        if let Some(mut state) = self.states.get_mut(job_id) {
            if state.status == BreakerStatus::HalfOpen {
                info!(
                    "Circuit breaker closed for job '{}' after successful recovery",
                    job_id
                );
            }
            state.consecutive_failures = 0;
            state.status = BreakerStatus::Closed;
        }
    }

    /// Record a failed execution
    pub fn record_failure(&self, job_id: &str) {
        let mut state = self.states.entry(job_id.to_string()).or_default();

        state.consecutive_failures += 1;
        state.last_failure = Some(Instant::now());

        // Check if we should open the circuit
        if state.status == BreakerStatus::HalfOpen {
            // Failed during recovery attempt
            let cooldown_until = Instant::now() + self.cooldown;
            state.status = BreakerStatus::Open(cooldown_until);
            error!(
                "Circuit breaker OPEN for job '{}' after failed recovery attempt. Cooldown: {} minutes",
                job_id, COOLDOWN_MINUTES
            );
        } else if state.consecutive_failures >= self.max_failures {
            let cooldown_until = Instant::now() + self.cooldown;
            state.status = BreakerStatus::Open(cooldown_until);
            error!(
                "Circuit breaker OPEN for job '{}': {} consecutive failures. Cooldown: {} minutes",
                job_id, state.consecutive_failures, COOLDOWN_MINUTES
            );
        } else {
            warn!(
                "Job '{}' failed ({}/{} failures before circuit break)",
                job_id, state.consecutive_failures, self.max_failures
            );
        }

        metrics::counter!("scheduler_circuit_breaker_failures").increment(1);
    }

    /// Get the current status of a job's circuit breaker
    pub fn status(&self, job_id: &str) -> BreakerStatus {
        self.states
            .get(job_id)
            .map(|state| state.status)
            .unwrap_or(BreakerStatus::Closed)
    }

    /// Reset a job's circuit breaker (for manual intervention)
    pub fn reset(&self, job_id: &str) {
        self.states.remove(job_id);
        info!("Circuit breaker manually reset for job '{}'", job_id);
    }
}

impl Default for CircuitBreaker {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_circuit_breaker_opens_after_failures() {
        let breaker = CircuitBreaker::new();
        let job_id = "test_job";

        // Record failures up to threshold
        for i in 0..MAX_CONSECUTIVE_FAILURES {
            assert!(breaker.can_execute(job_id));
            breaker.record_failure(job_id);

            if i < MAX_CONSECUTIVE_FAILURES - 1 {
                // Should still be closed
                assert_eq!(breaker.status(job_id), BreakerStatus::Closed);
            }
        }

        // Should now be open
        assert!(!breaker.can_execute(job_id));
        match breaker.status(job_id) {
            BreakerStatus::Open(_) => {}
            _ => panic!("Expected circuit to be open"),
        }
    }

    #[test]
    fn test_circuit_breaker_recovery() {
        let breaker = CircuitBreaker::new();
        let job_id = "test_job";

        // Open the circuit
        for _ in 0..MAX_CONSECUTIVE_FAILURES {
            breaker.record_failure(job_id);
        }

        // Should be open
        assert!(!breaker.can_execute(job_id));

        // Manually set to half-open (simulating cooldown expiry)
        if let Some(mut state) = breaker.states.get_mut(job_id) {
            state.status = BreakerStatus::HalfOpen;
        }

        // Should allow one attempt
        assert!(breaker.can_execute(job_id));

        // Success should close it
        breaker.record_success(job_id);
        assert_eq!(breaker.status(job_id), BreakerStatus::Closed);
    }
}

