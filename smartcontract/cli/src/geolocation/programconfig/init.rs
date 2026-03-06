use crate::geoclicommand::GeoCliCommand;
use clap::Args;
use doublezero_sdk::geolocation::programconfig::init::InitProgramConfigCommand;
use std::io::Write;

#[derive(Args, Debug)]
pub struct InitProgramConfigCliCommand {}

impl InitProgramConfigCliCommand {
    pub fn execute<C: GeoCliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (sig, pda) = client.init_program_config(InitProgramConfigCommand {})?;

        writeln!(out, "Signature: {sig}\r\nAccount: {pda}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::geoclicommand::MockGeoCliCommand;
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

        let mut output = Vec::new();
        let res = InitProgramConfigCliCommand {}.execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("Signature:"));
        assert!(output_str.contains(&config_pda.to_string()));
    }
}
