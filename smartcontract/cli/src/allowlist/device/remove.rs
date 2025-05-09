use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::commands::allowlist::device::remove::RemoveDeviceAllowlistCommand;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use std::str::FromStr;

use crate::requirements::{ CHECK_BALANCE, CHECK_ID_JSON};

#[derive(Args, Debug)]
pub struct RemoveDeviceAllowlistCliCommand {
    #[arg(long)]
    pub pubkey: String,
}

impl RemoveDeviceAllowlistCliCommand {
    pub fn execute<W: Write>(self, client: &dyn CliCommand, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let pubkey = {
            if self.pubkey.eq_ignore_ascii_case("me") {
                client.get_payer()
            } else {
                Pubkey::from_str(&self.pubkey)?
            }
        };

        let res = client.remove_device_allowlist(RemoveDeviceAllowlistCommand { pubkey })?;
        writeln!(out, "Signature: {}", res)?;

        Ok(())
    }
}
