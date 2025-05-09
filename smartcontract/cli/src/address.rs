use crate::doublezerocommand::CliCommand;
use crate::requirements::{check_requirements, CHECK_ID_JSON};
use clap::Args;
use doublezero_sdk::get_doublezero_pubkey;
use solana_sdk::signer::Signer;
use std::io::Write;

#[derive(Args, Debug)]
pub struct AddressCliCommand {}

impl AddressCliCommand {
    pub fn execute<W: Write>(self, client: &dyn CliCommand, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON)?;

        match get_doublezero_pubkey() {
            Some(pubkey) => {
                writeln!(out, "{}", pubkey.pubkey())?;
            }
            None => {
                writeln!(out, "Unable to read the Pubkey")?;
            }
        }
        Ok(())
    }
}
