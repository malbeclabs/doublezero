#[cfg(test)]
mod tests {
    use anyhow::Result;
    use contributor_rewards::{
        calculator::keypair_loader::load_keypair,
        processor::{internet::InternetTelemetryStatMap, telemetry::DZDTelemetryStatMap},
    };
    use std::{collections::HashMap, fs, path::PathBuf};
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
    fn test_borsh_serialization_dzd_telemetry() {
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
