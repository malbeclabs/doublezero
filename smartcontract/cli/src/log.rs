use crate::helpers::parse_pubkey;
use clap::Args;
use doublezero_sdk::DZClient;
use std::io::Write;

#[derive(Args, Debug)]
pub struct LogCliCommand {
    /// Public key of the user to get logs for
    #[arg(long)]
    pubkey: String,
}

impl LogCliCommand {
    pub fn execute<W: Write>(self, client: &DZClient, out: &mut W) -> eyre::Result<()> {
        let pubkey = parse_pubkey(&self.pubkey).expect("Invalid pubkey");

        for msg in client.get_logs(&pubkey)? {
            writeln!(out, "{msg}")?;
        }

        Ok(())
    }
}
