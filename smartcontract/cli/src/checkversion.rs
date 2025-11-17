use crate::doublezerocommand::CliCommand;
use doublezero_sdk::{commands::programconfig::get::GetProgramConfigCommand, ProgramVersion};
use std::io::Write;

pub fn check_version<C: CliCommand, W: Write>(
    client: &C,
    out: &mut W,
    client_version: ProgramVersion,
) -> eyre::Result<()> {
    if let Ok((_, pconfig)) = client.get_program_config(GetProgramConfigCommand) {
        let program_version = pconfig.version;
        if client_version < pconfig.min_compatible_version {
            eyre::bail!(
                "Your client version ({client_version}) is no longer compatible. \
                 Please upgrade to {program_version} before continuing to use the client."
            )
        } else if client_version < program_version {
            writeln!(
                out,
                "A new version of the client is available: {client_version} → {program_version}\n\
                 We recommend upgrading to the latest version for the best experience."
            )?;
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
        contract_min_compatible_version: ProgramVersion,
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
                    min_compatible_version: contract_min_compatible_version.clone(),
                };
                Ok((Pubkey::new_unique(), program_config))
            });

        check_version(&client, out, client_version)
    }

    #[test]
    fn test_check_version_same_versions_ok() {
        let mut output = Vec::new();
        let res = test_check_version(
            &mut output,
            ProgramVersion::new(1, 0, 0), // program version
            ProgramVersion::new(1, 0, 0), // min compatible
            ProgramVersion::new(1, 0, 0), // client
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "");
    }

    #[test]
    fn test_check_version_program_higher_patch_warning() {
        // client == min_compatible < program → warning
        let mut output = Vec::new();
        let res = test_check_version(
            &mut output,
            ProgramVersion::new(1, 0, 1), // program version
            ProgramVersion::new(1, 0, 0), // min compatible
            ProgramVersion::new(1, 0, 0), // client
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,
            "A new version of the client is available: 1.0.0 → 1.0.1\n\
We recommend upgrading to the latest version for the best experience.\n"
        );
    }

    #[test]
    fn test_check_version_program_higher_minor_warning() {
        // client == min_compatible < program → warning
        let mut output = Vec::new();
        let res = test_check_version(
            &mut output,
            ProgramVersion::new(1, 2, 0), // program version
            ProgramVersion::new(1, 0, 0), // min compatible
            ProgramVersion::new(1, 0, 0), // client
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,
            "A new version of the client is available: 1.0.0 → 1.2.0\n\
We recommend upgrading to the latest version for the best experience.\n"
        );
    }

    #[test]
    fn test_check_version_client_higher_minor_ok() {
        // client > program >= min_compatible → no warning, no error
        let mut output = Vec::new();
        let res = test_check_version(
            &mut output,
            ProgramVersion::new(1, 1, 0), // program version
            ProgramVersion::new(1, 0, 0), // min compatible
            ProgramVersion::new(1, 2, 0), // client
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "");
    }

    #[test]
    fn test_check_version_client_higher_patch_ok() {
        // client > program >= min_compatible → no warning, no error
        let mut output = Vec::new();
        let res = test_check_version(
            &mut output,
            ProgramVersion::new(1, 2, 0), // program version
            ProgramVersion::new(1, 2, 0), // min compatible
            ProgramVersion::new(1, 2, 1), // client
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "");
    }

    #[test]
    fn test_check_version_client_below_min_compatible_error() {
        // client < min_compatible <= program → error
        let mut output = Vec::new();
        let res = test_check_version(
            &mut output,
            ProgramVersion::new(2, 0, 0), // program version
            ProgramVersion::new(1, 1, 0), // min compatible
            ProgramVersion::new(1, 0, 0), // client
        );
        assert!(res.is_err());
        let error_msg = res.unwrap_err().to_string();
        assert_eq!(
            error_msg,
            "Your client version (1.0.0) is no longer compatible. \
Please upgrade to 2.0.0 before continuing to use the client."
        );
    }

    #[test]
    fn test_check_version_client_much_older_major_error() {
        // client << min_compatible == program → error
        let mut output = Vec::new();
        let res = test_check_version(
            &mut output,
            ProgramVersion::new(2, 0, 0), // program version
            ProgramVersion::new(2, 0, 0), // min compatible
            ProgramVersion::new(1, 0, 0), // client
        );
        assert!(res.is_err());
        let error_msg = res.unwrap_err().to_string();
        assert_eq!(
            error_msg,
            "Your client version (1.0.0) is no longer compatible. \
Please upgrade to 2.0.0 before continuing to use the client."
        );
    }
}
