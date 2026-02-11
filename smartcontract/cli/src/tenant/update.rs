use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::validate_pubkey_or_code,
};
use clap::Args;
use doublezero_sdk::commands::tenant::{get::GetTenantCommand, update::UpdateTenantCommand};
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, str::FromStr};

#[derive(Args, Debug)]
pub struct UpdateTenantCliCommand {
    /// Tenant pubkey or code
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub pubkey: String,
    /// Updated VRF ID
    #[arg(long)]
    pub vrf_id: Option<u16>,
    /// Solana 2Z token account to monitor for billing
    #[arg(long)]
    pub token_account: Option<String>,
    /// Enable/disable metro routing
    #[arg(long)]
    pub metro_route: Option<bool>,
    /// Enable/disable route aliveness checks
    #[arg(long)]
    pub route_aliveness: Option<bool>,
}

impl UpdateTenantCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let (tenant_pubkey, _tenant) = client.get_tenant(GetTenantCommand {
            pubkey_or_code: self.pubkey,
        })?;

        let token_account = self
            .token_account
            .map(|s| Pubkey::from_str(&s))
            .transpose()?;

        let signature = client.update_tenant(UpdateTenantCommand {
            tenant_pubkey,
            vrf_id: self.vrf_id,
            token_account,
            metro_route: self.metro_route,
            route_aliveness: self.route_aliveness,
        })?;

        writeln!(out, "Signature: {signature}")?;

        Ok(())
    }
}
