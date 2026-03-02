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
struct ConfigDisplay {
    pub account: String,
    pub code: String,
    pub vrf_id: u16,
    pub metro_routing: bool,
    pub route_liveness: bool,
    pub reference_count: u32,
    pub owner: String,
}

#[derive(Tabled, Serialize)]
struct AdminDisplay {
    pub administrator: String,
}

impl GetTenantCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (pubkey, tenant) = client.get_tenant(GetTenantCommand {
            pubkey_or_code: self.code.clone(),
        })?;

        let config = ConfigDisplay {
            account: pubkey.to_string(),
            code: tenant.code.clone(),
            vrf_id: tenant.vrf_id,
            metro_routing: tenant.metro_routing,
            route_liveness: tenant.route_liveness,
            reference_count: tenant.reference_count,
            owner: tenant.owner.to_string(),
        };
        let admin_rows: Vec<AdminDisplay> = tenant
            .administrators
            .iter()
            .map(|a| AdminDisplay {
                administrator: a.to_string(),
            })
            .collect();

        if self.json {
            #[derive(Serialize)]
            struct Output {
                config: ConfigDisplay,
                administrators: Vec<AdminDisplay>,
            }
            let output = Output {
                config,
                administrators: admin_rows,
            };
            let json = serde_json::to_string_pretty(&output)?;
            writeln!(out, "{}", json)?;
        } else {
            let table = tabled::Table::new([config]);
            writeln!(out, "{}", table)?;
            if !admin_rows.is_empty() {
                let admin_table = tabled::Table::new(admin_rows);
                writeln!(out, "\n{}", admin_table)?;
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

        let tenant_with_admins = Tenant {
            administrators: vec![Pubkey::from_str_const(
                "FposHWrkvPP3VErBAWCd4ELWGuh2mgx2Wx6cuNEA4X2S",
            )],
            ..tenant.clone()
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
        let tenant_with_admins_cloned = tenant_with_admins.clone();
        client
            .expect_get_tenant()
            .with(predicate::eq(GetTenantCommand {
                pubkey_or_code: "test-tenant-admin".to_string(),
            }))
            .returning(move |_| Ok((tenant_pubkey, tenant_with_admins_cloned.clone())));
        client
            .expect_get_tenant()
            .returning(move |_| Err(eyre::eyre!("not found")));

        // Expected failure
        let mut output = Vec::new();
        let res = GetTenantCliCommand {
            code: Pubkey::new_unique().to_string(),
            json: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_err(), "I shouldn't find anything.");

        // Expected success by pubkey (no admins, table output)
        let mut output = Vec::new();
        let res = GetTenantCliCommand {
            code: tenant_pubkey.to_string(),
            json: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by pubkey");
        let output_str = String::from_utf8(output).unwrap();
        assert!(
            output_str.contains("account"),
            "Should contain table header"
        );
        assert!(
            output_str.contains("test-tenant"),
            "Should contain tenant code"
        );
        assert!(
            !output_str.contains("administrator"),
            "Should not contain admin table"
        );

        // Expected success by code (no admins, JSON output)
        let mut output = Vec::new();
        let res = GetTenantCliCommand {
            code: "test-tenant".to_string(),
            json: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by code");
        let output_str = String::from_utf8(output).unwrap();
        assert!(
            output_str.contains("\"config\""),
            "Should contain config key in JSON"
        );
        assert!(
            output_str.contains("\"administrators\""),
            "Should contain administrators key in JSON"
        );
        assert!(
            output_str.contains("test-tenant"),
            "Should contain tenant code in JSON"
        );

        // Expected success with admins (table output)
        let mut output = Vec::new();
        let res = GetTenantCliCommand {
            code: "test-tenant-admin".to_string(),
            json: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by code with admins");
        let output_str = String::from_utf8(output).unwrap();
        assert!(
            output_str.contains("administrator"),
            "Should contain admin table header"
        );
        assert!(
            output_str.contains("FposHWrkvPP3VErBAWCd4ELWGuh2mgx2Wx6cuNEA4X2S"),
            "Should contain admin pubkey"
        );

        // Expected success with admins (JSON output)
        let mut output = Vec::new();
        let res = GetTenantCliCommand {
            code: "test-tenant-admin".to_string(),
            json: true,
        }
        .execute(&client, &mut output);
        assert!(
            res.is_ok(),
            "I should find a item by code with admins (json)"
        );
        let output_str = String::from_utf8(output).unwrap();
        assert!(
            output_str.contains("\"administrator\""),
            "Should contain admin key in JSON"
        );
        assert!(
            output_str.contains("FposHWrkvPP3VErBAWCd4ELWGuh2mgx2Wx6cuNEA4X2S"),
            "Should contain admin pubkey in JSON"
        );
    }
}
