use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::commands::tenant::list::ListTenantCommand;
use std::io::Write;

#[derive(Args, Debug)]
pub struct ListTenantCliCommand {
    /// Output as JSON
    #[arg(long)]
    pub json: bool,
    /// Output as compact JSON
    #[arg(long)]
    pub json_compact: bool,
}

impl ListTenantCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let tenants = client.list_tenant(ListTenantCommand {})?;

        if self.json {
            writeln!(out, "{}", serde_json::to_string_pretty(&tenants)?)?;
        } else if self.json_compact {
            writeln!(out, "{}", serde_json::to_string(&tenants)?)?;
        } else {
            // Sort by owner
            let mut tenants: Vec<_> = tenants.iter().collect();
            tenants.sort_by_key(|(_, tenant)| tenant.owner);

            writeln!(
                out,
                "{:<45} {:<33} {:<8} {:<45}",
                "account", "code", "vrf_id", "owner"
            )?;
            for (pubkey, tenant) in tenants {
                writeln!(
                    out,
                    "{:<45} {:<33} {:<8} {:<45}",
                    pubkey, tenant.code, tenant.vrf_id, tenant.owner
                )?;
            }
        }

        Ok(())
    }
}
