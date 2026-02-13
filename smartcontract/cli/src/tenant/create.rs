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
    pub metro_routing: bool,
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
            metro_routing: self.metro_routing,
            route_liveness: self.route_liveness,
        })?;

        writeln!(out, "Signature: {signature}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tenant::create::CreateTenantCliCommand,
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{
        commands::tenant::{create::CreateTenantCommand, list::ListTenantCommand},
        AccountType,
    };
    use doublezero_serviceability::state::tenant::{
        Tenant, TenantBillingConfig, TenantPaymentStatus,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_tenant_create() {
        let mut client = create_test_client();

        let tenant_pubkey = Pubkey::new_unique();
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let existing_tenant = Tenant {
            account_type: AccountType::Tenant,
            owner: Pubkey::default(),
            bump_seed: 0,
            code: "existing".to_string(),
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
        client
            .expect_list_tenant()
            .with(predicate::eq(ListTenantCommand {}))
            .returning(move |_| {
                Ok(vec![(tenant_pubkey, existing_tenant.clone())]
                    .into_iter()
                    .collect())
            });
        let payer: Pubkey = Pubkey::from_str_const("DDddB7bhR9azxLAUEH7ZVtW168wRdreiDKhi4McDfKZt");
        client
            .expect_create_tenant()
            .with(predicate::eq(CreateTenantCommand {
                code: "new-tenant".to_string(),
                administrator: payer,
                token_account: None,
                metro_routing: false,
                route_liveness: false,
            }))
            .times(1)
            .returning(move |_| Ok((signature, tenant_pubkey)));

        /*****************************************************************************************************/
        // Duplicate code should fail
        let mut output = Vec::new();
        let res = CreateTenantCliCommand {
            code: "existing".to_string(),
            administrator: "me".to_string(),
            token_account: None,
            metro_routing: false,
            route_liveness: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_err());

        // New code should succeed
        let mut output = Vec::new();
        let res = CreateTenantCliCommand {
            code: "new-tenant".to_string(),
            administrator: "me".to_string(),
            token_account: None,
            metro_routing: false,
            route_liveness: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,
            "Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }
}
