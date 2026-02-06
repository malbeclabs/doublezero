use crate::doublezerocommand::CliCommand;
use doublezero_sdk::{commands::programconfig::get::GetProgramConfigCommand, ProgramVersion};
use std::io::Write;

// NOTE: if the client is out of date, there is an error because the client warning will cause the json to be malformed. This was resolved in this PR (https://github.com/malbeclabs/doublezero/pull/2807) but the global monitor and maybe other things will break so these tests capture the expected format. The json response should be fixed sooner than later.
pub fn check_version<C: CliCommand, W: Write>(
    client: &C,
    out: &mut W,
    client_version: ProgramVersion,
) -> eyre::Result<()> {
    // Check the program configuration version
    if let Ok((_, pconfig)) = client.get_program_config(GetProgramConfigCommand) {
        // Compare the program version with the client version
        // If the program version is incompatible, return an error
        if client_version < pconfig.min_compatible_version {
            eyre::bail!("A new version of the client is available: {} → {}\nYour client version is no longer up to date. Please update it before continuing to use the client.", client_version, pconfig.min_compatible_version);
        }
        // Warn the user if their client version is older than the program version
        if client_version < pconfig.version {
            writeln!(out, "A new version of the client is available: {} → {}\nWe recommend updating to the latest version for the best experience.", client_version, pconfig.version)?;
        }
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use crate::doublezerocommand::MockCliCommand;
    use doublezero_sdk::AccountType;
    use doublezero_serviceability::state::programconfig::ProgramConfig;
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    use super::*;

    fn test_check_version_helper(
        out: &mut Vec<u8>,
        contract_version: ProgramVersion,
        min_compatible_version: ProgramVersion,
        client_version: ProgramVersion,
    ) -> eyre::Result<()> {
        let mut client = MockCliCommand::new();

        client
            .expect_get_program_config()
            .with(predicate::eq(GetProgramConfigCommand))
            .returning(move |_| {
                let program_config = ProgramConfig {
                    account_type: AccountType::ProgramConfig,
                    bump_seed: 1,
                    version: contract_version.clone(),
                    min_compatible_version: min_compatible_version.clone(),
                };
                Ok((Pubkey::new_unique(), program_config))
            });

        check_version(&client, out, client_version)
    }

    /// Test: Client version equals program version - no output, no error
    #[test]
    fn test_check_version_ok() {
        let mut output = Vec::new();
        let res = test_check_version_helper(
            &mut output,
            ProgramVersion::new(1, 0, 0),
            ProgramVersion::new(0, 9, 0),
            ProgramVersion::new(1, 0, 0),
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "");
    }

    /// Test: Client version is newer than program version - no output, no error
    #[test]
    fn test_check_version_client_newer_minor() {
        let mut output = Vec::new();
        let res = test_check_version_helper(
            &mut output,
            ProgramVersion::new(1, 1, 0),
            ProgramVersion::new(1, 0, 0),
            ProgramVersion::new(1, 2, 0),
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "");
    }

    /// Test: Client version is newer major than program version - no output, no error
    #[test]
    fn test_check_version_client_newer_major() {
        let mut output = Vec::new();
        let res = test_check_version_helper(
            &mut output,
            ProgramVersion::new(1, 0, 0),
            ProgramVersion::new(0, 9, 0),
            ProgramVersion::new(2, 0, 0),
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "");
    }

    /// Test: Client version is older but still compatible - warning message, no error
    #[test]
    fn test_check_version_outdated_but_compatible() {
        let mut output = Vec::new();
        let res = test_check_version_helper(
            &mut output,
            ProgramVersion::new(1, 5, 10),
            ProgramVersion::new(1, 1, 0),
            ProgramVersion::new(1, 2, 0),
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "A new version of the client is available: 1.2.0 → 1.5.10\nWe recommend updating to the latest version for the best experience.\n");
    }

    /// Test: Client version is below minimum compatible version - error returned
    #[test]
    fn test_check_version_incompatible() {
        let mut output = Vec::new();
        let res = test_check_version_helper(
            &mut output,
            ProgramVersion::new(1, 5, 10),
            ProgramVersion::new(1, 1, 0),
            ProgramVersion::new(1, 0, 0),
        );
        assert!(res.is_err());
        assert_eq!(
            res.unwrap_err().to_string(),
            "A new version of the client is available: 1.0.0 → 1.1.0\nYour client version is no longer up to date. Please update it before continuing to use the client."
        );
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "");
    }

    /// Test: Client version exactly at minimum compatible version - no error
    #[test]
    fn test_check_version_at_minimum_compatible() {
        let mut output = Vec::new();
        let res = test_check_version_helper(
            &mut output,
            ProgramVersion::new(1, 5, 0), // program version
            ProgramVersion::new(1, 2, 0), // min compatible
            ProgramVersion::new(1, 2, 0), // client version (exactly at min)
        );
        assert!(res.is_ok());
        // Should show upgrade recommendation since client < program version
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("A new version of the client is available"));
    }

    /// Test: Program config unavailable - gracefully succeeds (no-op)
    #[test]
    fn test_check_version_config_unavailable() {
        let mut client = MockCliCommand::new();
        client
            .expect_get_program_config()
            .with(predicate::eq(GetProgramConfigCommand))
            .returning(|_| Err(eyre::eyre!("RPC error")));

        let mut output = Vec::new();
        let res = check_version(&client, &mut output, ProgramVersion::new(1, 0, 0));

        // Should succeed even if config is unavailable
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "");
    }

    /// Test: Patch version difference - client older patch, still compatible
    #[test]
    fn test_check_version_patch_difference() {
        let mut output = Vec::new();
        let res = test_check_version_helper(
            &mut output,
            ProgramVersion::new(1, 2, 5), // program version
            ProgramVersion::new(1, 2, 0), // min compatible
            ProgramVersion::new(1, 2, 3), // client version (older patch)
        );
        assert!(res.is_ok());
        // Should show upgrade recommendation since client < program version
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("1.2.3 → 1.2.5"));
    }
}
