pub mod processor;
pub mod settings;
pub mod shapley_types;

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;

/// Processed metrics for a single link
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ProcessedLinkMetrics {
    pub link_pubkey: String,
    pub uptime_percentage: f64,
    pub latency_p50_us: f64,
    pub latency_p95_us: f64,
    pub latency_p99_us: f64,
    pub sample_count: u64,
    pub performance_score: f64,
}

/// Struct for all processed metrics
#[derive(Debug, Default, Clone, Serialize, Deserialize)]
pub struct ProcessedMetrics {
    pub link_metrics: HashMap<String, ProcessedLinkMetrics>,
    pub total_links: usize,
    pub processing_timestamp: DateTime<Utc>,
}
