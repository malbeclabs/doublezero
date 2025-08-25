//! Core job trait and configuration

use crate::Result;
use async_trait::async_trait;
use std::time::Duration;
use uuid::Uuid;

/// Execution context passed to jobs
#[derive(Debug, Clone)]
pub struct ExecutionContext {
    /// Current epoch (if triggered by epoch change)
    pub epoch: Option<u64>,

    /// Current slot
    pub slot: Option<u64>,

    /// Scheduled execution timestamp
    pub scheduled_for: i64,

    /// Unique execution ID
    pub execution_id: Uuid,

    /// Trigger type that caused this execution
    pub trigger: TriggerType,
}

#[derive(Debug, Clone, Copy, borsh::BorshSerialize, borsh::BorshDeserialize)]
pub enum TriggerType {
    Interval,
    EpochChange,
    Manual,
}

/// Configuration for a scheduled job
#[derive(Debug, Clone)]
pub struct JobConfig {
    /// Optional timeout override (defaults to 60s)
    pub timeout: Option<Duration>,
}

impl Default for JobConfig {
    fn default() -> Self {
        Self { timeout: None }
    }
}

/// Core trait that all scheduled jobs must implement
#[async_trait]
pub trait ScheduledJob: Send + Sync {
    /// Unique identifier for this job type (max 32 bytes)
    fn job_id(&self) -> &str;

    /// Job-specific configuration
    fn config(&self) -> JobConfig {
        JobConfig::default()
    }

    /// Fetch and return the data to write (as Borsh bytes)
    async fn execute(&self) -> Result<Vec<u8>>;

    /// Seeds for where to write the data in doublezero-record
    fn seeds(&self, context: &ExecutionContext) -> Vec<Vec<u8>>;

    /// Optional validation before write
    fn validate(&self, _data: &[u8]) -> Result<()> {
        Ok(())
    }
}

/// Validates job ID format and length
pub fn validate_job_id(id: &str) -> Result<()> {
    const MAX_JOB_ID_LENGTH: usize = 32;

    if id.is_empty() {
        return Err(crate::Error::InvalidJobId("Job ID cannot be empty".into()));
    }

    if id.len() > MAX_JOB_ID_LENGTH {
        return Err(crate::Error::InvalidJobId(format!(
            "Job ID '{}' exceeds maximum length of {} bytes",
            id, MAX_JOB_ID_LENGTH
        )));
    }

    // Only allow alphanumeric, underscore, and hyphen
    if !id
        .chars()
        .all(|c| c.is_ascii_alphanumeric() || c == '_' || c == '-')
    {
        return Err(crate::Error::InvalidJobId(
            format!("Job ID '{}' contains invalid characters. Use only alphanumeric, underscore, and hyphen", id)
        ));
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_job_id_validation() {
        assert!(validate_job_id("valid_job_123").is_ok());
        assert!(validate_job_id("valid-job-123").is_ok());
        assert!(validate_job_id("").is_err());
        assert!(validate_job_id(&"a".repeat(33)).is_err());
        assert!(validate_job_id("invalid job!").is_err());
        assert!(validate_job_id("invalid@job").is_err());
    }
}
