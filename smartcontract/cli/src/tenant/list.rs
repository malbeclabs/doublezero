use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_program_common::serializer;
use doublezero_sdk::commands::tenant::list::ListTenantCommand;
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use tabled::{settings::Style, Table, Tabled};

#[derive(Args, Debug)]
pub struct ListTenantCliCommand {
    /// Output as pretty JSON
    #[arg(long, default_value_t = false)]
    pub json: bool,
    /// Output as compact JSON
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
}

#[derive(Tabled, Serialize)]
pub struct TenantDisplay {
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub code: String,
    pub vrf_id: u16,
    pub metro_routing: bool,
    pub route_liveness: bool,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey,
}

impl ListTenantCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let tenants = client.list_tenant(ListTenantCommand {})?;

        let mut tenant_displays: Vec<TenantDisplay> = tenants
            .into_iter()
            .map(|(pubkey, tenant)| TenantDisplay {
                account: pubkey,
                code: tenant.code,
                vrf_id: tenant.vrf_id,
                metro_routing: tenant.metro_routing,
                route_liveness: tenant.route_liveness,
                owner: tenant.owner,
            })
            .collect();

        tenant_displays.sort_by(|a, b| a.code.cmp(&b.code));

        let res = if self.json {
            serde_json::to_string_pretty(&tenant_displays)?
        } else if self.json_compact {
            serde_json::to_string(&tenant_displays)?
        } else {
            Table::new(tenant_displays)
                .with(Style::psql().remove_horizontals())
                .to_string()
        };

        writeln!(out, "{res}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{tenant::list::ListTenantCliCommand, tests::utils::create_test_client};
    use doublezero_sdk::AccountType;
    use doublezero_serviceability::state::tenant::{
        Tenant, TenantBillingConfig, TenantPaymentStatus,
    };
    use solana_sdk::pubkey::Pubkey;
    use std::collections::HashMap;

    #[test]
    fn test_cli_tenant_list() {
        let mut client = create_test_client();

        let tenant1_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");
        let tenant1 = Tenant {
            account_type: AccountType::Tenant,
            owner: tenant1_pubkey,
            bump_seed: 0,
            code: "tenant-a".to_string(),
            vrf_id: 100,
            reference_count: 0,
            administrators: vec![],
            token_account: Pubkey::default(),
            payment_status: TenantPaymentStatus::Paid,
            metro_routing: true,
            route_liveness: false,
            billing: TenantBillingConfig::default(),
        };

        client
            .expect_list_tenant()
            .returning(move |_| Ok(HashMap::from([(tenant1_pubkey, tenant1.clone())])));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let res = ListTenantCliCommand {
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,
            " account                                   | code     | vrf_id | metro_routing | route_liveness | owner                                     \n 11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo | tenant-a | 100    | true        | false           | 11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo \n"
        );

        let mut output = Vec::new();
        let res = ListTenantCliCommand {
            json: false,
            json_compact: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,
            "[{\"account\":\"11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo\",\"code\":\"tenant-a\",\"vrf_id\":100,\"metro_routing\":true,\"route_liveness\":false,\"owner\":\"11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo\"}]\n"
        );
    }
}
