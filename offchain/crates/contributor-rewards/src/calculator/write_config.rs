//! Configuration for controlling which write operations are performed during reward calculation.

/// Configuration for controlling which write operations to skip during reward calculation.
///
/// This struct allows fine-grained control over the 5 write operations that occur when
/// calculating rewards:
/// 1. Device telemetry aggregates -> DZ Ledger
/// 2. Internet telemetry aggregates -> DZ Ledger
/// 3. Reward calculation input -> DZ Ledger
/// 4. Shapley output storage -> DZ Ledger
/// 5. Merkle root posting -> Solana
///
/// # Examples
///
/// ```
/// use doublezero_contributor_rewards::calculator::WriteConfig;
///
/// // Default: all writes enabled
/// let config = WriteConfig::default();
/// assert!(!config.all_writes_skipped());
///
/// // Skip only device telemetry
/// let config = WriteConfig {
///     skip_device_telemetry: true,
///     ..Default::default()
/// };
/// assert!(config.should_skip_device_telemetry());
/// assert!(!config.should_skip_internet_telemetry());
///
/// // Skip all writes
/// let config = WriteConfig::skip_all();
/// assert!(config.all_writes_skipped());
/// ```
#[derive(Debug, Default, Clone, Copy, PartialEq, Eq)]
pub struct WriteConfig {
    /// Skip writing device telemetry aggregates to DZ Ledger
    pub skip_device_telemetry: bool,
    /// Skip writing internet telemetry aggregates to DZ Ledger
    pub skip_internet_telemetry: bool,
    /// Skip writing reward calculation input to DZ Ledger
    pub skip_reward_input: bool,
    /// Skip writing shapley output storage to DZ Ledger
    pub skip_shapley_output: bool,
    /// Skip posting merkle root to Solana
    pub skip_merkle_root: bool,
}

impl WriteConfig {
    /// Create a new WriteConfig with all writes enabled.
    pub fn new() -> Self {
        Self::default()
    }

    /// Create a WriteConfig that skips all write operations.
    pub fn skip_all() -> Self {
        Self {
            skip_device_telemetry: true,
            skip_internet_telemetry: true,
            skip_reward_input: true,
            skip_shapley_output: true,
            skip_merkle_root: true,
        }
    }

    /// Create a WriteConfig from CLI arguments.
    pub fn from_flags(
        skip_device_telemetry: bool,
        skip_internet_telemetry: bool,
        skip_reward_input: bool,
        skip_shapley_output: bool,
        skip_merkle_root: bool,
    ) -> Self {
        Self {
            skip_device_telemetry,
            skip_internet_telemetry,
            skip_reward_input,
            skip_shapley_output,
            skip_merkle_root,
        }
    }

    /// Returns true if device telemetry write should be skipped.
    pub fn should_skip_device_telemetry(&self) -> bool {
        self.skip_device_telemetry
    }

    /// Returns true if internet telemetry write should be skipped.
    pub fn should_skip_internet_telemetry(&self) -> bool {
        self.skip_internet_telemetry
    }

    /// Returns true if reward input write should be skipped.
    pub fn should_skip_reward_input(&self) -> bool {
        self.skip_reward_input
    }

    /// Returns true if shapley output storage write should be skipped.
    pub fn should_skip_shapley_output(&self) -> bool {
        self.skip_shapley_output
    }

    /// Returns true if merkle root posting should be skipped.
    pub fn should_skip_merkle_root(&self) -> bool {
        self.skip_merkle_root
    }

    /// Returns true if all write operations are skipped.
    pub fn all_writes_skipped(&self) -> bool {
        self.skip_device_telemetry
            && self.skip_internet_telemetry
            && self.skip_reward_input
            && self.skip_shapley_output
            && self.skip_merkle_root
    }

    /// Returns true if at least one write operation is enabled (not skipped).
    pub fn any_writes_enabled(&self) -> bool {
        !self.all_writes_skipped()
    }

    /// Returns the count of write operations that are skipped.
    pub fn skipped_count(&self) -> usize {
        let mut count = 0;
        if self.skip_device_telemetry {
            count += 1;
        }
        if self.skip_internet_telemetry {
            count += 1;
        }
        if self.skip_reward_input {
            count += 1;
        }
        if self.skip_shapley_output {
            count += 1;
        }
        if self.skip_merkle_root {
            count += 1;
        }
        count
    }

    /// Returns the count of write operations that are enabled (not skipped).
    pub fn enabled_count(&self) -> usize {
        5 - self.skipped_count()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_default_config() {
        let config = WriteConfig::default();
        assert!(!config.should_skip_device_telemetry());
        assert!(!config.should_skip_internet_telemetry());
        assert!(!config.should_skip_reward_input());
        assert!(!config.should_skip_shapley_output());
        assert!(!config.should_skip_merkle_root());
        assert!(!config.all_writes_skipped());
        assert!(config.any_writes_enabled());
        assert_eq!(config.skipped_count(), 0);
        assert_eq!(config.enabled_count(), 5);
    }

    #[test]
    fn test_new_config() {
        let config = WriteConfig::new();
        assert!(!config.all_writes_skipped());
        assert!(config.any_writes_enabled());
    }

    #[test]
    fn test_skip_all() {
        let config = WriteConfig::skip_all();
        assert!(config.should_skip_device_telemetry());
        assert!(config.should_skip_internet_telemetry());
        assert!(config.should_skip_reward_input());
        assert!(config.should_skip_shapley_output());
        assert!(config.should_skip_merkle_root());
        assert!(config.all_writes_skipped());
        assert!(!config.any_writes_enabled());
        assert_eq!(config.skipped_count(), 5);
        assert_eq!(config.enabled_count(), 0);
    }

    #[test]
    fn test_from_flags_single_skip() {
        let config = WriteConfig::from_flags(true, false, false, false, false);
        assert!(config.should_skip_device_telemetry());
        assert!(!config.should_skip_internet_telemetry());
        assert!(!config.all_writes_skipped());
        assert!(config.any_writes_enabled());
        assert_eq!(config.skipped_count(), 1);
        assert_eq!(config.enabled_count(), 4);
    }

    #[test]
    fn test_from_flags_multiple_skips() {
        let config = WriteConfig::from_flags(true, true, false, false, false);
        assert!(config.should_skip_device_telemetry());
        assert!(config.should_skip_internet_telemetry());
        assert!(!config.should_skip_reward_input());
        assert!(!config.all_writes_skipped());
        assert!(config.any_writes_enabled());
        assert_eq!(config.skipped_count(), 2);
        assert_eq!(config.enabled_count(), 3);
    }

    #[test]
    fn test_all_but_one_skipped() {
        let config = WriteConfig::from_flags(true, true, true, true, false);
        assert!(!config.should_skip_merkle_root());
        assert!(!config.all_writes_skipped());
        assert!(config.any_writes_enabled());
        assert_eq!(config.skipped_count(), 4);
        assert_eq!(config.enabled_count(), 1);
    }

    #[test]
    fn test_skip_counts() {
        // No skips
        let config = WriteConfig::default();
        assert_eq!(config.skipped_count(), 0);
        assert_eq!(config.enabled_count(), 5);

        // One skip
        let config = WriteConfig::from_flags(true, false, false, false, false);
        assert_eq!(config.skipped_count(), 1);
        assert_eq!(config.enabled_count(), 4);

        // Three skips
        let config = WriteConfig::from_flags(true, false, true, true, false);
        assert_eq!(config.skipped_count(), 3);
        assert_eq!(config.enabled_count(), 2);

        // All skips
        let config = WriteConfig::skip_all();
        assert_eq!(config.skipped_count(), 5);
        assert_eq!(config.enabled_count(), 0);
    }

    #[test]
    fn test_clone_and_copy() {
        let config1 = WriteConfig::from_flags(true, false, true, false, true);
        let config2 = config1;
        assert_eq!(config1, config2);
        assert_eq!(config1.skipped_count(), config2.skipped_count());
    }
}
