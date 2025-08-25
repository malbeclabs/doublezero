//! Error types for the scheduler

use borsh::{BorshDeserialize, BorshSerialize};
use thiserror::Error;

pub type Result<T> = std::result::Result<T, Error>;

#[derive(Error, Debug)]
pub enum Error {
    #[error("Job execution failed: {0}")]
    ExecutionFailed(String),

    #[error("Invalid job ID: {0}")]
    InvalidJobId(String),

    #[error("Job already registered: {0}")]
    DuplicateJob(String),

    #[error("Circuit breaker open for job: {0}")]
    CircuitBreakerOpen(String),

    #[error("Job execution timeout: {0}")]
    Timeout(String),

    #[error("RPC error: {0}")]
    Rpc(#[from] Box<solana_client::client_error::ClientError>),

    #[error("Serialization error: {0}")]
    Serialization(#[from] borsh::io::Error),

    #[error("WebSocket error: {0}")]
    WebSocket(String),

    #[error("PubSub client error: {0}")]
    PubSubClient(String),

    #[error("Channel send error")]
    ChannelSend,

    #[error("Scheduler shutdown")]
    Shutdown,

    #[error(transparent)]
    Other(#[from] anyhow::Error),
}

/// Core error categories for on-chain storage (compact)
#[repr(u8)]
#[derive(Debug, Clone, Copy, BorshSerialize, BorshDeserialize)]
#[borsh(use_discriminant = true)]
pub enum ErrorCategory {
    Transient = 1,    // Retry with backoff
    RateLimited = 2,  // Retry with specific delay
    InvalidInput = 3, // Don't retry, fix required
    Duplicate = 4,    // Already done, skip
    Fatal = 5,        // Circuit break, alert
}

/// Compact error context for on-chain storage
#[derive(Debug, Clone, BorshSerialize, BorshDeserialize)]
pub struct ErrorContext {
    pub category: ErrorCategory,
    pub job_error_code: Option<u16>,
    pub context_hash: Option<[u8; 16]>,
}

impl ErrorContext {
    pub fn new(category: ErrorCategory) -> Self {
        Self {
            category,
            job_error_code: None,
            context_hash: None,
        }
    }

    pub fn with_code(mut self, code: u16) -> Self {
        self.job_error_code = Some(code);
        self
    }
}
