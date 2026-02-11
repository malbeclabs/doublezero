use std::time::Duration;

use crate::{
    commands::{
        accesspass::{list::ListAccessPassCommand, set::SetAccessPassCommand},
        user::{delete::DeleteUserCommand, list::ListUserCommand},
    },
    DoubleZeroClient,
};
use backon::{BlockingRetryable, ExponentialBuilder};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_globalstate_pda, get_resource_extension_pda},
    processors::tenant::delete::TenantDeleteArgs,
    resource::ResourceType,
    state::accountdata::AccountData,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeleteTenantCommand {
    pub tenant_pubkey: Pubkey,
    pub allow_delete_users: bool,
}

impl DeleteTenantCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        if self.allow_delete_users {
            // 1. List all users belonging to this tenant and delete them
            let users = ListUserCommand.execute(client)?;
            let tenant_users: Vec<Pubkey> = users
                .into_iter()
                .filter(|(_, user)| user.tenant_pk == self.tenant_pubkey)
                .map(|(pk, _)| pk)
                .collect();

            for user_pk in &tenant_users {
                DeleteUserCommand { pubkey: *user_pk }.execute(client)?;
            }

            // 2. Wait for activator to process close accounts (reference_count reaches 0)
            if !tenant_users.is_empty() {
                let tenant_pubkey = self.tenant_pubkey;
                let builder = ExponentialBuilder::new()
                    .with_max_times(8) // 1+2+4+8+16+32+32+32 = 127 seconds max
                    .with_min_delay(Duration::from_secs(1))
                    .with_max_delay(Duration::from_secs(32));

                let get_tenant = || match client.get(tenant_pubkey) {
                    Ok(AccountData::Tenant(tenant)) => {
                        if tenant.reference_count != 0 {
                            Err(())
                        } else {
                            Ok(())
                        }
                    }
                    _ => Err(()),
                };

                get_tenant.retry(builder).call().map_err(|_| {
                    eyre::eyre!("Timeout waiting for tenant reference_count to reach 0")
                })?;
            }
        }

        // Always List access passes that include this tenant and reset their tenant to default
        let access_passes = ListAccessPassCommand.execute(client)?;
        let tenant_passes: Vec<_> = access_passes
            .into_iter()
            .filter(|(_, ap)| ap.tenant_allowlist.contains(&self.tenant_pubkey))
            .collect();

        for (_, ap) in &tenant_passes {
            SetAccessPassCommand {
                accesspass_type: ap.accesspass_type.clone(),
                client_ip: ap.client_ip,
                user_payer: ap.user_payer,
                last_access_epoch: ap.last_access_epoch,
                allow_multiple_ip: ap.allow_multiple_ip(),
                tenant: Pubkey::default(),
            }
            .execute(client)?;
        }

        // Execute the DeleteTenant transaction
        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let (vrf_ids_pda, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::VrfIds);

        client.execute_transaction(
            DoubleZeroInstruction::DeleteTenant(TenantDeleteArgs {}),
            vec![
                AccountMeta::new(self.tenant_pubkey, false),
                AccountMeta::new_readonly(globalstate_pubkey, false),
                AccountMeta::new(vrf_ids_pda, false),
            ],
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::tenant::delete::DeleteTenantCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_program_common::types::NetworkV4;
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_accesspass_pda, get_globalstate_pda, get_resource_extension_pda},
        processors::{
            accesspass::set::SetAccessPassArgs, tenant::delete::TenantDeleteArgs,
            user::delete::UserDeleteArgs,
        },
        resource::ResourceType,
        state::{
            accesspass::{AccessPass, AccessPassStatus, AccessPassType},
            accountdata::AccountData,
            accounttype::AccountType,
            tenant::{Tenant, TenantBillingConfig, TenantPaymentStatus},
            user::{User, UserCYOA, UserStatus, UserType},
        },
    };
    use mockall::{predicate, Sequence};
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
    use std::{collections::HashMap, net::Ipv4Addr};

    #[test]
    fn test_delete_tenant_without_cascade() {
        let mut client = create_test_client();

        let tenant_pubkey = Pubkey::new_unique();
        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let (vrf_ids_pda, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::VrfIds);

        // ListAccessPassCommand: gets(AccountType::AccessPass) - no passes for this tenant
        client
            .expect_gets()
            .with(predicate::eq(AccountType::AccessPass))
            .times(1)
            .returning(|_| Ok(HashMap::new()));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeleteTenant(TenantDeleteArgs {})),
                predicate::eq(vec![
                    AccountMeta::new(tenant_pubkey, false),
                    AccountMeta::new_readonly(globalstate_pubkey, false),
                    AccountMeta::new(vrf_ids_pda, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = DeleteTenantCommand {
            tenant_pubkey,
            allow_delete_users: false,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_delete_tenant_with_cascade() {
        let mut client = create_test_client();

        let tenant_pubkey = Pubkey::new_unique();
        let user_pubkey = Pubkey::new_unique();
        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let (vrf_ids_pda, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::VrfIds);
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        let user = User {
            account_type: AccountType::User,
            owner: client.get_payer(),
            bump_seed: 0,
            index: 1,
            tenant_pk: tenant_pubkey,
            user_type: UserType::IBRL,
            device_pk: Pubkey::default(),
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            dz_ip: client_ip,
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            status: UserStatus::Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        };

        let (accesspass_pubkey, _) = get_accesspass_pda(
            &client.get_program_id(),
            &Ipv4Addr::UNSPECIFIED,
            &client.get_payer(),
        );
        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 0,
            accesspass_type: AccessPassType::Prepaid,
            client_ip: Ipv4Addr::UNSPECIFIED,
            user_payer: client.get_payer(),
            last_access_epoch: 0,
            connection_count: 1,
            status: AccessPassStatus::Connected,
            owner: client.get_payer(),
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![tenant_pubkey],
            flags: 0,
        };

        let tenant_after = Tenant {
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

        let mut seq = Sequence::new();

        let value = accesspass.clone();
        client
            .expect_get()
            .with(predicate::eq(accesspass_pubkey))
            .returning(move |_| Ok(AccountData::AccessPass(value.clone())));

        // 1. ListUserCommand: gets(AccountType::User)
        let user_clone = user.clone();
        client
            .expect_gets()
            .with(predicate::eq(AccountType::User))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| {
                let mut map = HashMap::new();
                map.insert(user_pubkey, AccountData::User(user_clone.clone()));
                Ok(map)
            });

        // 2. DeleteUserCommand internally: get(user_pubkey)
        let user_clone2 = user.clone();
        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::User(user_clone2.clone())));

        // 3. DeleteUserCommand internally: gets(AccountType::MulticastGroup) - empty
        client
            .expect_gets()
            .with(predicate::eq(AccountType::MulticastGroup))
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_| Ok(HashMap::new()));

        // 4. DeleteUserCommand internally: execute_transaction(DeleteUser)
        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeleteUser(UserDeleteArgs {})),
                predicate::always(),
            )
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_, _| Ok(Signature::new_unique()));

        // 6. Wait for reference_count: get(tenant_pubkey)
        let tenant_after_clone = tenant_after.clone();
        client
            .expect_get()
            .with(predicate::eq(tenant_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::Tenant(tenant_after_clone.clone())));

        // 7. ListAccessPassCommand: gets(AccountType::AccessPass)
        let accesspass_for_list = accesspass.clone();
        client
            .expect_gets()
            .with(predicate::eq(AccountType::AccessPass))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| {
                let mut map = HashMap::new();
                map.insert(
                    accesspass_pubkey,
                    AccountData::AccessPass(accesspass_for_list.clone()),
                );
                Ok(map)
            });

        // 8. SetAccessPassCommand: execute_transaction(SetAccessPass) to reset tenant
        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
                    accesspass_type: AccessPassType::Prepaid,
                    client_ip: Ipv4Addr::UNSPECIFIED,
                    last_access_epoch: 0,
                    allow_multiple_ip: false,
                })),
                predicate::always(),
            )
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_, _| Ok(Signature::new_unique()));

        // 9. Final: execute_transaction(DeleteTenant)
        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeleteTenant(TenantDeleteArgs {})),
                predicate::eq(vec![
                    AccountMeta::new(tenant_pubkey, false),
                    AccountMeta::new_readonly(globalstate_pubkey, false),
                    AccountMeta::new(vrf_ids_pda, false),
                ]),
            )
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = DeleteTenantCommand {
            tenant_pubkey,
            allow_delete_users: true,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_delete_tenant_without_cascade_resets_access_passes() {
        let mut client = create_test_client();

        let tenant_pubkey = Pubkey::new_unique();
        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let (vrf_ids_pda, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::VrfIds);
        let client_ip = Ipv4Addr::new(10, 0, 0, 1);

        let (accesspass_pubkey, _) =
            get_accesspass_pda(&client.get_program_id(), &client_ip, &client.get_payer());
        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 0,
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            user_payer: client.get_payer(),
            last_access_epoch: 0,
            connection_count: 1,
            status: AccessPassStatus::Connected,
            owner: client.get_payer(),
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![tenant_pubkey],
            flags: 0,
        };

        let mut seq = Sequence::new();

        // GetAccessPassCommand inside SetAccessPassCommand: get(accesspass_pda)
        let ap_for_get = accesspass.clone();
        client
            .expect_get()
            .with(predicate::eq(accesspass_pubkey))
            .returning(move |_| Ok(AccountData::AccessPass(ap_for_get.clone())));

        // 1. ListAccessPassCommand: gets(AccountType::AccessPass)
        let ap_for_list = accesspass.clone();
        client
            .expect_gets()
            .with(predicate::eq(AccountType::AccessPass))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| {
                let mut map = HashMap::new();
                map.insert(
                    accesspass_pubkey,
                    AccountData::AccessPass(ap_for_list.clone()),
                );
                Ok(map)
            });

        // 2. SetAccessPassCommand: execute_transaction(SetAccessPass) to reset tenant
        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
                    accesspass_type: AccessPassType::Prepaid,
                    client_ip,
                    last_access_epoch: 0,
                    allow_multiple_ip: false,
                })),
                predicate::always(),
            )
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_, _| Ok(Signature::new_unique()));

        // 3. execute_transaction(DeleteTenant)
        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeleteTenant(TenantDeleteArgs {})),
                predicate::eq(vec![
                    AccountMeta::new(tenant_pubkey, false),
                    AccountMeta::new_readonly(globalstate_pubkey, false),
                    AccountMeta::new(vrf_ids_pda, false),
                ]),
            )
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = DeleteTenantCommand {
            tenant_pubkey,
            allow_delete_users: false,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_delete_tenant_fails_when_users_connected() {
        let mut client = create_test_client();

        let tenant_pubkey = Pubkey::new_unique();
        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let (vrf_ids_pda, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::VrfIds);

        let mut seq = Sequence::new();

        // 1. ListAccessPassCommand: gets(AccountType::AccessPass) - empty
        client
            .expect_gets()
            .with(predicate::eq(AccountType::AccessPass))
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_| Ok(HashMap::new()));

        // 2. execute_transaction(DeleteTenant) fails because users still connected
        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeleteTenant(TenantDeleteArgs {})),
                predicate::eq(vec![
                    AccountMeta::new(tenant_pubkey, false),
                    AccountMeta::new_readonly(globalstate_pubkey, false),
                    AccountMeta::new(vrf_ids_pda, false),
                ]),
            )
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_, _| Err(eyre::eyre!("Tenant has active users")));

        let res = DeleteTenantCommand {
            tenant_pubkey,
            allow_delete_users: false,
        }
        .execute(&client);

        assert!(res.is_err());
        assert!(res
            .unwrap_err()
            .to_string()
            .contains("Tenant has active users"));
    }

    #[test]
    fn test_delete_tenant_with_allow_delete_users_no_users() {
        let mut client = create_test_client();

        let tenant_pubkey = Pubkey::new_unique();
        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let (vrf_ids_pda, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::VrfIds);

        let mut seq = Sequence::new();

        // 1. ListUserCommand: gets(AccountType::User) - no users for this tenant
        client
            .expect_gets()
            .with(predicate::eq(AccountType::User))
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_| Ok(HashMap::new()));

        // 2. ListAccessPassCommand: gets(AccountType::AccessPass) - empty
        client
            .expect_gets()
            .with(predicate::eq(AccountType::AccessPass))
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_| Ok(HashMap::new()));

        // 3. execute_transaction(DeleteTenant)
        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeleteTenant(TenantDeleteArgs {})),
                predicate::eq(vec![
                    AccountMeta::new(tenant_pubkey, false),
                    AccountMeta::new_readonly(globalstate_pubkey, false),
                    AccountMeta::new(vrf_ids_pda, false),
                ]),
            )
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = DeleteTenantCommand {
            tenant_pubkey,
            allow_delete_users: true,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
