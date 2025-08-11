#[cfg(test)]
mod tests {
    use anyhow::Result;
    use calculator::{keypair_loader::load_keypair, settings::Settings};
    use std::fs;
    use std::path::PathBuf;
    use tempfile::TempDir;

    #[test]
    fn test_keypair_cli_takes_precedence() -> Result<()> {
        // Create a temporary directory and keypair file
        let temp_dir = TempDir::new()?;
        let keypair_path = temp_dir.path().join("test_keypair.json");

        // Create a valid test keypair
        let test_keypair = solana_sdk::signature::Keypair::new();
        let keypair_bytes = test_keypair.to_bytes();
        fs::write(
            &keypair_path,
            serde_json::to_string(&keypair_bytes.to_vec())?,
        )?;

        // Set env var to a different path
        unsafe {
            std::env::set_var("REWARDER_KEYPAIR_PATH", "/some/other/path");
        }

        // CLI path should take precedence
        let result = load_keypair(&Some(keypair_path));
        assert!(result.is_ok());

        // Clean up
        unsafe {
            std::env::remove_var("REWARDER_KEYPAIR_PATH");
        }
        Ok(())
    }

    #[test]
    fn test_keypair_env_fallback() -> Result<()> {
        // Create a temporary directory and keypair file
        let temp_dir = TempDir::new()?;
        let keypair_path = temp_dir.path().join("test_keypair.json");

        // Create a valid test keypair
        let test_keypair = solana_sdk::signature::Keypair::new();
        let keypair_bytes = test_keypair.to_bytes();
        fs::write(
            &keypair_path,
            serde_json::to_string(&keypair_bytes.to_vec())?,
        )?;

        // Set env var
        unsafe {
            std::env::set_var("REWARDER_KEYPAIR_PATH", keypair_path.to_str().unwrap());
        }

        // Should use env var when no CLI path provided
        let result = load_keypair(&None);
        assert!(result.is_ok());

        // Clean up
        unsafe {
            std::env::remove_var("REWARDER_KEYPAIR_PATH");
        }
        Ok(())
    }

    #[test]
    fn test_keypair_not_provided_error() {
        // Ensure no env var is set
        unsafe {
            std::env::remove_var("REWARDER_KEYPAIR_PATH");
        }

        // Should return NotProvided error
        let result = load_keypair(&None);
        assert!(result.is_err());

        let err = result.unwrap_err();
        assert!(err.to_string().contains("Keypair not provided"));
    }

    #[test]
    fn test_keypair_file_not_found() {
        let non_existent_path = PathBuf::from("/non/existent/keypair.json");

        let result = load_keypair(&Some(non_existent_path));
        assert!(result.is_err());

        let err = result.unwrap_err();
        assert!(err.to_string().contains("Keypair file not found"));
    }

    #[test]
    fn test_keypair_invalid_format() -> Result<()> {
        // Create a temporary directory and invalid keypair file
        let temp_dir = TempDir::new()?;
        let keypair_path = temp_dir.path().join("invalid_keypair.json");

        // Write invalid JSON
        fs::write(&keypair_path, "not valid json")?;

        let result = load_keypair(&Some(keypair_path));
        assert!(result.is_err());

        let err = result.unwrap_err();
        assert!(err.to_string().contains("Invalid keypair format"));

        Ok(())
    }

    #[test]
    fn test_prefix_from_env() -> Result<()> {
        // Set prefix env vars and shapley settings
        unsafe {
            std::env::set_var("CALCULATOR__DEVICE_TELEMETRY_PREFIX", "test_device_prefix");
            std::env::set_var(
                "CALCULATOR__INTERNET_TELEMETRY_PREFIX",
                "test_internet_prefix",
            );
            // Set required shapley settings
            std::env::set_var("CALCULATOR__SHAPLEY__OPERATOR_UPTIME", "0.98");
            std::env::set_var("CALCULATOR__SHAPLEY__CONTIGUITY_BONUS", "5.0");
            std::env::set_var("CALCULATOR__SHAPLEY__DEMAND_MULTIPLIER", "1.2");
        }

        // Create settings (this would normally load from env)
        let settings = Settings::new::<PathBuf>(None)?;

        // Test that prefixes are loaded correctly
        let device_prefix = settings.get_device_telemetry_prefix(false)?;
        assert_eq!(device_prefix, b"test_device_prefix");

        let internet_prefix = settings.get_internet_telemetry_prefix(false)?;
        assert_eq!(internet_prefix, b"test_internet_prefix");

        // Clean up
        unsafe {
            std::env::remove_var("CALCULATOR__DEVICE_TELEMETRY_PREFIX");
            std::env::remove_var("CALCULATOR__INTERNET_TELEMETRY_PREFIX");
            std::env::remove_var("CALCULATOR__SHAPLEY__OPERATOR_UPTIME");
            std::env::remove_var("CALCULATOR__SHAPLEY__CONTIGUITY_BONUS");
            std::env::remove_var("CALCULATOR__SHAPLEY__DEMAND_MULTIPLIER");
        }

        Ok(())
    }

    #[test]
    fn test_prefix_required_in_non_dry_run() {
        // Ensure no prefix env vars are set but set required shapley settings
        unsafe {
            std::env::remove_var("CALCULATOR__DEVICE_TELEMETRY_PREFIX");
            std::env::remove_var("CALCULATOR__INTERNET_TELEMETRY_PREFIX");
            // Set required shapley settings
            std::env::set_var("CALCULATOR__SHAPLEY__OPERATOR_UPTIME", "0.98");
            std::env::set_var("CALCULATOR__SHAPLEY__CONTIGUITY_BONUS", "5.0");
            std::env::set_var("CALCULATOR__SHAPLEY__DEMAND_MULTIPLIER", "1.2");
        }

        // Create settings without prefixes
        let settings = Settings::new::<PathBuf>(None).unwrap();

        // Should fail when not in dry-run mode
        let device_result = settings.get_device_telemetry_prefix(false);
        assert!(device_result.is_err());

        let internet_result = settings.get_internet_telemetry_prefix(false);
        assert!(internet_result.is_err());

        // Clean up shapley env vars
        unsafe {
            std::env::remove_var("CALCULATOR__SHAPLEY__OPERATOR_UPTIME");
            std::env::remove_var("CALCULATOR__SHAPLEY__CONTIGUITY_BONUS");
            std::env::remove_var("CALCULATOR__SHAPLEY__DEMAND_MULTIPLIER");
        }
    }

    #[test]
    fn test_prefix_optional_in_dry_run() {
        // Ensure no prefix env vars are set but set required shapley settings
        unsafe {
            std::env::remove_var("CALCULATOR__DEVICE_TELEMETRY_PREFIX");
            std::env::remove_var("CALCULATOR__INTERNET_TELEMETRY_PREFIX");
            // Set required shapley settings
            std::env::set_var("CALCULATOR__SHAPLEY__OPERATOR_UPTIME", "0.98");
            std::env::set_var("CALCULATOR__SHAPLEY__CONTIGUITY_BONUS", "5.0");
            std::env::set_var("CALCULATOR__SHAPLEY__DEMAND_MULTIPLIER", "1.2");
        }

        // Create settings without prefixes
        let settings = Settings::new::<PathBuf>(None).unwrap();

        // Should provide defaults in dry-run mode
        let device_result = settings.get_device_telemetry_prefix(true);
        assert!(device_result.is_ok());
        assert_eq!(
            device_result.unwrap(),
            b"doublezero_device_telemetry_aggregate_test1"
        );

        let internet_result = settings.get_internet_telemetry_prefix(true);
        assert!(internet_result.is_ok());
        assert_eq!(
            internet_result.unwrap(),
            b"doublezero_internet_telemetry_aggregate_test1"
        );

        // Clean up shapley env vars
        unsafe {
            std::env::remove_var("CALCULATOR__SHAPLEY__OPERATOR_UPTIME");
            std::env::remove_var("CALCULATOR__SHAPLEY__CONTIGUITY_BONUS");
            std::env::remove_var("CALCULATOR__SHAPLEY__DEMAND_MULTIPLIER");
        }
    }

    #[test]
    fn test_borsh_serialization_dzd_telemetry() {
        use processor::telemetry::DZDTelemetryStatMap;
        use std::collections::HashMap;

        // Create an empty test telemetry map
        let stat_map: DZDTelemetryStatMap = HashMap::new();

        // Serialize
        let serialized = borsh::to_vec(&stat_map).unwrap();

        // Deserialize
        let deserialized: DZDTelemetryStatMap = borsh::from_slice(&serialized).unwrap();

        // Check round-trip - just verify it deserializes correctly
        assert_eq!(stat_map.len(), deserialized.len());
    }

    #[test]
    fn test_borsh_serialization_internet_telemetry() {
        use processor::internet::InternetTelemetryStatMap;
        use std::collections::HashMap;

        // Create an empty test telemetry map
        let stat_map: InternetTelemetryStatMap = HashMap::new();

        // Serialize
        let serialized = borsh::to_vec(&stat_map).unwrap();

        // Deserialize
        let deserialized: InternetTelemetryStatMap = borsh::from_slice(&serialized).unwrap();

        // Check round-trip - just verify it deserializes correctly
        assert_eq!(stat_map.len(), deserialized.len());
    }
}
