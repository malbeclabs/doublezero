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

        let fields = [
            ("account", pubkey.to_string()),
            ("code", tenant.code.clone()),
            ("vrf_id", tenant.vrf_id.to_string()),
            ("metro_routing", tenant.metro_routing.to_string()),
            ("route_liveness", tenant.route_liveness.to_string()),
            ("reference_count", tenant.reference_count.to_string()),
            ("owner", tenant.owner.to_string()),
        ];
        let max_len = fields.iter().map(|(k, _)| k.len()).max().unwrap_or(0);
        for (key, value) in &fields {
            writeln!(out, " {key:<max_len$} | {value}")?;
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
        }
        .execute(&client, &mut output);
        assert!(res.is_err(), "I shouldn't find anything.");

        // Expected success by pubkey
        let mut output = Vec::new();
        let res = GetTenantCliCommand {
            code: tenant_pubkey.to_string(),
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
        assert!(
            has_row("code", "test-tenant"),
            "code row should contain value"
        );
        assert!(has_row("vrf_id", "100"), "vrf_id row should contain value");

        // Expected success by code
        let mut output = Vec::new();
        let res = GetTenantCliCommand {
            code: "test-tenant".to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by code");
    }
}
