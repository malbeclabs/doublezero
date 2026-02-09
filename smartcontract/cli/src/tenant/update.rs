use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::validate_pubkey_or_code,
};
use clap::Args;
use doublezero_sdk::commands::tenant::{get::GetTenantCommand, update::UpdateTenantCommand};
use std::io::Write;

#[derive(Args, Debug)]
pub struct UpdateTenantCliCommand {
    /// Tenant pubkey or code
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub pubkey: String,
    /// Updated VRF ID
    #[arg(long)]
    pub vrf_id: Option<u16>,
}

impl UpdateTenantCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let (tenant_pubkey, _tenant) = client.get_tenant(GetTenantCommand {
            pubkey_or_code: self.pubkey,
        })?;

        let signature = client.update_tenant(UpdateTenantCommand {
            tenant_pubkey,
            vrf_id: self.vrf_id,
        })?;

        writeln!(out, "Signature: {signature}")?;

        Ok(())
    }
}
