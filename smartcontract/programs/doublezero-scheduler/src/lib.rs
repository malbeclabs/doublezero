//! DoubleZero Scheduler - Generic scheduling library for on-chain writes
//!
//! A standalone, generic scheduling library that can schedule any Borsh-serializable
//! data to be written to doublezero-record at specified intervals or epoch changes.

pub mod epoch_monitor;
pub mod error;
pub mod execution;
pub mod job;
pub mod safety;
pub mod schedule;
pub mod scheduler;
pub mod state;

pub use error::{Error, ErrorCategory, ErrorContext, Result};
pub use job::{ExecutionContext, JobConfig, ScheduledJob};
pub use schedule::Schedule;
pub use scheduler::Scheduler;
pub use state::{ExecutionRecord, JobState};

/// Library version for on-chain audit trail
pub const SCHEDULER_VERSION: u8 = 1;
