use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::validate_pubkey_or_code,
};
use clap::Args;
use doublezero_sdk::commands::tenant::{get::GetTenantCommand, update::UpdateTenantCommand};
use doublezero_serviceability::state::tenant::{FlatPerEpochConfig, TenantBillingConfig};
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
    pub metro_routing: Option<bool>,
    /// Enable/disable route aliveness checks
    #[arg(long)]
    pub route_liveness: Option<bool>,
    /// Flat billing rate per epoch (in lamports)
    #[arg(long)]
    pub billing_rate: Option<u64>,
}

impl UpdateTenantCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let (tenant_pubkey, tenant) = client.get_tenant(GetTenantCommand {
            pubkey_or_code: self.pubkey,
        })?;

        let token_account = self
            .token_account
            .map(|s| Pubkey::from_str(&s))
            .transpose()?;

        let billing = self.billing_rate.map(|rate| {
            // NOTE: we use existing dz_epoch for idempotency
            let TenantBillingConfig::FlatPerEpoch(existing) = tenant.billing;
            TenantBillingConfig::FlatPerEpoch(FlatPerEpochConfig {
                rate,
                last_deduction_dz_epoch: existing.last_deduction_dz_epoch,
            })
        });

        let signature = client.update_tenant(UpdateTenantCommand {
            tenant_pubkey,
            vrf_id: self.vrf_id,
            token_account,
            metro_routing: self.metro_routing,
            route_liveness: self.route_liveness,
            billing,
        })?;

        writeln!(out, "Signature: {signature}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tenant::update::UpdateTenantCliCommand,
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{
        commands::tenant::{get::GetTenantCommand, update::UpdateTenantCommand},
        AccountType,
    };
    use doublezero_serviceability::state::tenant::{
        Tenant, TenantBillingConfig, TenantPaymentStatus,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_tenant_update() {
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
        client
            .expect_get_tenant()
            .with(predicate::eq(GetTenantCommand {
                pubkey_or_code: tenant_pubkey.to_string(),
            }))
            .returning(move |_| Ok((tenant_pubkey, tenant.clone())));
        client
            .expect_update_tenant()
            .with(predicate::eq(UpdateTenantCommand {
                tenant_pubkey,
                vrf_id: Some(200),
                token_account: None,
                metro_routing: Some(true),
                route_liveness: None,
                billing: None,
            }))
            .returning(move |_| Ok(signature));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let res = UpdateTenantCliCommand {
            pubkey: tenant_pubkey.to_string(),
            vrf_id: Some(200),
            token_account: None,
            metro_routing: Some(true),
            route_liveness: None,
            billing_rate: None,
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
