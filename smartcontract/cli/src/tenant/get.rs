use crate::{doublezerocommand::CliCommand, validators::validate_pubkey_or_code};
use clap::Args;
use doublezero_sdk::commands::tenant::get::GetTenantCommand;
use serde::Serialize;
use std::io::Write;
use tabled::Tabled;

#[derive(Args, Debug)]
pub struct GetTenantCliCommand {
    /// Tenant pubkey or code
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub code: String,
    /// Output as JSON
    #[arg(long)]
    pub json: bool,
}

#[derive(Tabled, Serialize)]
struct TenantDisplay {
    pub account: String,
    pub code: String,
    pub vrf_id: u16,
    pub metro_routing: bool,
    pub route_liveness: bool,
    pub reference_count: u32,
    pub owner: String,
}

impl GetTenantCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (pubkey, tenant) = client.get_tenant(GetTenantCommand {
            pubkey_or_code: self.code,
        })?;

        let display = TenantDisplay {
            account: pubkey.to_string(),
            code: tenant.code,
            vrf_id: tenant.vrf_id,
            metro_routing: tenant.metro_routing,
            route_liveness: tenant.route_liveness,
            reference_count: tenant.reference_count,
            owner: tenant.owner.to_string(),
        };

        if self.json {
            let json = serde_json::to_string_pretty(&display)?;
            writeln!(out, "{json}")?;
        } else {
            let headers = TenantDisplay::headers();
            let fields = display.fields();
            let max_len = headers.iter().map(|h| h.len()).max().unwrap_or(0);
            for (header, value) in headers.iter().zip(fields.iter()) {
                writeln!(out, " {header:<max_len$} | {value}")?;
            }
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{tenant::get::GetTenantCliCommand, tests::utils::create_test_client};
    use doublezero_sdk::{commands::tenant::get::GetTenantCommand, AccountType};
    use doublezero_serviceability::state::tenant::{
        Tenant, TenantBillingConfig, TenantPaymentStatus,
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_cli_tenant_get() {
        let mut client = create_test_client();

        let tenant_pubkey = Pubkey::from_str_const("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB");
        let tenant = Tenant {
            account_type: AccountType::Tenant,
            owner: tenant_pubkey,
            bump_seed: 0,
            code: "test-tenant".to_string(),
            vrf_id: 100,
            reference_count: 0,
            administrators: vec![],
            token_account: Pubkey::default(),
            payment_status: TenantPaymentStatus::Paid,
            metro_routing: true,
            route_liveness: false,
            billing: TenantBillingConfig::default(),
        };

        let tenant_cloned = tenant.clone();
        client
            .expect_get_tenant()
            .with(predicate::eq(GetTenantCommand {
                pubkey_or_code: tenant_pubkey.to_string(),
            }))
            .returning(move |_| Ok((tenant_pubkey, tenant_cloned.clone())));
        let tenant_cloned2 = tenant.clone();
        client
            .expect_get_tenant()
            .with(predicate::eq(GetTenantCommand {
                pubkey_or_code: "test-tenant".to_string(),
            }))
            .returning(move |_| Ok((tenant_pubkey, tenant_cloned2.clone())));
        client
            .expect_get_tenant()
            .returning(move |_| Err(eyre::eyre!("not found")));

        /*****************************************************************************************************/
        // Expected failure
        let mut output = Vec::new();
        let res = GetTenantCliCommand {
            code: Pubkey::new_unique().to_string(),
            json: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_err(), "I shouldn't find anything.");

        // Expected success by pubkey (table)
        let mut output = Vec::new();
        let res = GetTenantCliCommand {
            code: tenant_pubkey.to_string(),
            json: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by pubkey");
        let output_str = String::from_utf8(output).unwrap();
        let has_row = |header: &str, value: &str| {
            output_str
                .lines()
                .any(|l| l.contains(header) && l.contains(value))
        };
        assert!(
            has_row("account", &tenant_pubkey.to_string()),
            "account row should contain pubkey"
        );
        assert!(has_row("code", "test-tenant"), "code row should contain value");
        assert!(has_row("vrf_id", "100"), "vrf_id row should contain value");
        assert!(
            has_row("metro_routing", "true"),
            "metro_routing row should contain value"
        );
        assert!(
            has_row("route_liveness", "false"),
            "route_liveness row should contain value"
        );

        // Expected success by code (JSON)
        let mut output = Vec::new();
        let res = GetTenantCliCommand {
            code: "test-tenant".to_string(),
            json: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by code");
        let json: serde_json::Value =
            serde_json::from_str(&String::from_utf8(output).unwrap()).unwrap();
        assert_eq!(
            json["account"].as_str().unwrap(),
            tenant_pubkey.to_string()
        );
        assert_eq!(json["code"].as_str().unwrap(), "test-tenant");
        assert_eq!(json["vrf_id"].as_u64().unwrap(), 100);
        assert_eq!(json["metro_routing"].as_bool().unwrap(), true);
        assert_eq!(json["route_liveness"].as_bool().unwrap(), false);
        assert_eq!(json["reference_count"].as_u64().unwrap(), 0);
        assert_eq!(
            json["owner"].as_str().unwrap(),
            tenant_pubkey.to_string()
        );
    }
}
