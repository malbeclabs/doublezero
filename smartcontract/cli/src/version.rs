use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_sdk::commands::programconfig::get::GetProgramConfigCommand;
use std::io::Write;

#[derive(Args, Debug)]
pub struct VersionCliCommand;

impl VersionCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        tracing::debug!(env = %ctx.env, "version");

        writeln!(out, "client version:       {}", ctx.client_version)?;
        if let Ok((_, pconfig)) = client.get_program_config(GetProgramConfigCommand) {
            writeln!(out, "program version:      {}", pconfig.version)?;
            writeln!(
                out,
                "min required version: {}",
                pconfig.min_compatible_version
            )?;
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::tests::utils::create_test_client;
    use doublezero_cli_core::testing::{block_on, cli_context_for_tests};
    use doublezero_sdk::commands::programconfig::get::GetProgramConfigCommand;
    use doublezero_serviceability::{
        programversion::ProgramVersion,
        state::{accounttype::AccountType, programconfig::ProgramConfig},
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_version_displays_all_versions() {
        let mut client = create_test_client();

        let pconfig = ProgramConfig {
            account_type: AccountType::default(),
            bump_seed: 0,
            version: ProgramVersion {
                major: 1,
                minor: 2,
                patch: 3,
            },
            min_compatible_version: ProgramVersion {
                major: 0,
                minor: 5,
                patch: 0,
            },
        };
        let pk = Pubkey::new_unique();
        client
            .expect_get_program_config()
            .with(predicate::eq(GetProgramConfigCommand))
            .returning(move |_| Ok((pk, pconfig.clone())));

        let ctx = cli_context_for_tests()
            .with_client_version("0.24.0-SNAPSHOT-abc1234")
            .build()
            .unwrap();

        let mut output = Vec::new();
        let res = block_on(VersionCliCommand.execute(&ctx, &client, &mut output));
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("0.24.0-SNAPSHOT-abc1234"));
        assert!(output_str.contains("1.2.3"));
        assert!(output_str.contains("0.5.0"));
    }

    #[test]
    fn test_version_handles_program_config_error() {
        let mut client = create_test_client();
        client
            .expect_get_program_config()
            .returning(|_| Err(eyre::eyre!("unavailable")));

        let ctx = cli_context_for_tests()
            .with_client_version("0.24.0")
            .build()
            .unwrap();

        let mut output = Vec::new();
        let res = block_on(VersionCliCommand.execute(&ctx, &client, &mut output));
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("0.24.0"));
        assert!(!output_str.contains("program version"));
    }
}
