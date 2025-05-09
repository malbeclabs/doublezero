use crate::doublezerocommand::CliCommand;
use crate::requirements::{ CHECK_BALANCE, CHECK_ID_JSON};
use clap::Args;
use doublezero_sdk::commands::allowlist::user::remove::RemoveUserAllowlistCommand;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use std::str::FromStr;

#[derive(Args, Debug)]
pub struct RemoveUserAllowlistCliCommand {
    #[arg(long)]
    pub pubkey: String,
}

impl RemoveUserAllowlistCliCommand {
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

        let res = client.remove_user_allowlist(RemoveUserAllowlistCommand { pubkey })?;
        writeln!(out, "Signature: {}", res)?;

        Ok(())
    }
}
