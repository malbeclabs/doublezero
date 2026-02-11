use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::validate_pubkey_or_code,
};
use clap::Args;
use doublezero_sdk::commands::tenant::{
    get::GetTenantCommand, update_payment_status::UpdatePaymentStatusCommand,
};
use std::io::Write;

#[derive(Args, Debug)]
pub struct UpdatePaymentStatusCliCommand {
    /// Tenant pubkey or code
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub pubkey: String,
    /// Payment status (0=Unknown, 1=Paid, 2=Delinquent, 3=Suspended)
    #[arg(long)]
    pub payment_status: u8,
}

impl UpdatePaymentStatusCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let (tenant_pubkey, _tenant) = client.get_tenant(GetTenantCommand {
            pubkey_or_code: self.pubkey,
        })?;

        let signature = client.update_payment_status_tenant(UpdatePaymentStatusCommand {
            tenant_pubkey,
            payment_status: self.payment_status,
            last_deduction_dz_epoch: None,
        })?;

        writeln!(out, "Signature: {signature}")?;

        Ok(())
    }
}
