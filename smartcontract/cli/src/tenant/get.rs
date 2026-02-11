use crate::{doublezerocommand::CliCommand, validators::validate_pubkey_or_code};
use clap::Args;
use doublezero_sdk::commands::tenant::get::GetTenantCommand;
use std::io::Write;

#[derive(Args, Debug)]
pub struct GetTenantCliCommand {
    /// Tenant pubkey or code
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub code: String,
}

impl GetTenantCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (pubkey, tenant) = client.get_tenant(GetTenantCommand {
            pubkey_or_code: self.code,
        })?;

        writeln!(out, "account: {pubkey}")?;
        writeln!(out, "code: {}", tenant.code)?;
        writeln!(out, "vrf_id: {}", tenant.vrf_id)?;
        writeln!(out, "metro_route: {}", tenant.metro_route)?;
        writeln!(out, "route_liveness: {}", tenant.route_liveness)?;
        writeln!(out, "reference_count: {}", tenant.reference_count)?;
        writeln!(out, "owner: {}", tenant.owner)?;

        Ok(())
    }
}
