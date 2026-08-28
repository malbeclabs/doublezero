//! A library for making CLI commands schedulable with cron-like intervals.
//!
//! This library provides a simple trait that allows any command to be run once
//! or on a scheduled interval based on a schedule string.
//!
//! # Example
//!
//! ```
//! use anyhow::Result;
//! use clap::Parser;
//! use doublezero_scheduled_command::{Schedulable, ScheduleOption};
//!
//! #[derive(Parser, Clone)]
//! struct MyCommand {
//!     #[command(flatten)]
//!     schedule: ScheduleOption,
//!
//!     #[arg(long)]
//!     message: String,
//! }
//!
//! #[async_trait::async_trait]
//! impl Schedulable for MyCommand {
//!     fn schedule(&self) -> &ScheduleOption {
//!         &self.schedule
//!     }
//!
//!     async fn execute_once(&self) -> Result<()> {
//!         println!("{}", self.message);
//!         Ok(())
//!     }
//! }
//! ```

use std::time::Duration;

use anyhow::{Result, bail};
use clap::Args;
use tokio_cron_scheduler::{Job, JobScheduler};
use tracing::{error, info};

/// Schedule configuration that can be flattened into command structs.
#[derive(Debug, Args, Clone, Default)]
pub struct ScheduleOption {
    /// Schedule interval (e.g. "5s", "10m", "2h"). If not provided, runs once
    /// and exits.
    #[arg(long, help = "Schedule interval (e.g. '5s', '10m', '2h')")]
    pub schedule: Option<String>,
}

impl ScheduleOption {
    /// Check if a schedule is configured.
    pub fn is_scheduled(&self) -> bool {
        self.schedule.is_some()
    }
}

/// Trait for commands that can be scheduled to run at intervals.
#[async_trait::async_trait]
pub trait Schedulable: Clone {
    /// Get the schedule configuration.
    fn schedule(&self) -> &ScheduleOption;

    /// Execute the command once - this is what implementors define.
    async fn execute_once(&self) -> Result<()>;

    /// Execute the command, either once or on schedule.
    ///
    /// This method checks if a schedule is provided and either:
    /// - Runs `execute_once()` immediately if no schedule.
    /// - Sets up a cron job to run `execute_once()` at intervals if scheduled.
    async fn execute(&self) -> Result<()>
    where
        Self: Sized + Send + Sync + 'static,
    {
        run_schedulable(self).await
    }
}

/// Run a schedulable command, handling both one-time and scheduled execution.
pub async fn run_schedulable<T: Schedulable + Send + Sync + 'static>(command: &T) -> Result<()> {
    match command.schedule().schedule.as_deref() {
        Some(schedule_str) => {
            let cron_expr = schedule_to_cron(schedule_str)?;

            let command_clone = command.clone();
            let job = Job::new_async(cron_expr.as_str(), move |_uuid, _l| {
                let command = command_clone.clone();

                Box::pin(async move {
                    if let Err(e) = command.execute_once().await {
                        error!("Command execution failed: {e}");
                    }
                })
            })?;

            let sched = JobScheduler::new().await?;
            sched.add(job).await?;
            sched.start().await?;

            info!("Scheduler started. Command will run every {schedule_str}");
            info!("Press Ctrl+C to stop...");

            tokio::signal::ctrl_c().await?;
            info!("Shutting down...");
        }
        None => {
            command.execute_once().await?;
        }
    }

    Ok(())
}

/// Convert a schedule string to a cron expression.
///
/// Supports formats like "5s", "10m", "2h" or plain numbers (treated as
/// seconds). Maximum allowed duration is less than 24 hours.
fn schedule_to_cron(s: &str) -> Result<String> {
    let s = s.trim().to_lowercase();

    let duration = if let Some(num_str) = s.strip_suffix('s') {
        let secs: u64 = num_str.parse()?;
        Duration::from_secs(secs)
    } else if let Some(num_str) = s.strip_suffix('m') {
        let mins: u64 = num_str.parse()?;
        Duration::from_secs(mins * 60)
    } else if let Some(num_str) = s.strip_suffix('h') {
        let hours: u64 = num_str.parse()?;
        Duration::from_secs(hours * 3600)
    } else {
        let secs: u64 = s.parse()?;
        Duration::from_secs(secs)
    };

    // Check if duration is 24 hours or more.
    if duration.as_secs() >= 24 * 3600 {
        bail!("Schedule duration '{s}' is too long. Maximum allowed is less than 24 hours");
    }

    // Convert to cron expression.
    let secs = duration.as_secs();
    if secs < 60 {
        Ok(format!("*/{secs} * * * * *"))
    } else if secs < 3600 {
        let mins = secs / 60;
        Ok(format!("0 */{mins} * * * *"))
    } else {
        let hours = secs / 3600;
        Ok(format!("0 0 */{hours} * * *"))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_schedule_to_cron() {
        // Test direct conversion.
        assert_eq!(schedule_to_cron("30s").unwrap(), "*/30 * * * * *");
        assert_eq!(schedule_to_cron("2m").unwrap(), "0 */2 * * * *");
        assert_eq!(schedule_to_cron("2h").unwrap(), "0 0 */2 * * *");

        // Test plain numbers (seconds).
        assert_eq!(schedule_to_cron("5").unwrap(), "*/5 * * * * *");
        assert_eq!(schedule_to_cron("120").unwrap(), "0 */2 * * * *");

        // Test case insensitive.
        assert_eq!(schedule_to_cron("5S").unwrap(), "*/5 * * * * *");
        assert_eq!(schedule_to_cron("10M").unwrap(), "0 */10 * * * *");

        // Test whitespace.
        assert_eq!(schedule_to_cron(" 5s ").unwrap(), "*/5 * * * * *");

        // Test 24 hour limit.
        assert!(schedule_to_cron("24h").is_err());
        assert!(schedule_to_cron("86400").is_err());
        assert!(schedule_to_cron("23h").is_ok());
    }

    #[test]
    fn test_schedule() {
        let schedule = ScheduleOption::default();
        assert!(!schedule.is_scheduled());

        let schedule = ScheduleOption {
            schedule: Some("5m".to_string()),
        };
        assert!(schedule.is_scheduled());
    }
}
