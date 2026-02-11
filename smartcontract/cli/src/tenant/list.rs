use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_program_common::serializer;
use doublezero_sdk::commands::tenant::list::ListTenantCommand;
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use tabled::{settings::Style, Table, Tabled};

#[derive(Args, Debug)]
pub struct ListTenantCliCommand {
    /// Output as pretty JSON
    #[arg(long, default_value_t = false)]
    pub json: bool,
    /// Output as compact JSON
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
}

#[derive(Tabled, Serialize)]
pub struct TenantDisplay {
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub code: String,
    pub vrf_id: u16,
    pub metro_route: bool,
    pub route_aliveness: bool,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey,
}

impl ListTenantCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let tenants = client.list_tenant(ListTenantCommand {})?;

        let mut tenant_displays: Vec<TenantDisplay> = tenants
            .into_iter()
            .map(|(pubkey, tenant)| TenantDisplay {
                account: pubkey,
                code: tenant.code,
                vrf_id: tenant.vrf_id,
                metro_route: tenant.metro_route,
                route_aliveness: tenant.route_aliveness,
                owner: tenant.owner,
            })
            .collect();

        tenant_displays.sort_by(|a, b| a.code.cmp(&b.code));

        let res = if self.json {
            serde_json::to_string_pretty(&tenant_displays)?
        } else if self.json_compact {
            serde_json::to_string(&tenant_displays)?
        } else {
            Table::new(tenant_displays)
                .with(Style::psql().remove_horizontals())
                .to_string()
        };

        writeln!(out, "{res}")?;

        Ok(())
    }
}
