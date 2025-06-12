use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::create_new_pubkey_user;
use solana_sdk::signer::Signer;
use std::io::Write;

#[derive(Args, Debug)]
pub struct KeyGenCliCommand {
    #[arg(short, default_value = "false", help = "Force keypair generation")]
    force: bool,
}

impl KeyGenCliCommand {
    pub fn execute<W: Write>(self, _client: &dyn CliCommand, out: &mut W) -> eyre::Result<()> {
        match create_new_pubkey_user(self.force) {
            Ok(keypair) => {
                writeln!(out, "Pubkey: {}", keypair.pubkey())?;
            }
            Err(e) => {
                writeln!(out, "Error: {e}")?;
            }
        };

        Ok(())
    }
}
