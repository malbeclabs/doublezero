//! Main scheduler implementation

use crate::{
    epoch_monitor::{EpochChange, EpochMonitor},
    execution::ExecutionEngine,
    job::{validate_job_id, TriggerType},
    safety::{CircuitBreaker, CircuitBreakerConfig, Watchdog},
    Error, ExecutionContext, Result, Schedule, ScheduledJob,
};
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_sdk::signature::Keypair;
use std::{sync::Arc, time::Duration};
use tokio::sync::mpsc;
use tokio_util::sync::CancellationToken;
use tracing::{error, info};

/// Main scheduler that coordinates job execution
pub struct Scheduler {
    jobs: Vec<(Arc<dyn ScheduledJob>, Schedule)>,
    rpc_client: Arc<RpcClient>,
    payer: Arc<Keypair>,
    watchdog: Arc<Watchdog>,
    circuit_breaker: Arc<CircuitBreaker>,
    execution_engine: Arc<ExecutionEngine>,
    ws_url: Option<String>,
    shutdown: CancellationToken,
}

impl Scheduler {
    /// Create a new scheduler builder
    pub fn builder() -> SchedulerBuilder {
        SchedulerBuilder::default()
    }

    /// Register a job with a schedule
    pub fn register(&mut self, job: impl ScheduledJob + 'static, schedule: Schedule) -> Result<()> {
        let job_id = job.job_id();

        // Validate job ID
        validate_job_id(job_id)?;

        // Check for duplicates
        if self.jobs.iter().any(|(j, _)| j.job_id() == job_id) {
            return Err(Error::DuplicateJob(job_id.to_string()));
        }

        info!("Registered job '{}' with schedule {:?}", job_id, schedule);
        self.jobs.push((Arc::new(job), schedule));
        Ok(())
    }

    /// Run the scheduler
    pub async fn run(self) -> Result<()> {
        info!("Starting scheduler with {} jobs", self.jobs.len());

        let mut tasks = Vec::new();

        // Start watchdog
        let watchdog = self.watchdog.clone();
        let shutdown = self.shutdown.clone();
        tasks.push(tokio::spawn(async move {
            watchdog.run(shutdown).await;
        }));

        // Separate jobs by schedule type
        let mut interval_jobs = Vec::new();
        let mut epoch_jobs = Vec::new();

        for (job, schedule) in self.jobs {
            match schedule {
                Schedule::Interval { .. } => interval_jobs.push((job, schedule)),
                Schedule::EpochChange { .. } => epoch_jobs.push((job, schedule)),
            }
        }

        // Start interval schedulers
        for (job, schedule) in interval_jobs {
            let engine = self.execution_engine.clone();
            let shutdown = self.shutdown.clone();

            if let Schedule::Interval { seconds } = schedule {
                tasks.push(tokio::spawn(async move {
                    run_interval_job(job, Duration::from_secs(seconds), engine, shutdown).await;
                }));
            }
        }

        // Start epoch monitor if we have epoch jobs
        if !epoch_jobs.is_empty() {
            let rpc_url = self.rpc_client.url();
            let (monitor, epoch_rx) = EpochMonitor::new(rpc_url, self.ws_url.clone());

            let shutdown = self.shutdown.clone();
            tasks.push(tokio::spawn(async move {
                if let Err(e) = monitor.run(shutdown).await {
                    error!("Epoch monitor error: {}", e);
                }
            }));

            // Spawn epoch job handler
            let engine = self.execution_engine.clone();
            let shutdown = self.shutdown.clone();
            tasks.push(tokio::spawn(async move {
                run_epoch_jobs(epoch_jobs, epoch_rx, engine, shutdown).await;
            }));
        }

        // Wait for shutdown or task failure
        tokio::select! {
            _ = self.shutdown.cancelled() => {
                info!("Scheduler shutting down");
            }
            result = futures::future::select_all(tasks) => {
                if let Err(e) = result.0 {
                    error!("Task failed: {}", e);
                }
            }
        }

        Ok(())
    }

    /// Trigger shutdown
    pub fn shutdown(&self) {
        self.shutdown.cancel();
    }
}

/// Run an interval-based job
async fn run_interval_job(
    job: Arc<dyn ScheduledJob>,
    interval: Duration,
    engine: Arc<ExecutionEngine>,
    shutdown: CancellationToken,
) {
    let mut interval_timer = tokio::time::interval(interval);
    interval_timer.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);

    loop {
        tokio::select! {
            _ = interval_timer.tick() => {
                let context = ExecutionContext {
                    epoch: None,
                    slot: None,
                    scheduled_for: chrono::Utc::now().timestamp(),
                    execution_id: uuid::Uuid::new_v4(),
                    trigger: TriggerType::Interval,
                };

                info!("Executing interval job '{}'", job.job_id());

                if let Err(e) = engine.execute_job(job.as_ref(), context).await {
                    error!("Failed to execute job '{}': {}", job.job_id(), e);
                }
            }
            _ = shutdown.cancelled() => {
                info!("Stopping interval job '{}'", job.job_id());
                break;
            }
        }
    }
}

/// Run epoch-based jobs
async fn run_epoch_jobs(
    jobs: Vec<(Arc<dyn ScheduledJob>, Schedule)>,
    mut epoch_rx: mpsc::UnboundedReceiver<EpochChange>,
    engine: Arc<ExecutionEngine>,
    shutdown: CancellationToken,
) {
    loop {
        tokio::select! {
            Some(epoch_change) = epoch_rx.recv() => {
                info!("Processing epoch change to epoch {}", epoch_change.new_epoch);

                for (job, schedule) in &jobs {
                    if let Schedule::EpochChange { grace_seconds } = schedule {
                        let job = job.clone();
                        let engine = engine.clone();
                        let epoch_change = epoch_change.clone();
                        let grace_seconds = *grace_seconds;

                        // Spawn task for each job with grace period
                        tokio::spawn(async move {
                            // Apply grace period
                            if grace_seconds > 0 {
                                info!("Waiting {} seconds grace period for job '{}'",
                                     grace_seconds, job.job_id());
                                tokio::time::sleep(Duration::from_secs(grace_seconds)).await;
                            }

                            let context = ExecutionContext {
                                epoch: Some(epoch_change.new_epoch),
                                slot: Some(epoch_change.slot),
                                scheduled_for: epoch_change.timestamp,
                                execution_id: uuid::Uuid::new_v4(),
                                trigger: TriggerType::EpochChange,
                            };

                            info!("Executing epoch job '{}'", job.job_id());

                            if let Err(e) = engine.execute_job(job.as_ref(), context).await {
                                error!("Failed to execute job '{}': {}", job.job_id(), e);
                            }
                        });
                    }
                }
            }
            _ = shutdown.cancelled() => {
                info!("Stopping epoch jobs");
                break;
            }
        }
    }
}

/// Builder for creating a scheduler
pub struct SchedulerBuilder {
    rpc_url: Option<String>,
    ws_url: Option<String>,
    payer: Option<Keypair>,
}

impl Default for SchedulerBuilder {
    fn default() -> Self {
        Self {
            rpc_url: None,
            ws_url: None,
            payer: None,
        }
    }
}

impl SchedulerBuilder {
    pub fn rpc_url(mut self, url: impl Into<String>) -> Self {
        self.rpc_url = Some(url.into());
        self
    }

    pub fn ws_url(mut self, url: impl Into<String>) -> Self {
        self.ws_url = Some(url.into());
        self
    }

    pub fn payer(mut self, keypair: Keypair) -> Self {
        self.payer = Some(keypair);
        self
    }

    pub fn build(self) -> Result<Scheduler> {
        let rpc_url = self
            .rpc_url
            .ok_or_else(|| Error::Other(anyhow::anyhow!("RPC URL is required")))?;

        let payer = self
            .payer
            .ok_or_else(|| Error::Other(anyhow::anyhow!("Payer keypair is required")))?;

        let rpc_client = Arc::new(RpcClient::new(rpc_url));
        let payer = Arc::new(payer);

        let watchdog = Arc::new(Watchdog::new(Duration::from_secs(5)));
        let circuit_breaker = Arc::new(CircuitBreaker::new(CircuitBreakerConfig::default()));
        let execution_engine = Arc::new(ExecutionEngine::new(
            watchdog.clone(),
            circuit_breaker.clone(),
        ));

        Ok(Scheduler {
            jobs: Vec::new(),
            rpc_client,
            payer,
            watchdog,
            circuit_breaker,
            execution_engine,
            ws_url: self.ws_url,
            shutdown: CancellationToken::new(),
        })
    }
}
