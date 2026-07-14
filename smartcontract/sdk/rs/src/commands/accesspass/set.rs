use std::net::Ipv4Addr;

use doublezero_serviceability::{
    processors::accesspass::set::SetAccessPassArgs, state::accesspass::AccessPassType,
};
use doublezero_serviceability_instruction::accesspass::set_access_pass;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

use crate::{commands::accesspass::get::GetAccessPassCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct SetAccessPassCommand {
    pub accesspass_type: AccessPassType,
    pub client_ip: Ipv4Addr,
    pub user_payer: Pubkey,
    pub last_access_epoch: u64,
    pub allow_multiple_ip: bool,
    pub tenant: Pubkey,
    pub max_unicast_users: u16,
    pub max_multicast_users: u16,
}

impl SetAccessPassCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        if self.last_access_epoch > 0 && self.last_access_epoch != u64::MAX {
            let epoch = client.get_epoch()?;
            if self.last_access_epoch < epoch {
                return Err(eyre::eyre!(
                    "last_access_epoch {} cannot be in the past (current epoch is {})",
                    self.last_access_epoch,
                    epoch
                ));
            }
        }

        let accesspass = GetAccessPassCommand {
            client_ip: self.client_ip,
            user_payer: self.user_payer,
        }
        .execute(client)?;

        // Get the current tenant from the existing access pass (if any). The builder appends the
        // `[current_tenant, new_tenant]` pair itself when either is non-default.
        let current_tenant = accesspass
            .as_ref()
            .and_then(|(_, ap)| ap.tenant_allowlist.first().copied())
            .unwrap_or_default();

        client.send_transaction(set_access_pass(
            &client.get_program_id(),
            &client.get_payer(),
            &self.user_payer,
            &current_tenant,
            &self.tenant,
            SetAccessPassArgs {
                accesspass_type: self.accesspass_type.clone(),
                client_ip: self.client_ip,
                last_access_epoch: self.last_access_epoch,
                allow_multiple_ip: self.allow_multiple_ip,
                max_unicast_users: self.max_unicast_users,
                max_multicast_users: self.max_multicast_users,
            },
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::accesspass::set::SetAccessPassCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        pda::get_accesspass_pda,
        processors::accesspass::set::SetAccessPassArgs,
        state::{
            accesspass::{AccessPass, AccessPassStatus, AccessPassType},
            accountdata::AccountData,
            accounttype::AccountType,
        },
    };
    use doublezero_serviceability_instruction::accesspass::set_access_pass;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::net::Ipv4Addr;

    #[test]
    fn test_commands_set_accesspass_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let client_ip = [10, 0, 0, 1].into();
        let user_payer = Pubkey::new_unique();

        let (pda_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &user_payer);

        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 0,
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            user_payer,
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Connected,
            owner: payer,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            flags: 0,
            unicast_user_count: 0,
            max_unicast_users: 1,
            multicast_user_count: 0,
            max_multicast_users: 1,
        };
        client
            .expect_get()
            .with(predicate::eq(pda_pubkey))
            .returning(move |_| Ok(AccountData::AccessPass(accesspass.clone())));

        // GetAccessPassCommand checks the UNSPECIFIED (dynamic) PDA first; no pass
        // exists there, so it falls back to the exact-IP PDA above.
        let (dynamic_pubkey, _) =
            get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &user_payer);
        client
            .expect_get()
            .with(predicate::eq(dynamic_pubkey))
            .returning(|_| Err(eyre::eyre!("account not found")));

        // No tenant on the existing pass and no new tenant, so the builder omits the tenant pair.
        let expected = set_access_pass(
            &program_id,
            &payer,
            &user_payer,
            &Pubkey::default(),
            &Pubkey::default(),
            SetAccessPassArgs {
                accesspass_type: AccessPassType::Prepaid,
                client_ip,
                last_access_epoch: 0,
                allow_multiple_ip: false,
                max_unicast_users: 1,
                max_multicast_users: 1,
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = SetAccessPassCommand {
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            user_payer,
            last_access_epoch: 0,
            allow_multiple_ip: false,
            tenant: Pubkey::default(),
            max_unicast_users: 1,
            max_multicast_users: 1,
        }
        .execute(&client);
        assert!(res.is_ok());
    }
}
