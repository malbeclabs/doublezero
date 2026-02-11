use std::net::Ipv4Addr;

use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_accesspass_pda,
    processors::accesspass::set::SetAccessPassArgs, state::accesspass::AccessPassType,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{
    commands::{accesspass::get::GetAccessPassCommand, globalstate::get::GetGlobalStateCommand},
    DoubleZeroClient,
};

#[derive(Debug, PartialEq, Clone)]
pub struct SetAccessPassCommand {
    pub accesspass_type: AccessPassType,
    pub client_ip: Ipv4Addr,
    pub user_payer: Pubkey,
    pub last_access_epoch: u64,
    pub allow_multiple_ip: bool,
    pub tenant: Pubkey,
}

impl SetAccessPassCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

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

        let (pda_pubkey, _) =
            get_accesspass_pda(&client.get_program_id(), &self.client_ip, &self.user_payer);

        let accesspass = GetAccessPassCommand {
            client_ip: self.client_ip,
            user_payer: self.user_payer,
        }
        .execute(client)?;

        let mut accounts = vec![
            AccountMeta::new(pda_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
            AccountMeta::new(self.user_payer, false),
        ];

        if self.tenant != Pubkey::default() {
            let tenant_allowlist_first = accesspass
                .as_ref()
                .and_then(|(_, ap)| ap.tenant_allowlist.first().copied())
                .unwrap_or_default();
            accounts.push(AccountMeta::new(tenant_allowlist_first, false));
            accounts.push(AccountMeta::new(self.tenant, false));
        }

        client.execute_transaction(
            DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
                accesspass_type: self.accesspass_type.clone(),
                client_ip: self.client_ip,
                last_access_epoch: self.last_access_epoch,
                allow_multiple_ip: self.allow_multiple_ip,
            }),
            accounts,
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::accesspass::set::SetAccessPassCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_accesspass_pda, get_globalstate_pda},
        processors::accesspass::set::SetAccessPassArgs,
        state::{
            accesspass::{AccessPass, AccessPassStatus, AccessPassType},
            accountdata::AccountData,
            accounttype::AccountType,
        },
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_set_accesspass_command() {
        let mut client = create_test_client();

        let client_ip = [10, 0, 0, 1].into();
        let payer = Pubkey::new_unique();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, _) = get_accesspass_pda(&client.get_program_id(), &client_ip, &payer);

        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 0,
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            user_payer: payer,
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Connected,
            owner: client.get_payer(),
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            flags: 0,
        };
        client
            .expect_get()
            .with(predicate::eq(pda_pubkey))
            .returning(move |_| Ok(AccountData::AccessPass(accesspass.clone())));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
                    accesspass_type: AccessPassType::Prepaid,
                    client_ip,
                    last_access_epoch: 0,
                    allow_multiple_ip: false,
                })),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new_readonly(globalstate_pubkey, false),
                    AccountMeta::new(payer, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = SetAccessPassCommand {
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            user_payer: payer,
            last_access_epoch: 0,
            allow_multiple_ip: false,
            tenant: Pubkey::default(),
        }
        .execute(&client);
        assert!(res.is_ok());
    }
}
