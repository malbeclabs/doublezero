use crate::client::GeoCliCommand;
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_sdk::geolocation::programconfig::init::InitProgramConfigCommand;
use std::io::Write;

#[derive(Args, Debug)]
pub struct InitProgramConfigCliCommand {
    /// Skip confirmation prompt
    #[arg(long, default_value_t = false)]
    pub yes: bool,
}

impl InitProgramConfigCliCommand {
    pub async fn execute<C: GeoCliCommand, W: Write>(
        self,
        ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        tracing::debug!(env = %ctx.env, "geolocation init");

        if !self.yes {
            eprint!("Initialize geolocation program config? This is a one-time operation. [y/N]: ");
            let mut input = String::new();
            std::io::stdin().read_line(&mut input)?;
            if !input.trim().eq_ignore_ascii_case("y") {
                writeln!(out, "Aborted.")?;
                return Ok(());
            }
        }

        let (sig, pda) = client.init_program_config(InitProgramConfigCommand {})?;

        writeln!(out, "Signature: {sig}\nAccount: {pda}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::client::MockGeoCliCommand;
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_geo_init_program_config() {
        let mut client = MockGeoCliCommand::new();

        let config_pda = Pubkey::from_str_const("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB");
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        client
            .expect_init_program_config()
            .with(predicate::eq(InitProgramConfigCommand {}))
            .returning(move |_| Ok((signature, config_pda)));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res =
            block_on(InitProgramConfigCliCommand { yes: true }.execute(&ctx, &client, &mut output));
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("Signature:"));
        assert!(output_str.contains(&config_pda.to_string()));
    }
}
