use crate::doublezerocommand::CliCommand;
use doublezero_sdk::{commands::programconfig::get::GetProgramConfigCommand, ProgramVersion};
use std::io::Write;

pub fn check_version<C: CliCommand, W: Write>(client: &C, out: &mut W) -> eyre::Result<()> {
    // Check the program configuration version
    match client.get_program_config(GetProgramConfigCommand {}) {
        Ok((_, pconfig)) => {
            let version = ProgramVersion::current().unwrap_or_default();

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

        let cli_version = ProgramVersion::current().unwrap_or_default();

        client
            .expect_get_program_config()
            .with(predicate::eq(GetProgramConfigCommand {}))
            .returning(move |_| {
                let program_config = ProgramConfig {
                    account_type: AccountType::ProgramConfig,
                    bump_seed: 1,
                    version: cli_version.clone(),
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

        let cli_version = ProgramVersion::current().unwrap_or_default();

        client
            .expect_get_program_config()
            .with(predicate::eq(GetProgramConfigCommand {}))
            .returning(move |_| {
                let program_config = ProgramConfig {
                    account_type: AccountType::ProgramConfig,
                    bump_seed: 1,
                    version: ProgramVersion::new(cli_version.major, cli_version.minor - 1, 0),
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
    fn test_check_version_major_ok() {
        let mut client = MockCliCommand::new();

        client
            .expect_get_program_config()
            .with(predicate::eq(GetProgramConfigCommand {}))
            .returning(move |_| {
                let program_config = ProgramConfig {
                    account_type: AccountType::ProgramConfig,
                    bump_seed: 1,
                    version: ProgramVersion::new(0, 0, 0),
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

        let cli_version = ProgramVersion::current().unwrap_or_default();

        client
            .expect_get_program_config()
            .with(predicate::eq(GetProgramConfigCommand {}))
            .returning(move |_| {
                let program_config = ProgramConfig {
                    account_type: AccountType::ProgramConfig,
                    bump_seed: 1,
                    version: ProgramVersion::new(
                        cli_version.major,
                        cli_version.minor,
                        cli_version.patch + 1,
                    ),
                };
                Ok((Pubkey::new_unique(), program_config))
            });

        let mut output = Vec::new();

        let res = check_version(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "A new version of the client is available. We recommend updating to the latest version for the best experience.\n");
    }
}
