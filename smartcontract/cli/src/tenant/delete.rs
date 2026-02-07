use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::validate_pubkey_or_code,
};
use clap::Args;
use doublezero_sdk::commands::tenant::{delete::DeleteTenantCommand, get::GetTenantCommand};
use std::io::Write;

#[derive(Args, Debug)]
pub struct DeleteTenantCliCommand {
    /// Tenant pubkey or code
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub pubkey: String,
}

impl DeleteTenantCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let (tenant_pubkey, tenant) = client.get_tenant(GetTenantCommand {
            pubkey_or_code: self.pubkey,
        })?;

        if tenant.reference_count > 0 {
            return Err(eyre::eyre!(
                "Cannot delete tenant with reference_count > 0 (current: {})",
                tenant.reference_count
            ));
        }

        let signature = client.delete_tenant(DeleteTenantCommand { tenant_pubkey })?;

        writeln!(out, "Signature: {signature}")?;

        Ok(())
    }
}
