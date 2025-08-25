//! Schedule types for job execution

use borsh::{BorshDeserialize, BorshSerialize};
use serde::{Deserialize, Serialize};
use std::time::Duration;

/// Defines when a job should run
#[derive(
    Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, BorshSerialize, BorshDeserialize,
)]
pub enum Schedule {
    /// Run every N seconds
    Interval { seconds: u64 },

    /// Run on epoch change with optional grace period
    EpochChange {
        /// Seconds to wait after epoch change before execution
        grace_seconds: u64,
    },
}

impl Schedule {
    /// Create an interval schedule
    pub fn interval(seconds: u64) -> Self {
        Schedule::Interval { seconds }
    }

    /// Create an epoch change schedule
    pub fn epoch_change(grace_seconds: u64) -> Self {
        Schedule::EpochChange { grace_seconds }
    }

    /// Get the duration for interval schedules
    pub fn as_duration(&self) -> Option<Duration> {
        match self {
            Schedule::Interval { seconds } => Some(Duration::from_secs(*seconds)),
            Schedule::EpochChange { .. } => None,
        }
    }

    /// Get the grace period for epoch schedules
    pub fn grace_period(&self) -> Option<Duration> {
        match self {
            Schedule::EpochChange { grace_seconds } => Some(Duration::from_secs(*grace_seconds)),
            Schedule::Interval { .. } => None,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_schedule_creation() {
        let interval = Schedule::interval(60);
        assert_eq!(interval.as_duration(), Some(Duration::from_secs(60)));
        assert_eq!(interval.grace_period(), None);

        let epoch = Schedule::epoch_change(900);
        assert_eq!(epoch.as_duration(), None);
        assert_eq!(epoch.grace_period(), Some(Duration::from_secs(900)));
    }
}
