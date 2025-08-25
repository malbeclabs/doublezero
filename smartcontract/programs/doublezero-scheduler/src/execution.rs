//! Job execution engine with idempotency and retry logic

use crate::{
    safety::{CircuitBreaker, Watchdog},
    state::ExecutionRecord,
    Error, ErrorCategory, ErrorContext, ExecutionContext, Result, ScheduledJob,
};
use backon::{ExponentialBuilder, Retryable};
use std::{sync::Arc, time::Duration};
use tracing::{error, info, warn};
use uuid::Uuid;

/// Default constants
const MAX_RETRIES: u32 = 3;
const RETRY_BASE_MS: u64 = 1000;

/// Executes jobs with safety mechanisms
pub struct ExecutionEngine {
    watchdog: Arc<Watchdog>,
    circuit_breaker: Arc<CircuitBreaker>,
}

impl ExecutionEngine {
    pub fn new(watchdog: Arc<Watchdog>, circuit_breaker: Arc<CircuitBreaker>) -> Self {
        Self {
            watchdog,
            circuit_breaker,
        }
    }

    /// Execute a job with all safety checks
    pub async fn execute_job(
        &self,
        job: &dyn ScheduledJob,
        context: ExecutionContext,
    ) -> Result<Vec<u8>> {
        let job_id = job.job_id();

        // Check circuit breaker
        if !self.circuit_breaker.can_execute(job_id) {
            warn!("Circuit breaker open for job '{}'", job_id);
            return Err(Error::CircuitBreakerOpen(job_id.to_string()));
        }

        // Check idempotency
        if !self.check_idempotency(job, &context).await? {
            info!("Job '{}' already executed for this schedule", job_id);
            return Ok(Vec::new());
        }

        // Create execution record
        let execution_id = context.execution_id;
        let mut record = ExecutionRecord::new(execution_id, job_id.to_string());
        record.data_seeds = job.seeds(&context);

        // Write initial execution record
        self.write_execution_record(&record).await?;

        // Setup watchdog
        let (abort_handle, abort_registration) = futures::future::AbortHandle::new_pair();
        let timeout = job.config().timeout.unwrap_or(Duration::from_secs(60));
        self.watchdog
            .register(job_id.to_string(), Some(timeout), abort_handle);

        // Execute with retries
        let execution_future =
            futures::future::Abortable::new(self.execute_with_retry(job), abort_registration);

        let result = tokio::time::timeout(timeout, execution_future).await;

        // Unregister from watchdog
        self.watchdog.unregister(job_id);

        // Process result
        match result {
            Ok(Ok(Ok(data))) => {
                // Success
                info!("Job '{}' executed successfully", job_id);
                self.circuit_breaker.record_success(job_id);

                // Update execution record
                let data_hash = calculate_hash(&data);
                record = record.complete_success(data_hash);
                self.write_execution_record(&record).await?;

                // Update job state
                self.update_job_state(job_id, &execution_id, data_hash)
                    .await?;

                Ok(data)
            }
            Ok(Ok(Err(e))) => {
                // Execution failed
                error!("Job '{}' execution failed: {}", job_id, e);
                self.circuit_breaker.record_failure(job_id);

                let error_ctx = ErrorContext::new(ErrorCategory::Fatal);
                record = record.complete_failure(error_ctx.clone());
                self.write_execution_record(&record).await?;

                Err(e)
            }
            Ok(Err(_)) => {
                // Aborted by watchdog
                error!("Job '{}' was aborted", job_id);
                self.circuit_breaker.record_failure(job_id);

                let error_ctx = ErrorContext::new(ErrorCategory::Fatal).with_code(1000);
                record = record.complete_failure(error_ctx);
                self.write_execution_record(&record).await?;

                Err(Error::Timeout(job_id.to_string()))
            }
            Err(_) => {
                // Timeout
                error!("Job '{}' timed out", job_id);
                self.circuit_breaker.record_failure(job_id);

                let error_ctx = ErrorContext::new(ErrorCategory::Fatal).with_code(1001);
                record = record.complete_failure(error_ctx);
                self.write_execution_record(&record).await?;

                Err(Error::Timeout(job_id.to_string()))
            }
        }
    }

    async fn execute_with_retry(&self, job: &dyn ScheduledJob) -> Result<Vec<u8>> {
        let job_id = job.job_id();

        (|| async { job.execute().await })
            .retry(&ExponentialBuilder::default().with_jitter())
            .notify(|err: &Error, dur: Duration| {
                warn!(
                    "Job '{}' execution failed, retrying in {:?}: {:?}",
                    job_id, dur, err
                );
            })
            .await
    }

    async fn check_idempotency(
        &self,
        job: &dyn ScheduledJob,
        context: &ExecutionContext,
    ) -> Result<bool> {
        // TODO: Check job state from doublezero-record
        // Will use seeds from job.seeds(context) to check if this execution already happened
        let _seeds = job.seeds(context);
        let _job_id = job.job_id();

        // For now, return true (allow execution)
        Ok(true)
    }

    async fn write_execution_record(&self, record: &ExecutionRecord) -> Result<()> {
        // TODO: Write to doublezero-record
        info!("Would write execution record for job '{}'", record.job_id);
        Ok(())
    }

    async fn update_job_state(
        &self,
        job_id: &str,
        execution_id: &Uuid,
        data_hash: [u8; 32],
    ) -> Result<()> {
        // TODO: Update job state in doublezero-record
        // This will write to the ledger with seeds: ["scheduler", "job_state", job_id_bytes]
        info!(
            "Would update job state for '{}' with execution_id: {} and data_hash: {:?}",
            job_id, execution_id, data_hash
        );
        Ok(())
    }
}

fn calculate_hash(data: &[u8]) -> [u8; 32] {
    let hash = solana_sdk::hash::hash(data);
    hash.to_bytes()
}
