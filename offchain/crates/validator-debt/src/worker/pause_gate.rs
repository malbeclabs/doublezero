use std::sync::atomic::{AtomicBool, Ordering};

use doublezero_solana_sdk::revenue_distribution::state::ProgramConfig;

/// Tracks whether we've already seen the paused state (to avoid WARN spam).
static WAS_PAUSED: AtomicBool = AtomicBool::new(false);

/// Check if the Revenue Distribution program is paused.
/// Returns `true` if paused (caller should bail with `Ok`), `false` otherwise.
pub fn is_config_paused(config: &ProgramConfig) -> bool {
    let paused = config.is_paused();

    if paused {
        let was_previously_paused = WAS_PAUSED.swap(true, Ordering::SeqCst);
        if !was_previously_paused {
            tracing::warn!(
                "Revenue Distribution program is PAUSED. Skipping validator debt operations."
            );
        } else {
            tracing::debug!(
                "Revenue Distribution program is still paused. Skipping validator debt operations."
            );
        }
    } else {
        let was_previously_paused = WAS_PAUSED.swap(false, Ordering::SeqCst);
        if was_previously_paused {
            tracing::info!(
                "Revenue Distribution program has RESUMED. Proceeding with validator debt operations."
            );
        }
    }

    paused
}

#[cfg(test)]
mod tests {
    use super::*;

    fn reset_pause_state() {
        WAS_PAUSED.store(false, Ordering::SeqCst);
    }

    #[test]
    fn test_pause_transition_first_detection_triggers_warn_path() {
        reset_pause_state();

        let mut config = ProgramConfig::default();
        config.set_is_paused(true);

        let result = is_config_paused(&config);
        assert!(result); // paused = should skip
        assert!(WAS_PAUSED.load(Ordering::SeqCst));
    }

    #[test]
    fn test_pause_repeated_detection_does_not_retrigger() {
        reset_pause_state();

        let mut config = ProgramConfig::default();
        config.set_is_paused(true);

        let _ = is_config_paused(&config);
        assert!(WAS_PAUSED.load(Ordering::SeqCst));

        let was_before = WAS_PAUSED.load(Ordering::SeqCst);
        let result = is_config_paused(&config);
        assert!(result); // still paused
        assert_eq!(was_before, WAS_PAUSED.load(Ordering::SeqCst)); // unchanged
    }

    #[test]
    fn test_resume_transition_flips_state() {
        reset_pause_state();

        let mut paused_config = ProgramConfig::default();
        paused_config.set_is_paused(true);

        let _ = is_config_paused(&paused_config);
        assert!(WAS_PAUSED.load(Ordering::SeqCst));

        let mut unpaused_config = ProgramConfig::default();
        unpaused_config.set_is_paused(false);

        let result = is_config_paused(&unpaused_config);
        assert!(!result); // not paused = proceed
        assert!(!WAS_PAUSED.load(Ordering::SeqCst)); // state is now false
    }
}
