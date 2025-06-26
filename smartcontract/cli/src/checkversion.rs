use crate::doublezerocommand::CliCommand;
use doublezero_sdk::{commands::programconfig::get::GetProgramConfigCommand, ProgramVersion};
use std::io::Write;

pub fn check_version<C: CliCommand, W: Write>(
    client: &C,
    out: &mut W,
    client_version: ProgramVersion,
) -> eyre::Result<()> {
    // Check the program configuration version
    if let Ok((_, pconfig)) = client.get_program_config(GetProgramConfigCommand {}) {
        // Compare the program version with the client version
        // If the program version is incompatible, return an error
        if pconfig.version.error(&client_version) {
            eyre::bail!("Your client version is no longer up to date. Please update it before continuing to use the client.")
        }
        // If the program version is compatible, but the client version is behind, print a warning
        if pconfig.version.warning(&client_version) {
            writeln!(out, "A new version of the client is available. We recommend updating to the latest version for the best experience.")?;
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

    pub fn test_check_version(
        out: &mut Vec<u8>,
        contract_version: ProgramVersion,
        client_version: ProgramVersion,
    ) -> eyre::Result<()> {
        let mut client = MockCliCommand::new();

        client
            .expect_get_program_config()
            .with(predicate::eq(GetProgramConfigCommand {}))
            .returning(move |_| {
                let program_config = ProgramConfig {
                    account_type: AccountType::ProgramConfig,
                    bump_seed: 1,
                    version: contract_version.clone(),
                };
                Ok((Pubkey::new_unique(), program_config))
            });

        check_version(&client, out, client_version)
    }

    #[test]
    fn test_check_version_ok() {
        let mut output = Vec::new();
        let res = test_check_version(
            &mut output,
            ProgramVersion::new(1, 0, 0),
            ProgramVersion::new(1, 0, 0),
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "");
    }

    #[test]
    fn test_check_version_minor_ok() {
        let mut output = Vec::new();
        let res = test_check_version(
            &mut output,
            ProgramVersion::new(1, 1, 0),
            ProgramVersion::new(1, 2, 0),
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "");
    }

    #[test]
    fn test_check_version_major_ok() {
        let mut output = Vec::new();
        let res = test_check_version(
            &mut output,
            ProgramVersion::new(1, 0, 0),
            ProgramVersion::new(2, 0, 0),
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "");
    }

    #[test]
    fn test_check_version_build_warning() {
        let mut output = Vec::new();
        let res = test_check_version(
            &mut output,
            ProgramVersion::new(1, 2, 10),
            ProgramVersion::new(1, 2, 0),
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "A new version of the client is available. We recommend updating to the latest version for the best experience.\n");
    }
}
