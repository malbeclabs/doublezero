//! State management for scheduler on doublezero-record

use crate::error::ErrorContext;
use borsh::{BorshDeserialize, BorshSerialize};
use uuid::Uuid;

/// Job execution state stored on-chain
/// Seeds: ["scheduler", "job_state", job_id_bytes]
#[derive(Debug, Clone, BorshSerialize, BorshDeserialize)]
pub struct JobState {
    pub job_id: String,
    pub last_execution_time: i64,
    pub last_execution_slot: u64,
    pub last_execution_id: [u8; 16],
    pub last_data_hash: [u8; 32],
    pub total_executions: u64,
    pub total_failures: u64,
    pub last_error: Option<ErrorContext>,
}

impl JobState {
    pub fn new(job_id: String) -> Self {
        Self {
            job_id,
            last_execution_time: 0,
            last_execution_slot: 0,
            last_execution_id: [0; 16],
            last_data_hash: [0; 32],
            total_executions: 0,
            total_failures: 0,
            last_error: None,
        }
    }

    pub fn seeds(&self) -> Vec<Vec<u8>> {
        vec![
            b"scheduler".to_vec(),
            b"job_state".to_vec(),
            self.job_id.as_bytes().to_vec(),
        ]
    }
}

/// Execution status
#[derive(Debug, Clone, Copy, BorshSerialize, BorshDeserialize)]
pub enum ExecutionStatus {
    InProgress,
    Success,
    Failed,
    Timeout,
    Skipped,
}

/// Individual execution record stored on-chain
/// Seeds: ["scheduler", "execution", execution_id_bytes]
#[derive(Debug, Clone, BorshSerialize, BorshDeserialize)]
pub struct ExecutionRecord {
    pub execution_id: [u8; 16],
    pub job_id: String,
    pub started_at: i64,
    pub completed_at: Option<i64>,
    pub status: ExecutionStatus,
    pub data_seeds: Vec<Vec<u8>>,
    pub data_hash: [u8; 32],
    pub error: Option<ErrorContext>,
}

impl ExecutionRecord {
    pub fn new(execution_id: Uuid, job_id: String) -> Self {
        Self {
            execution_id: *execution_id.as_bytes(),
            job_id,
            started_at: chrono::Utc::now().timestamp(),
            completed_at: None,
            status: ExecutionStatus::InProgress,
            data_seeds: Vec::new(),
            data_hash: [0; 32],
            error: None,
        }
    }

    pub fn seeds(&self) -> Vec<Vec<u8>> {
        vec![
            b"scheduler".to_vec(),
            b"execution".to_vec(),
            self.execution_id.to_vec(),
        ]
    }

    pub fn complete_success(mut self, data_hash: [u8; 32]) -> Self {
        self.completed_at = Some(chrono::Utc::now().timestamp());
        self.status = ExecutionStatus::Success;
        self.data_hash = data_hash;
        self
    }

    pub fn complete_failure(mut self, error: ErrorContext) -> Self {
        self.completed_at = Some(chrono::Utc::now().timestamp());
        self.status = ExecutionStatus::Failed;
        self.error = Some(error);
        self
    }
}

/// Metadata wrapper for scheduled writes
#[derive(Debug, Clone, BorshSerialize, BorshDeserialize)]
pub struct ScheduledWrite<T: BorshSerialize> {
    /// Job identifier
    pub job_id: String,

    /// Scheduler version
    pub scheduler_version: u8,

    /// What triggered this write
    pub trigger_type: crate::job::TriggerType,

    /// When it was scheduled
    pub scheduled_at: i64,

    /// Unique execution ID
    pub execution_id: [u8; 16],

    /// The actual payload
    pub data: T,
}

impl<T: BorshSerialize> ScheduledWrite<T> {
    pub fn new(
        job_id: String,
        trigger_type: crate::job::TriggerType,
        execution_id: Uuid,
        data: T,
    ) -> Self {
        Self {
            job_id,
            scheduler_version: crate::SCHEDULER_VERSION,
            trigger_type,
            scheduled_at: chrono::Utc::now().timestamp(),
            execution_id: *execution_id.as_bytes(),
            data,
        }
    }
}
