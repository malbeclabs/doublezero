use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::validate_pubkey_or_code,
};
use clap::Args;
use doublezero_sdk::commands::tenant::{
    add_administrator::AddAdministratorTenantCommand, get::GetTenantCommand,
};
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, str::FromStr};

#[derive(Args, Debug)]
pub struct AddAdministratorTenantCliCommand {
    /// Tenant pubkey or code
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub pubkey: String,
    /// Administrator pubkey to add
    #[arg(long)]
    pub administrator: String,
}

impl AddAdministratorTenantCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let (tenant_pubkey, _tenant) = client.get_tenant(GetTenantCommand {
            pubkey_or_code: self.pubkey,
        })?;

        let administrator = Pubkey::from_str(&self.administrator)
            .map_err(|_| eyre::eyre!("Invalid administrator pubkey"))?;

        let signature = client.add_administrator_tenant(AddAdministratorTenantCommand {
            tenant_pubkey,
            administrator,
        })?;

        writeln!(out, "Signature: {signature}")?;

        Ok(())
    }
}
