use crate::{
    commands::{accesspass::get::GetAccessPassCommand, globalstate::get::GetGlobalStateCommand},
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    processors::user::transfer_ownership::TransferUserOwnershipArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

#[derive(Debug, PartialEq, Clone)]
pub struct TransferUserOwnershipCommand {
    pub user_pubkey: Pubkey,
    pub client_ip: Ipv4Addr,
    pub old_user_payer: Pubkey,
    pub new_user_payer: Pubkey,
}

impl TransferUserOwnershipCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        // Resolve old access pass (current owner's)
        let (old_accesspass_pk, _) = GetAccessPassCommand {
            client_ip: self.client_ip,
            user_payer: self.old_user_payer,
        }
        .execute(client)?
        .ok_or_else(|| {
            eyre::eyre!(
                "Old AccessPass not found for IP {} and payer {}",
                self.client_ip,
                self.old_user_payer
            )
        })?;

        // Resolve new access pass (new owner's)
        let (new_accesspass_pk, _) = GetAccessPassCommand {
            client_ip: self.client_ip,
            user_payer: self.new_user_payer,
        }
        .execute(client)?
        .ok_or_else(|| {
            eyre::eyre!(
                "New AccessPass not found for IP {} and payer {}",
                self.client_ip,
                self.new_user_payer
            )
        })?;

        let accounts = vec![
            AccountMeta::new(self.user_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(old_accesspass_pk, false),
            AccountMeta::new(new_accesspass_pk, false),
        ];

        client.execute_transaction(
            DoubleZeroInstruction::TransferUserOwnership(TransferUserOwnershipArgs {}),
            accounts,
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{tests::utils::create_test_client, MockDoubleZeroClient};
    use doublezero_serviceability::{
        pda::{get_accesspass_pda, get_globalstate_pda},
        state::{
            accesspass::{AccessPass, AccessPassStatus, AccessPassType},
            accountdata::AccountData,
            accounttype::AccountType,
            globalstate::GlobalState,
        },
    };
    use mockall::predicate;

    #[test]
    fn test_transfer_user_ownership_command() {
        let mut client = MockDoubleZeroClient::new();

        let payer = Pubkey::new_unique();
        client.expect_get_payer().returning(move || payer);
        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);

        let old_user_payer = Pubkey::new_unique();
        let new_user_payer = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(100, 0, 0, 1);
        let user_pubkey = Pubkey::new_unique();

        // Mock GlobalState
        let (globalstate_pubkey, bump_seed) = get_globalstate_pda(&program_id);
        let globalstate = GlobalState {
            account_type: AccountType::GlobalState,
            bump_seed,
            feed_authority_pk: old_user_payer,
            ..Default::default()
        };
        client
            .expect_get()
            .with(predicate::eq(globalstate_pubkey))
            .returning(move |_| Ok(AccountData::GlobalState(globalstate.clone())));

        // Mock old AccessPass
        let (old_accesspass_pk, _) = get_accesspass_pda(&program_id, &client_ip, &old_user_payer);
        let old_ap = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 0,
            client_ip,
            user_payer: old_user_payer,
            owner: old_user_payer,
            status: AccessPassStatus::Connected,
            connection_count: 1,
            accesspass_type: AccessPassType::Prepaid,
            last_access_epoch: 9999,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            flags: 0,
        };
        client
            .expect_get()
            .with(predicate::eq(old_accesspass_pk))
            .returning(move |_| Ok(AccountData::AccessPass(old_ap.clone())));

        // Mock new AccessPass
        let (new_accesspass_pk, _) = get_accesspass_pda(&program_id, &client_ip, &new_user_payer);
        let new_ap = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 0,
            client_ip,
            user_payer: new_user_payer,
            owner: new_user_payer,
            status: AccessPassStatus::Requested,
            connection_count: 0,
            accesspass_type: AccessPassType::Prepaid,
            last_access_epoch: 9999,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            flags: 0,
        };
        client
            .expect_get()
            .with(predicate::eq(new_accesspass_pk))
            .returning(move |_| Ok(AccountData::AccessPass(new_ap.clone())));

        // Expect transaction
        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::TransferUserOwnership(
                    TransferUserOwnershipArgs {},
                )),
                predicate::eq(vec![
                    AccountMeta::new(user_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(old_accesspass_pk, false),
                    AccountMeta::new(new_accesspass_pk, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let result = TransferUserOwnershipCommand {
            user_pubkey,
            client_ip,
            old_user_payer,
            new_user_payer,
        }
        .execute(&client);

        assert!(result.is_ok());
    }

    #[test]
    fn test_transfer_user_ownership_command_old_accesspass_not_found() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let old_user_payer = Pubkey::new_unique();
        let new_user_payer = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(100, 0, 0, 1);

        // Mock old AccessPass — not found
        let (old_accesspass_pk, _) = get_accesspass_pda(&program_id, &client_ip, &old_user_payer);
        client
            .expect_get()
            .with(predicate::eq(old_accesspass_pk))
            .returning(|_| Err(eyre::eyre!("not found")));

        let result = TransferUserOwnershipCommand {
            user_pubkey: Pubkey::new_unique(),
            client_ip,
            old_user_payer,
            new_user_payer,
        }
        .execute(&client);

        assert!(result.is_err());
        assert!(result
            .unwrap_err()
            .to_string()
            .contains("Old AccessPass not found"));
    }
}
