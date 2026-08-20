//! Per-source-IP rate limiting.
//!
//! Signing is cheap but not free, and an unbounded request rate from one address is both a way to
//! spend the service's CPU and a way to mine for a moment when the epoch is rolling over. A token
//! bucket per address gives a small burst and a steady drip, which is the shape a legitimate
//! `doublezero connect` needs (one request, occasionally retried).

use std::{collections::HashMap, net::IpAddr, sync::Mutex, time::Instant};

/// Bucket state for one address. Tokens are fractional so a sub-token-per-second rate still
/// accumulates instead of rounding away.
#[derive(Debug, Clone, Copy)]
struct Bucket {
    tokens: f64,
    last_seen: Instant,
}

pub struct RateLimiter {
    /// Most requests one address may make back to back.
    burst: f64,
    /// Sustained rate, in tokens per second.
    refill_per_sec: f64,
    /// Address count above which idle buckets are dropped. A bucket at full capacity carries no
    /// information, so forgetting it cannot let anyone exceed the limit — it only bounds memory
    /// against a spray of one request per address.
    max_entries: usize,
    buckets: Mutex<HashMap<IpAddr, Bucket>>,
}

impl RateLimiter {
    pub fn new(burst: u32, per_minute: u32, max_entries: usize) -> Self {
        Self {
            burst: burst.max(1) as f64,
            refill_per_sec: f64::from(per_minute.max(1)) / 60.0,
            max_entries,
            buckets: Mutex::new(HashMap::new()),
        }
    }

    /// Takes one token for `addr`, returning false when the address is over its limit.
    pub fn check(&self, addr: IpAddr) -> bool {
        self.check_at(addr, Instant::now())
    }

    fn check_at(&self, addr: IpAddr, now: Instant) -> bool {
        let mut buckets = self
            .buckets
            .lock()
            .expect("rate limiter lock is never held across a panic");

        if buckets.len() >= self.max_entries && !buckets.contains_key(&addr) {
            self.evict_idle(&mut buckets, now);
        }

        let bucket = buckets.entry(addr).or_insert(Bucket {
            tokens: self.burst,
            last_seen: now,
        });

        let elapsed = now
            .saturating_duration_since(bucket.last_seen)
            .as_secs_f64();
        bucket.tokens = (bucket.tokens + elapsed * self.refill_per_sec).min(self.burst);
        bucket.last_seen = now;

        if bucket.tokens < 1.0 {
            return false;
        }

        bucket.tokens -= 1.0;
        true
    }

    /// Drops every bucket that has refilled to capacity — the ones whose absence changes nothing.
    fn evict_idle(&self, buckets: &mut HashMap<IpAddr, Bucket>, now: Instant) {
        let burst = self.burst;
        let refill = self.refill_per_sec;

        buckets.retain(|_, bucket| {
            let elapsed = now
                .saturating_duration_since(bucket.last_seen)
                .as_secs_f64();
            bucket.tokens + elapsed * refill < burst
        });
    }

    #[cfg(test)]
    fn len(&self) -> usize {
        self.buckets.lock().unwrap().len()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::Duration;

    fn addr(last_octet: u8) -> IpAddr {
        IpAddr::from([198, 18, 0, last_octet])
    }

    #[test]
    fn a_burst_is_allowed_then_refused() {
        let limiter = RateLimiter::new(3, 60, 1024);
        let now = Instant::now();

        assert!(limiter.check_at(addr(1), now));
        assert!(limiter.check_at(addr(1), now));
        assert!(limiter.check_at(addr(1), now));
        assert!(!limiter.check_at(addr(1), now));
    }

    #[test]
    fn tokens_refill_over_time() {
        // 60 per minute is one token per second.
        let limiter = RateLimiter::new(1, 60, 1024);
        let start = Instant::now();

        assert!(limiter.check_at(addr(1), start));
        assert!(!limiter.check_at(addr(1), start + Duration::from_millis(500)));
        assert!(limiter.check_at(addr(1), start + Duration::from_secs(1)));
    }

    #[test]
    fn refill_is_capped_at_the_burst() {
        let limiter = RateLimiter::new(2, 60, 1024);
        let start = Instant::now();

        assert!(limiter.check_at(addr(1), start));
        // An hour of idling does not bank an hour of requests.
        let later = start + Duration::from_secs(3600);
        assert!(limiter.check_at(addr(1), later));
        assert!(limiter.check_at(addr(1), later));
        assert!(!limiter.check_at(addr(1), later));
    }

    #[test]
    fn addresses_are_limited_independently() {
        let limiter = RateLimiter::new(1, 60, 1024);
        let now = Instant::now();

        assert!(limiter.check_at(addr(1), now));
        assert!(!limiter.check_at(addr(1), now));
        assert!(limiter.check_at(addr(2), now));
    }

    #[test]
    fn idle_buckets_are_evicted_but_active_ones_survive() {
        let limiter = RateLimiter::new(1, 60, 3);
        let start = Instant::now();

        // Two addresses spend their token; one of them stays over its limit.
        assert!(limiter.check_at(addr(1), start));
        assert!(limiter.check_at(addr(2), start + Duration::from_secs(60)));
        assert!(limiter.check_at(addr(3), start + Duration::from_secs(60)));
        assert_eq!(limiter.len(), 3);

        // A fourth address triggers eviction: addr(1) has refilled, addr(2) and addr(3) have not.
        assert!(limiter.check_at(addr(4), start + Duration::from_secs(60)));
        assert!(!limiter.check_at(addr(2), start + Duration::from_secs(60)));
        assert!(limiter.len() <= 3);
    }
}
