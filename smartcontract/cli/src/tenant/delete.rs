use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::validate_pubkey_or_code,
};
use clap::Args;
use doublezero_sdk::commands::tenant::{delete::DeleteTenantCommand, get::GetTenantCommand};
use std::io::Write;

#[derive(Args, Debug)]
pub struct DeleteTenantCliCommand {
    /// Tenant pubkey or code
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub pubkey: String,

    /// Delete all users in the tenant and close related access passes before deleting
    #[arg(long, default_value_t = false)]
    pub allow_delete_users: bool,
}

impl DeleteTenantCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let (tenant_pubkey, tenant) = client.get_tenant(GetTenantCommand {
            pubkey_or_code: self.pubkey,
        })?;

        if tenant.reference_count > 0 && !self.allow_delete_users {
            return Err(eyre::eyre!(
                "Cannot delete tenant with reference_count > 0 (current: {}). Use --allow-delete-users to cascade delete.",
                tenant.reference_count
            ));
        }

        let signature = client.delete_tenant(DeleteTenantCommand {
            tenant_pubkey,
            allow_delete_users: self.allow_delete_users,
        })?;

        writeln!(out, "Signature: {signature}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tenant::delete::DeleteTenantCliCommand,
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{
        commands::tenant::{delete::DeleteTenantCommand, get::GetTenantCommand},
        AccountType,
    };
    use doublezero_serviceability::state::tenant::{
        Tenant, TenantBillingConfig, TenantPaymentStatus,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_tenant_delete() {
        let mut client = create_test_client();

        let tenant_pubkey = Pubkey::new_unique();
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let tenant = Tenant {
            account_type: AccountType::Tenant,
            owner: Pubkey::default(),
            bump_seed: 0,
            code: "test".to_string(),
            vrf_id: 100,
            reference_count: 0,
            administrators: vec![],
            token_account: Pubkey::default(),
            payment_status: TenantPaymentStatus::Paid,
            metro_routing: false,
            route_liveness: false,
            billing: TenantBillingConfig::default(),
        };

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        let tenant_cloned = tenant.clone();
        client
            .expect_get_tenant()
            .with(predicate::eq(GetTenantCommand {
                pubkey_or_code: tenant_pubkey.to_string(),
            }))
            .returning(move |_| Ok((tenant_pubkey, tenant_cloned.clone())));
        client
            .expect_delete_tenant()
            .with(predicate::eq(DeleteTenantCommand {
                tenant_pubkey,
                allow_delete_users: false,
            }))
            .returning(move |_| Ok(signature));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let res = DeleteTenantCliCommand {
            pubkey: tenant_pubkey.to_string(),
            allow_delete_users: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,
            "Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }

    #[test]
    fn test_cli_tenant_delete_with_references() {
        let mut client = create_test_client();

        let tenant_pubkey = Pubkey::new_unique();

        let tenant = Tenant {
            account_type: AccountType::Tenant,
            owner: Pubkey::default(),
            bump_seed: 0,
            code: "test".to_string(),
            vrf_id: 100,
            reference_count: 5,
            administrators: vec![],
            token_account: Pubkey::default(),
            payment_status: TenantPaymentStatus::Paid,
            metro_routing: false,
            route_liveness: false,
            billing: TenantBillingConfig::default(),
        };

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_tenant()
            .with(predicate::eq(GetTenantCommand {
                pubkey_or_code: tenant_pubkey.to_string(),
            }))
            .returning(move |_| Ok((tenant_pubkey, tenant.clone())));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let res = DeleteTenantCliCommand {
            pubkey: tenant_pubkey.to_string(),
            allow_delete_users: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_err());
    }

    #[test]
    fn test_cli_tenant_delete_with_references_and_allow_delete_users() {
        let mut client = create_test_client();

        let tenant_pubkey = Pubkey::new_unique();
        let signature = Signature::new_unique();

        let tenant = Tenant {
            account_type: AccountType::Tenant,
            owner: Pubkey::default(),
            bump_seed: 0,
            code: "test".to_string(),
            vrf_id: 100,
            reference_count: 2,
            administrators: vec![],
            token_account: Pubkey::default(),
            payment_status: TenantPaymentStatus::Paid,
            metro_routing: false,
            route_liveness: false,
            billing: TenantBillingConfig::default(),
        };

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        let tenant_cloned = tenant.clone();
        client
            .expect_get_tenant()
            .with(predicate::eq(GetTenantCommand {
                pubkey_or_code: tenant_pubkey.to_string(),
            }))
            .returning(move |_| Ok((tenant_pubkey, tenant_cloned.clone())));
        client
            .expect_delete_tenant()
            .with(predicate::eq(DeleteTenantCommand {
                tenant_pubkey,
                allow_delete_users: true,
            }))
            .returning(move |_| Ok(signature));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let res = DeleteTenantCliCommand {
            pubkey: tenant_pubkey.to_string(),
            allow_delete_users: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
    }
}
