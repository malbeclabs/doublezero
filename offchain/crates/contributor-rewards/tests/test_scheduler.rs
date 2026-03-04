#[cfg(test)]
mod tests {
    use std::fs;

    use doublezero_contributor_rewards::scheduler::SchedulerState;
    use tempfile::TempDir;

    #[test]
    fn test_worker_state_persistence() {
        let temp_dir = TempDir::new().unwrap();
        let state_file = temp_dir.path().join("test.state");

        // Create and save state
        let mut state = SchedulerState::default();
        state.mark_success(100);
        state.save(&state_file).unwrap();

        // Load state and verify
        let loaded_state = SchedulerState::load_or_default(&state_file).unwrap();
        assert_eq!(loaded_state.last_processed_epoch, Some(100));
        assert_eq!(loaded_state.consecutive_failures, 0);
    }

    #[test]
    fn test_should_process_epoch() {
        let mut state = SchedulerState::default();

        // Should process when no epoch has been processed
        assert!(state.should_process_epoch(1));

        // Mark epoch 5 as processed
        state.mark_success(5);

        // Should not process epochs <= 5
        assert!(!state.should_process_epoch(5));
        assert!(!state.should_process_epoch(4));

        // Should process epochs > 5
        assert!(state.should_process_epoch(6));
        assert!(state.should_process_epoch(10));
    }

    #[test]
    fn test_failure_tracking() {
        let mut state = SchedulerState::default();

        // Initially no failures
        assert_eq!(state.consecutive_failures, 0);
        assert!(!state.is_in_failure_state(5));

        // Track failures
        state.mark_failure();
        assert_eq!(state.consecutive_failures, 1);

        state.mark_failure();
        assert_eq!(state.consecutive_failures, 2);

        // Check failure state
        assert!(!state.is_in_failure_state(3));
        assert!(state.is_in_failure_state(2)); // Exactly at threshold

        // Success resets failures
        state.mark_success(10);
        assert_eq!(state.consecutive_failures, 0);
        assert!(!state.is_in_failure_state(1));
    }

    #[test]
    fn test_state_file_creation() {
        let temp_dir = TempDir::new().unwrap();
        let non_existent_path = temp_dir.path().join("subdir").join("state.json");

        // Should create parent directories
        let mut state = SchedulerState::default();
        state.mark_success(42);
        state.save(&non_existent_path).unwrap();

        // Verify file was created and can be loaded
        assert!(non_existent_path.exists());
        let loaded = SchedulerState::load_or_default(&non_existent_path).unwrap();
        assert_eq!(loaded.last_processed_epoch, Some(42));
    }

    /// Test state corruption recovery - creates backup and returns default
    #[test]
    fn test_state_corruption_recovery() {
        let temp_dir = TempDir::new().unwrap();
        let state_file = temp_dir.path().join("corrupted.state");

        // Write corrupted JSON
        fs::write(&state_file, "{ this is not valid json }").unwrap();

        // Should recover by returning default state
        let state = SchedulerState::load_or_default(&state_file).unwrap();
        assert!(state.last_processed_epoch.is_none());
        assert_eq!(state.consecutive_failures, 0);

        // Should have created a backup file
        let backup_path = state_file.with_extension("state.backup");
        assert!(backup_path.exists());
    }

    /// Test atomic save pattern (uses temp file + rename)
    #[test]
    fn test_atomic_save_pattern() {
        let temp_dir = TempDir::new().unwrap();
        let state_file = temp_dir.path().join("atomic.state");

        let mut state = SchedulerState::default();
        state.mark_success(50);
        state.save(&state_file).unwrap();

        // Temp file should not exist after save completes
        let temp_path = state_file.with_extension("state.tmp");
        assert!(!temp_path.exists());

        // Main file should exist with correct content
        assert!(state_file.exists());
        let loaded = SchedulerState::load_or_default(&state_file).unwrap();
        assert_eq!(loaded.last_processed_epoch, Some(50));
    }

    /// Test mark_check updates last_check_time
    #[test]
    fn test_mark_check() {
        let mut state = SchedulerState::default();
        let initial_check_time = state.last_check_time;

        // Small delay to ensure time difference
        std::thread::sleep(std::time::Duration::from_millis(10));

        state.mark_check();

        // Check time should have been updated
        assert!(state.last_check_time > initial_check_time);
    }

    /// Test mark_snapshot_created
    #[test]
    fn test_mark_snapshot_created() {
        let mut state = SchedulerState::default();
        assert!(state.last_snapshot_location.is_none());

        state.mark_snapshot_created(42, "s3://bucket/snapshot-42.json".to_string());

        assert_eq!(
            state.last_snapshot_location,
            Some("s3://bucket/snapshot-42.json".to_string())
        );
    }

    /// Test success updates last_success_time
    #[test]
    fn test_success_updates_time() {
        let mut state = SchedulerState::default();
        assert!(state.last_success_time.is_none());

        state.mark_success(100);

        assert!(state.last_success_time.is_some());
        let success_time = state.last_success_time.unwrap();
        let now = chrono::Utc::now();
        let diff = (now - success_time).num_seconds().abs();
        assert!(diff < 5, "last_success_time should be close to now");
    }

    /// Test multiple failures accumulate
    #[test]
    fn test_multiple_failures_accumulate() {
        let mut state = SchedulerState::default();

        for i in 1..=10 {
            state.mark_failure();
            assert_eq!(state.consecutive_failures, i);
        }

        // 10 consecutive failures
        assert!(state.is_in_failure_state(10));
        assert!(state.is_in_failure_state(5));
        assert!(!state.is_in_failure_state(11));
    }

    /// Test that save creates valid JSON
    #[test]
    fn test_save_creates_valid_json() {
        let temp_dir = TempDir::new().unwrap();
        let state_file = temp_dir.path().join("json.state");

        let mut state = SchedulerState::default();
        state.mark_success(100);
        state.mark_snapshot_created(100, "s3://bucket/snapshot-100.json".to_string());
        state.save(&state_file).unwrap();

        // Read raw file and verify it's valid JSON
        let contents = fs::read_to_string(&state_file).unwrap();
        let parsed: serde_json::Value = serde_json::from_str(&contents).unwrap();

        let obj = &parsed.as_object();

        let mut state_keys: Vec<&String> = obj.unwrap().keys().collect();
        state_keys.sort();
        // make sure the keys don't change unexpectedly
        assert_eq!(
            state_keys,
            [
                "consecutive_distribution_failures",
                "consecutive_failures",
                "last_check_time",
                "last_distributed_epoch",
                "last_processed_epoch",
                "last_snapshot_location",
                "last_success_time"
            ]
        );

        // make sure we have values
        assert_eq!(parsed["last_processed_epoch"], 100);
        assert_eq!(parsed["consecutive_failures"], 0);
        assert_eq!(
            parsed["last_snapshot_location"],
            "s3://bucket/snapshot-100.json"
        );
        assert!(parsed["last_check_time"].is_string());
        assert!(parsed["last_success_time"].is_string());
    }
}
