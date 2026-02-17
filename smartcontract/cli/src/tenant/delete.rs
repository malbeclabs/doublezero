use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::validate_pubkey_or_code,
};
use clap::Args;
use doublezero_sdk::commands::{
    accesspass::{list::ListAccessPassCommand, set::SetAccessPassCommand},
    tenant::{delete::DeleteTenantCommand, get::GetTenantCommand},
    user::{delete::DeleteUserCommand, list::ListUserCommand},
};
use indicatif::{ProgressBar, ProgressStyle};
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, time::Duration};

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
            pubkey_or_code: self.pubkey.clone(),
        })?;

        if tenant.reference_count > 0 && !self.allow_delete_users {
            return Err(eyre::eyre!(
                "Cannot delete tenant with reference_count > 0 (current: {}). Use --allow-delete-users to cascade delete.",
                tenant.reference_count
            ));
        }

        if self.allow_delete_users {
            // 1. List all users belonging to this tenant and delete them
            let users = client.list_user(ListUserCommand)?;
            let tenant_users: Vec<Pubkey> = users
                .into_iter()
                .filter(|(_, user)| user.tenant_pk == tenant_pubkey)
                .map(|(pk, _)| pk)
                .collect();

            if !tenant_users.is_empty() {
                let spinner = ProgressBar::new(tenant_users.len() as u64);
                spinner.set_style(
                    ProgressStyle::default_spinner()
                        .template(
                            "{spinner:.green} [{elapsed_precise}] [{bar:40.cyan/blue}] {pos}/{len} {msg}",
                        )
                        .expect("Failed to set template")
                        .progress_chars("#>-")
                        .tick_strings(&["-", "\\", "|", "/"]),
                );
                spinner.enable_steady_tick(Duration::from_millis(100));
                spinner.println(format!("Deleting {} user(s)...", tenant_users.len()));

                for user_pk in &tenant_users {
                    spinner.set_message(format!("Deleting user {}", user_pk));
                    client.delete_user(DeleteUserCommand { pubkey: *user_pk })?;
                    spinner.inc(1);
                }

                spinner.finish_with_message("Done deleting users");
            }

            // 2. Clean up access passes before waiting for reference_count to reach 0
            let access_passes = client.list_accesspass(ListAccessPassCommand)?;
            let tenant_passes: Vec<_> = access_passes
                .into_iter()
                .filter(|(_, ap)| ap.tenant_allowlist.contains(&tenant_pubkey))
                .collect();

            if !tenant_passes.is_empty() {
                let spinner = ProgressBar::new(tenant_passes.len() as u64);
                spinner.set_style(
                    ProgressStyle::default_spinner()
                        .template(
                            "{spinner:.green} [{elapsed_precise}] [{bar:40.cyan/blue}] {pos}/{len} {msg}",
                        )
                        .expect("Failed to set template")
                        .progress_chars("#>-")
                        .tick_strings(&["-", "\\", "|", "/"]),
                );
                spinner.enable_steady_tick(Duration::from_millis(100));
                spinner.println(format!(
                    "Cleaning up {} access pass(es)...",
                    tenant_passes.len()
                ));

                for (_, ap) in &tenant_passes {
                    spinner.set_message(format!("Processing access pass {}", ap.client_ip));
                    client.set_accesspass(SetAccessPassCommand {
                        accesspass_type: ap.accesspass_type.clone(),
                        client_ip: ap.client_ip,
                        user_payer: ap.user_payer,
                        last_access_epoch: ap.last_access_epoch,
                        allow_multiple_ip: ap.allow_multiple_ip(),
                        tenant: Pubkey::default(),
                    })?;
                    spinner.inc(1);
                }

                spinner.finish_with_message("Done cleaning up access passes");
            }

            // 3. Wait for activator to process close accounts (reference_count reaches 0)
            if !tenant_users.is_empty() {
                let spinner = ProgressBar::new_spinner();
                spinner.set_style(
                    ProgressStyle::default_spinner()
                        .template("{spinner:.green} {msg}")
                        .expect("Failed to set template")
                        .tick_strings(&["-", "\\", "|", "/"]),
                );
                spinner.enable_steady_tick(Duration::from_millis(100));
                spinner.set_message("Waiting for activator to process account closures...");

                // Poll tenant reference_count with exponential backoff
                let max_attempts = 8; // 1+2+4+8+16+32+32+32 = 127 seconds max
                let mut delay = Duration::from_secs(1);
                let max_delay = Duration::from_secs(32);

                for attempt in 0..max_attempts {
                    std::thread::sleep(delay);

                    let (_, current_tenant) = client.get_tenant(GetTenantCommand {
                        pubkey_or_code: self.pubkey.clone(),
                    })?;

                    if current_tenant.reference_count == 0 {
                        break;
                    }

                    if attempt == max_attempts - 1 {
                        spinner
                            .finish_with_message("Timeout waiting for reference_count to reach 0");
                        return Err(eyre::eyre!(
                            "Timeout waiting for tenant reference_count to reach 0"
                        ));
                    }

                    // Exponential backoff
                    delay = (delay * 2).min(max_delay);
                }

                spinner.finish_with_message("Activator processing complete");
            }
        } else {
            // When not cascading user deletion, still clean up access passes
            let access_passes = client.list_accesspass(ListAccessPassCommand)?;
            let tenant_passes: Vec<_> = access_passes
                .into_iter()
                .filter(|(_, ap)| ap.tenant_allowlist.contains(&tenant_pubkey))
                .collect();

            if !tenant_passes.is_empty() {
                let spinner = ProgressBar::new(tenant_passes.len() as u64);
                spinner.set_style(
                    ProgressStyle::default_spinner()
                        .template(
                            "{spinner:.green} [{elapsed_precise}] [{bar:40.cyan/blue}] {pos}/{len} {msg}",
                        )
                        .expect("Failed to set template")
                        .progress_chars("#>-")
                        .tick_strings(&["-", "\\", "|", "/"]),
                );
                spinner.enable_steady_tick(Duration::from_millis(100));
                spinner.println(format!(
                    "Cleaning up {} access pass(es)...",
                    tenant_passes.len()
                ));

                for (_, ap) in &tenant_passes {
                    spinner.set_message(format!("Processing access pass {}", ap.client_ip));
                    client.set_accesspass(SetAccessPassCommand {
                        accesspass_type: ap.accesspass_type.clone(),
                        client_ip: ap.client_ip,
                        user_payer: ap.user_payer,
                        last_access_epoch: ap.last_access_epoch,
                        allow_multiple_ip: ap.allow_multiple_ip(),
                        tenant: Pubkey::default(),
                    })?;
                    spinner.inc(1);
                }

                spinner.finish_with_message("Done cleaning up access passes");
            }
        }

        // Execute the DeleteTenant transaction
        let signature = client.delete_tenant(DeleteTenantCommand {
            tenant_pubkey,
            allow_delete_users: false, // We already handled deletion above
        })?;

        writeln!(out)?;
        writeln!(out, "✓ Tenant '{}' deleted successfully", tenant.code)?;
        writeln!(out, "  Signature: {signature}")?;

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

        // List access passes - empty
        client
            .expect_list_accesspass()
            .returning(|_| Ok(std::collections::HashMap::new()));

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
        assert!(output_str.contains("Tenant 'test' deleted successfully"));
        assert!(output_str.contains(&signature.to_string()));
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
        use doublezero_program_common::types::NetworkV4;
        use doublezero_serviceability::state::user::{User, UserCYOA, UserStatus, UserType};
        use mockall::Sequence;
        use std::net::Ipv4Addr;

        let mut client = create_test_client();

        let tenant_pubkey = Pubkey::new_unique();
        let user_pubkey = Pubkey::new_unique();
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

        let user = User {
            account_type: AccountType::User,
            owner: Pubkey::default(),
            bump_seed: 0,
            index: 1,
            tenant_pk: tenant_pubkey,
            user_type: UserType::IBRL,
            device_pk: Pubkey::default(),
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: Ipv4Addr::new(10, 0, 0, 1),
            dz_ip: Ipv4Addr::new(10, 0, 0, 1),
            tunnel_id: 100,
            tunnel_net: NetworkV4::default(),
            status: UserStatus::Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        };

        let tenant_after = Tenant {
            reference_count: 0,
            ..tenant.clone()
        };

        let mut seq = Sequence::new();

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));

        // First get_tenant call
        let tenant_cloned = tenant.clone();
        client
            .expect_get_tenant()
            .times(1)
            .in_sequence(&mut seq)
            .with(predicate::eq(GetTenantCommand {
                pubkey_or_code: tenant_pubkey.to_string(),
            }))
            .returning(move |_| Ok((tenant_pubkey, tenant_cloned.clone())));

        // List users
        let user_cloned = user.clone();
        client
            .expect_list_user()
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| {
                let mut map = std::collections::HashMap::new();
                map.insert(user_pubkey, user_cloned.clone());
                Ok(map)
            });

        // Delete user
        client
            .expect_delete_user()
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_| Ok(Signature::new_unique()));

        // List access passes - empty
        client
            .expect_list_accesspass()
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_| Ok(std::collections::HashMap::new()));

        // Second get_tenant call (checking reference_count)
        let tenant_after_cloned = tenant_after.clone();
        client
            .expect_get_tenant()
            .times(1)
            .in_sequence(&mut seq)
            .with(predicate::eq(GetTenantCommand {
                pubkey_or_code: tenant_pubkey.to_string(),
            }))
            .returning(move |_| Ok((tenant_pubkey, tenant_after_cloned.clone())));

        // Delete tenant
        client
            .expect_delete_tenant()
            .times(1)
            .in_sequence(&mut seq)
            .with(predicate::eq(DeleteTenantCommand {
                tenant_pubkey,
                allow_delete_users: false,
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
