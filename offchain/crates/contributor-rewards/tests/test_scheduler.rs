#[cfg(test)]
mod tests {
    use contributor_rewards::scheduler::SchedulerState;
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
}
