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

#[cfg(test)]
mod tests {
    use crate::{tenant::get::GetTenantCliCommand, tests::utils::create_test_client};
    use doublezero_sdk::{commands::tenant::get::GetTenantCommand, AccountType};
    use doublezero_serviceability::state::tenant::{Tenant, TenantPaymentStatus};
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
            metro_route: true,
            route_aliveness: false,
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
        assert_eq!(
            output_str,
            "account: BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB\ncode: test-tenant\nvrf_id: 100\nmetro_route: true\nroute_aliveness: false\nowner: BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB\n"
        );

        // Expected success by code
        let mut output = Vec::new();
        let res = GetTenantCliCommand {
            code: "test-tenant".to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by code");
    }
}
