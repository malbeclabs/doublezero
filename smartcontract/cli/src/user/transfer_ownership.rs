use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::validate_pubkey,
};
use clap::Args;
use doublezero_sdk::commands::user::transfer_ownership::TransferUserOwnershipCommand;
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, net::Ipv4Addr, str::FromStr};

#[derive(Args, Debug)]
pub struct TransferUserOwnershipCliCommand {
    /// User account pubkey
    #[arg(long, value_parser = validate_pubkey)]
    pub user_pubkey: String,
    /// Client IP of the user
    #[arg(long)]
    pub client_ip: Ipv4Addr,
    /// Current owner's payer pubkey (old access pass user_payer)
    #[arg(long, value_parser = validate_pubkey)]
    pub old_user_payer: String,
    /// New owner's payer pubkey (new access pass user_payer)
    #[arg(long, value_parser = validate_pubkey)]
    pub new_user_payer: String,
}

impl TransferUserOwnershipCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let signature = client.transfer_user_ownership(TransferUserOwnershipCommand {
            user_pubkey: Pubkey::from_str(&self.user_pubkey)?,
            client_ip: self.client_ip,
            old_user_payer: Pubkey::from_str(&self.old_user_payer)?,
            new_user_payer: Pubkey::from_str(&self.new_user_payer)?,
        })?;
        writeln!(out, "Signature: {signature}")?;

        Ok(())
    }
}
