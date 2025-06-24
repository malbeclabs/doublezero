use crate::doublezerocommand::CliCommand;
use doublezero_sdk::{commands::programconfig::get::GetProgramConfigCommand, ProgramVersion};
use std::io::Write;

pub fn check_version<C: CliCommand, W: Write>(client: &C, out: &mut W) -> eyre::Result<()> {
    // Check the program configuration version
    match client.get_program_config(GetProgramConfigCommand {}) {
        Ok((_, pconfig)) => {
            let version = ProgramVersion::get_cargo_pkg_version();

            if pconfig.version.error(&version) {
                eyre::bail!("Your client version is no longer up to date. Please update it before continuing to use the client.")
            }
            if pconfig.version.warning(&version) {
                writeln!(out, "A new version of the client is available. We recommend updating to the latest version for the best experience.")?;
            }
        }
        Err(_) => {}
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

    #[test]
    fn test_check_version_ok() {
        let mut client = MockCliCommand::new();

        let version = ProgramVersion::get_cargo_pkg_version();

        client
            .expect_get_program_config()
            .with(predicate::eq(GetProgramConfigCommand {}))
            .returning(move |_| {
                let program_config = ProgramConfig {
                    account_type: AccountType::ProgramConfig,
                    bump_seed: 1,
                    version: version.clone(),
                };
                Ok((Pubkey::new_unique(), program_config))
            });

        let mut output = Vec::new();

        let res = check_version(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "");
    }

    #[test]
    fn test_check_version_minor_ok() {
        let mut client = MockCliCommand::new();

        let version = ProgramVersion::get_cargo_pkg_version();

        client
            .expect_get_program_config()
            .with(predicate::eq(GetProgramConfigCommand {}))
            .returning(move |_| {
                let program_config = ProgramConfig {
                    account_type: AccountType::ProgramConfig,
                    bump_seed: 1,
                    version: ProgramVersion::new(version.mayor, version.minor + 1, 0),
                };
                Ok((Pubkey::new_unique(), program_config))
            });

        let mut output = Vec::new();

        let res = check_version(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "");
    }

    #[test]
    fn test_check_version_mayor_ok() {
        let mut client = MockCliCommand::new();

        let version = ProgramVersion::get_cargo_pkg_version();

        client
            .expect_get_program_config()
            .with(predicate::eq(GetProgramConfigCommand {}))
            .returning(move |_| {
                let program_config = ProgramConfig {
                    account_type: AccountType::ProgramConfig,
                    bump_seed: 1,
                    version: ProgramVersion::new(version.mayor + 1, 0, 0),
                };
                Ok((Pubkey::new_unique(), program_config))
            });

        let mut output = Vec::new();

        let res = check_version(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "");
    }

    #[test]
    fn test_check_version_build_warning() {
        let mut client = MockCliCommand::new();

        let version = ProgramVersion::get_cargo_pkg_version();

        client
            .expect_get_program_config()
            .with(predicate::eq(GetProgramConfigCommand {}))
            .returning(move |_| {
                let program_config = ProgramConfig {
                    account_type: AccountType::ProgramConfig,
                    bump_seed: 1,
                    version: ProgramVersion::new(version.mayor, version.minor, version.patch - 1),
                };
                Ok((Pubkey::new_unique(), program_config))
            });

        let mut output = Vec::new();

        let res = check_version(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "A new version of the client is available. We recommend updating to the latest version for the best experience.\n");
    }

    #[test]
    fn test_check_version_minor_failure() {
        let mut client = MockCliCommand::new();

        let version = ProgramVersion::get_cargo_pkg_version();

        client
            .expect_get_program_config()
            .with(predicate::eq(GetProgramConfigCommand {}))
            .returning(move |_| {
                let program_config = ProgramConfig {
                    account_type: AccountType::ProgramConfig,
                    bump_seed: 1,
                    version: ProgramVersion::new(version.mayor, version.minor - 1, 0),
                };
                Ok((Pubkey::new_unique(), program_config))
            });

        let mut output = Vec::new();

        let res = check_version(&client, &mut output);
        assert!(res.is_err());
        assert!(res.unwrap_err().to_string().contains(
            "Your client version is no longer up to date. Please update it before continuing to use the client."
        ));
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "");
    }

    #[test]
    fn test_verification_version_major_failure() {
        let mut client = MockCliCommand::new();

        let version = ProgramVersion::get_cargo_pkg_version();

        client
            .expect_get_program_config()
            .with(predicate::eq(GetProgramConfigCommand {}))
            .returning(move |_| {
                let program_config = ProgramConfig {
                    account_type: AccountType::ProgramConfig,
                    bump_seed: 1,
                    version: ProgramVersion::new(version.mayor - 1, 0, 0),
                };
                Ok((Pubkey::new_unique(), program_config))
            });

        let mut output = Vec::new();

        let res = check_version(&client, &mut output);
        assert!(res.is_err());
        assert!(res.unwrap_err().to_string().contains(
            "Your client version is no longer up to date. Please update it before continuing to use the client."
        ));
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "");
    }
}
