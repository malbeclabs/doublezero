use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::validate_pubkey_or_code,
};
use clap::Args;
use doublezero_sdk::commands::tenant::{
    get::GetTenantCommand, update_payment_status::UpdatePaymentStatusCommand,
};
use std::io::Write;

#[derive(Args, Debug)]
pub struct UpdatePaymentStatusCliCommand {
    /// Tenant pubkey or code
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub pubkey: String,
    /// Payment status (0=Unknown, 1=Paid, 2=Delinquent, 3=Suspended)
    #[arg(long)]
    pub payment_status: u8,
}

impl UpdatePaymentStatusCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let (tenant_pubkey, _tenant) = client.get_tenant(GetTenantCommand {
            pubkey_or_code: self.pubkey,
        })?;

        let signature = client.update_payment_status_tenant(UpdatePaymentStatusCommand {
            tenant_pubkey,
            payment_status: self.payment_status,
            last_deduction_dz_epoch: None,
        })?;

        writeln!(out, "Signature: {signature}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tenant::update_payment_status::UpdatePaymentStatusCliCommand,
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{
        commands::tenant::{
            get::GetTenantCommand, update_payment_status::UpdatePaymentStatusCommand,
        },
        AccountType,
    };
    use doublezero_serviceability::state::tenant::{Tenant, TenantPaymentStatus};
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_tenant_update_payment_status() {
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
            payment_status: TenantPaymentStatus::Delinquent,
            metro_route: false,
            route_aliveness: false,
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
        client
            .expect_update_payment_status_tenant()
            .with(predicate::eq(UpdatePaymentStatusCommand {
                tenant_pubkey,
                payment_status: 1,
            }))
            .returning(move |_| Ok(signature));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let res = UpdatePaymentStatusCliCommand {
            pubkey: tenant_pubkey.to_string(),
            payment_status: 1,
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
