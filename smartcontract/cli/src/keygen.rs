use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::create_new_pubkey_user;
use solana_sdk::signer::Signer;
use std::{io::Write, path::PathBuf};

#[derive(Args, Debug)]
pub struct KeyGenCliCommand {
    /// Force keypair generation
    #[arg(short, default_value = "false")]
    force: bool,
    /// Path to generated file
    #[arg(short, long)]
    outfile: Option<PathBuf>,
}

impl KeyGenCliCommand {
    pub fn execute<W: Write>(self, _client: &dyn CliCommand, out: &mut W) -> eyre::Result<()> {
        match create_new_pubkey_user(self.force, self.outfile) {
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
