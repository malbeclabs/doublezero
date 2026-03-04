use crate::{doublezerocommand::CliCommand, validators::validate_pubkey_or_code};
use clap::Args;
use doublezero_program_common::serializer;
use doublezero_sdk::commands::tenant::get::GetTenantCommand;
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use tabled::{settings::Style, Table, Tabled};

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
struct AdministratorDisplay {
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    #[tabled(rename = "administrator")]
    pub pubkey: Pubkey,
}

#[derive(Tabled, Serialize)]
struct TenantDisplay {
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub code: String,
    pub vrf_id: u16,
    pub metro_routing: bool,
    pub route_liveness: bool,
    pub reference_count: u32,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey,
    #[tabled(skip)]
    pub administrators: Vec<AdministratorDisplay>,
}

impl GetTenantCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (pubkey, tenant) = client.get_tenant(GetTenantCommand {
            pubkey_or_code: self.code,
        })?;

        let display = TenantDisplay {
            account: pubkey,
            code: tenant.code,
            vrf_id: tenant.vrf_id,
            metro_routing: tenant.metro_routing,
            route_liveness: tenant.route_liveness,
            reference_count: tenant.reference_count,
            owner: tenant.owner,
            administrators: tenant
                .administrators
                .into_iter()
                .map(|pk| AdministratorDisplay { pubkey: pk })
                .collect(),
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
            if !display.administrators.is_empty() {
                writeln!(out)?;
                let table = Table::new(&display.administrators)
                    .with(Style::psql().remove_horizontals())
                    .to_string();
                writeln!(out, "{table}")?;
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

    fn make_tenant(pubkey: Pubkey, administrators: Vec<Pubkey>) -> Tenant {
        Tenant {
            account_type: AccountType::Tenant,
            owner: pubkey,
            bump_seed: 0,
            code: "test-tenant".to_string(),
            vrf_id: 100,
            reference_count: 0,
            administrators,
            token_account: Pubkey::default(),
            payment_status: TenantPaymentStatus::Paid,
            metro_routing: true,
            route_liveness: false,
            billing: TenantBillingConfig::default(),
        }
    }

    #[test]
    fn test_cli_tenant_get_not_found() {
        let mut client = create_test_client();
        client
            .expect_get_tenant()
            .returning(|_| Err(eyre::eyre!("not found")));

        let mut output = Vec::new();
        let res = GetTenantCliCommand {
            code: Pubkey::new_unique().to_string(),
            json: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_err());
    }

    #[test]
    fn test_cli_tenant_get_table() {
        let mut client = create_test_client();
        let tenant_pubkey = Pubkey::from_str_const("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB");
        let admin_pubkey = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let tenant = make_tenant(tenant_pubkey, vec![admin_pubkey]);

        client
            .expect_get_tenant()
            .with(predicate::eq(GetTenantCommand {
                pubkey_or_code: tenant_pubkey.to_string(),
            }))
            .returning(move |_| Ok((tenant_pubkey, tenant.clone())));

        let mut output = Vec::new();
        GetTenantCliCommand {
            code: tenant_pubkey.to_string(),
            json: false,
        }
        .execute(&client, &mut output)
        .unwrap();

        let output_str = String::from_utf8(output).unwrap();
        let has_row = |header: &str, value: &str| {
            output_str
                .lines()
                .any(|l| l.contains(header) && l.contains(value))
        };
        assert!(has_row("account", &tenant_pubkey.to_string()));
        assert!(has_row("code", "test-tenant"));
        assert!(has_row("vrf_id", "100"));
        assert!(has_row("metro_routing", "true"));
        assert!(has_row("route_liveness", "false"));
        assert!(has_row("owner", &tenant_pubkey.to_string()));
        assert!(
            output_str.contains(&admin_pubkey.to_string()),
            "administrators table should contain admin pubkey"
        );
    }

    #[test]
    fn test_cli_tenant_get_table_no_administrators() {
        let mut client = create_test_client();
        let tenant_pubkey = Pubkey::from_str_const("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB");
        let tenant = make_tenant(tenant_pubkey, vec![]);

        client
            .expect_get_tenant()
            .returning(move |_| Ok((tenant_pubkey, tenant.clone())));

        let mut output = Vec::new();
        GetTenantCliCommand {
            code: tenant_pubkey.to_string(),
            json: false,
        }
        .execute(&client, &mut output)
        .unwrap();

        let output_str = String::from_utf8(output).unwrap();
        assert!(
            !output_str.contains("administrator"),
            "administrators table should not appear when list is empty"
        );
    }

    #[test]
    fn test_cli_tenant_get_json() {
        let mut client = create_test_client();
        let tenant_pubkey = Pubkey::from_str_const("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB");
        let admin_pubkey = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let tenant = make_tenant(tenant_pubkey, vec![admin_pubkey]);

        client
            .expect_get_tenant()
            .with(predicate::eq(GetTenantCommand {
                pubkey_or_code: "test-tenant".to_string(),
            }))
            .returning(move |_| Ok((tenant_pubkey, tenant.clone())));

        let mut output = Vec::new();
        GetTenantCliCommand {
            code: "test-tenant".to_string(),
            json: true,
        }
        .execute(&client, &mut output)
        .unwrap();

        let json: serde_json::Value =
            serde_json::from_str(&String::from_utf8(output).unwrap()).unwrap();
        assert_eq!(json["account"].as_str().unwrap(), tenant_pubkey.to_string());
        assert_eq!(json["code"].as_str().unwrap(), "test-tenant");
        assert_eq!(json["vrf_id"].as_u64().unwrap(), 100);
        assert!(json["metro_routing"].as_bool().unwrap());
        assert!(!json["route_liveness"].as_bool().unwrap());
        assert_eq!(json["reference_count"].as_u64().unwrap(), 0);
        assert_eq!(json["owner"].as_str().unwrap(), tenant_pubkey.to_string());
        let admins = json["administrators"].as_array().unwrap();
        assert_eq!(admins.len(), 1);
        assert_eq!(admins[0]["pubkey"].as_str().unwrap(), admin_pubkey.to_string());
    }
}
