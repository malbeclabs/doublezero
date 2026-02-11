use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::{validate_code, validate_pubkey_or_code},
};
use clap::Args;
use doublezero_sdk::commands::tenant::{create::CreateTenantCommand, list::ListTenantCommand};
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, str::FromStr};

#[derive(Args, Debug)]
pub struct CreateTenantCliCommand {
    /// Unique tenant code
    #[arg(long, value_parser = validate_code)]
    pub code: String,
    /// Administrator of the tenant
    #[arg(long, value_parser = validate_pubkey_or_code, default_value = "me")]
    pub administrator: String,
    /// Solana 2Z token account to monitor for billing
    #[arg(long)]
    pub token_account: Option<String>,
    /// Enable metro routing for this tenant
    #[arg(long, default_value = "false")]
    pub metro_route: bool,
    /// Enable route aliveness checks for this tenant
    #[arg(long, default_value = "false")]
    pub route_liveness: bool,
}

impl CreateTenantCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let tenants = client.list_tenant(ListTenantCommand {})?;
        if tenants.iter().any(|(_, d)| d.code == self.code) {
            return Err(eyre::eyre!(
                "Tenant with code '{}' already exists",
                self.code
            ));
        }
        // Create tenant
        let administrator = {
            if self.administrator.eq_ignore_ascii_case("me") {
                client.get_payer()
            } else {
                Pubkey::from_str(&self.administrator)?
            }
        };

        let token_account = self
            .token_account
            .map(|s| Pubkey::from_str(&s))
            .transpose()?;

        let (signature, _pubkey) = client.create_tenant(CreateTenantCommand {
            code: self.code.clone(),
            administrator,
            token_account,
            metro_route: self.metro_route,
            route_liveness: self.route_liveness,
        })?;

        writeln!(out, "Signature: {signature}")?;

        Ok(())
    }
}
