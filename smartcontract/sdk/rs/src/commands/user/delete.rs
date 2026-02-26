use std::net::Ipv4Addr;

use crate::{
    commands::{accesspass::get::GetAccessPassCommand, globalstate::get::GetGlobalStateCommand},
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::user::delete::UserDeleteArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeleteUserCommand {
    pub pubkey: Pubkey,
}

impl DeleteUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let user = client
            .get(self.pubkey)
            .map_err(|_| eyre::eyre!("User not found ({})", self.pubkey))?
            .get_user()
            .map_err(|e| eyre::eyre!(e))?;

        let (accesspass_pk, _) = GetAccessPassCommand {
            client_ip: Ipv4Addr::UNSPECIFIED,
            user_payer: user.owner,
        }
        .execute(client)?
        .or_else(|| {
            GetAccessPassCommand {
                client_ip: user.client_ip,
                user_payer: user.owner,
            }
            .execute(client)
            .ok()
            .flatten()
        })
        .ok_or_else(|| eyre::eyre!("You have no Access Pass"))?;

        client.execute_transaction(
            DoubleZeroInstruction::DeleteUser(UserDeleteArgs {}),
            vec![
                AccountMeta::new(self.pubkey, false),
                AccountMeta::new(accesspass_pk, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::user::delete::DeleteUserCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_program_common::types::NetworkV4;
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_accesspass_pda, get_globalstate_pda, get_multicastgroup_pda},
        processors::user::delete::UserDeleteArgs,
        state::{
            accesspass::{AccessPass, AccessPassStatus, AccessPassType},
            accountdata::AccountData,
            accounttype::AccountType,
            user::{User, UserCYOA, UserStatus, UserType},
        },
    };
    use mockall::{predicate, Sequence};
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
    use std::net::Ipv4Addr;

    #[test]
    fn test_delete_user_sends_delete_transaction_directly() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let user_pubkey = Pubkey::new_unique();
        let (mgroup_pubkey, _) = get_multicastgroup_pda(&client.get_program_id(), 1);
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        // User with subscribers — delete should proceed without client-side unsubscribe
        let user = User {
            account_type: AccountType::User,
            owner: client.get_payer(),
            bump_seed: 0,
            index: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::Multicast,
            device_pk: Pubkey::default(),
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            dz_ip: client_ip,
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            status: UserStatus::Activated,
            publishers: vec![mgroup_pubkey],
            subscribers: vec![mgroup_pubkey],
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
            connection_count: 0,
            status: AccessPassStatus::Requested,
            owner: client.get_payer(),
            mgroup_pub_allowlist: vec![mgroup_pubkey],
            mgroup_sub_allowlist: vec![mgroup_pubkey],
            tenant_allowlist: vec![],
            flags: 0,
        };

        let mut seq = Sequence::new();

        // Call 1: Initial user fetch
        let user_clone = user.clone();
        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::User(user_clone.clone())));

        // Call 2: AccessPass fetch
        let accesspass_clone = accesspass.clone();
        client
            .expect_get()
            .with(predicate::eq(accesspass_pubkey))
            .times(1)
            .in_sequence(&mut seq)
            .returning(move |_| Ok(AccountData::AccessPass(accesspass_clone.clone())));

        // Call 3: DeleteUser transaction — no unsubscribe transactions should precede this
        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeleteUser(UserDeleteArgs {})),
                predicate::eq(vec![
                    AccountMeta::new(user_pubkey, false),
                    AccountMeta::new(accesspass_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = DeleteUserCommand {
            pubkey: user_pubkey,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
