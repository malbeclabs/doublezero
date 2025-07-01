use crate::{doublezerocommand::CliCommand, requirements::CHECK_ID_JSON};
use clap::Args;
use doublezero_sdk::get_doublezero_pubkey;
use solana_sdk::signer::Signer;
use std::io::Write;

#[derive(Args, Debug)]
pub struct AddressCliCommand;

impl AddressCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON)?;

        match get_doublezero_pubkey() {
            Err(_) => {
                writeln!(out, "Unable to read the Pubkey")?;
            }
            Ok(pubkey) => {
                writeln!(out, "{}", pubkey.pubkey())?;
            }
        }
        Ok(())
    }
}
