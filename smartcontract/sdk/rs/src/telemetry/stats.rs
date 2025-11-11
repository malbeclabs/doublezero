use solana_sdk::pubkey::Pubkey;

#[derive(Debug, Clone)]
pub struct LinkLatencyStats {
    pub epoch: u64,
    pub link_pk: Pubkey,
    pub link_code: Option<String>,
    pub origin_device_pk: Pubkey,
    pub target_device_pk: Pubkey,
    pub sample_count: usize,
    pub p50: f64,
    pub p90: f64,
    pub p95: f64,
    pub p99: f64,
    pub mean: f64,
    pub min: f64,
    pub max: f64,
    pub stddev: f64,
}

pub fn calculate_stats(
    epoch: u64,
    link_pk: Pubkey,
    link_code: Option<String>,
    origin_device_pk: Pubkey,
    target_device_pk: Pubkey,
    samples: &[u32],
) -> eyre::Result<LinkLatencyStats> {
    if samples.is_empty() {
        eyre::bail!("No samples available");
    }

    // Sort for percentiles
    let mut sorted_samples: Vec<f64> = samples.iter().map(|&s| (s as f64) / 1000.0).collect();
    sorted_samples.sort_by(|a, b| a.partial_cmp(b).unwrap());

    let n = sorted_samples.len();

    let p50 = percentile(&sorted_samples, 0.50);
    let p90 = percentile(&sorted_samples, 0.90);
    let p95 = percentile(&sorted_samples, 0.95);
    let p99 = percentile(&sorted_samples, 0.99);

    let sum: f64 = sorted_samples.iter().sum();
    let mean = sum / n as f64;

    let min = sorted_samples[0];
    let max = sorted_samples[n - 1];

    let variance: f64 = sorted_samples
        .iter()
        .map(|&x| {
            let diff = x - mean;
            diff * diff
        })
        .sum::<f64>()
        / n as f64;
    let stddev = variance.sqrt();

    Ok(LinkLatencyStats {
        epoch,
        link_pk,
        link_code,
        origin_device_pk,
        target_device_pk,
        sample_count: n,
        p50,
        p90,
        p95,
        p99,
        mean,
        min,
        max,
        stddev,
    })
}

fn percentile(sorted_samples: &[f64], p: f64) -> f64 {
    let n = sorted_samples.len() as f64;
    let index = (p * n).ceil() as usize - 1;
    sorted_samples[index]
}

#[cfg(test)]
mod tests {
    use super::calculate_stats;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn calculate_stats_test() {
        let epoch = 19500;
        let link_pk = Pubkey::new_unique();
        let origin_device_pk = Pubkey::new_unique();
        let target_device_pk = Pubkey::new_unique();
        let samples: &[u32] = &[10, 20, 30, 40, 50];

        let stats = calculate_stats(
            epoch,
            link_pk,
            None,
            origin_device_pk,
            target_device_pk,
            samples,
        )
        .unwrap();

        assert!((stats.p50 - 0.03).abs() < 1e-9);
        assert!((stats.p90 - 0.05).abs() < 1e-9);
        assert!((stats.p95 - 0.05).abs() < 1e-9);
        assert!((stats.p99 - 0.05).abs() < 1e-9);

        assert!((stats.mean - 0.03).abs() < 1e-9);
        assert!((stats.min - 0.01).abs() < 1e-9);
        assert!((stats.max - 0.05).abs() < 1e-9);

        assert!((stats.stddev - 0.0141421356237).abs() < 1e-9);
    }
}
