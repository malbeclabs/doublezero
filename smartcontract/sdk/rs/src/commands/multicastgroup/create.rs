use doublezero_program_common::validate_account_code;
use doublezero_serviceability::{
    pda::get_multicastgroup_pda, processors::multicastgroup::create::MulticastGroupCreateArgs,
};
use doublezero_serviceability_instruction::multicastgroup::create_multicast_group;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct CreateMulticastGroupCommand {
    pub code: String,
    pub max_bandwidth: u64,
    pub owner: Pubkey,
}

impl CreateMulticastGroupCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let code =
            validate_account_code(&self.code).map_err(|err| eyre::eyre!("invalid code: {err}"))?;

        let (_, globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let program_id = client.get_program_id();
        let account_index = globalstate.account_index + 1;
        let (pda_pubkey, _) = get_multicastgroup_pda(&program_id, account_index);

        client
            .send_transaction(create_multicast_group(
                &program_id,
                &client.get_payer(),
                account_index,
                MulticastGroupCreateArgs {
                    code,
                    max_bandwidth: self.max_bandwidth,
                    owner: self.owner,
                    use_onchain_allocation: true,
                },
            ))
            .map(|sig| (sig, pda_pubkey))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::multicastgroup::create::CreateMulticastGroupCommand,
        tests::utils::create_test_client, DoubleZeroClient,
    };
    use doublezero_serviceability::{
        pda::get_multicastgroup_pda, processors::multicastgroup::create::MulticastGroupCreateArgs,
    };
    use doublezero_serviceability_instruction::multicastgroup::create_multicast_group;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_multicastgroup_create() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let owner = Pubkey::new_unique();

        // create_test_client seeds globalstate.account_index = 0, so the new group index is 1.
        let expected = create_multicast_group(
            &program_id,
            &payer,
            1,
            MulticastGroupCreateArgs {
                code: "test_group".to_string(),
                max_bandwidth: 1000,
                owner,
                use_onchain_allocation: true,
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let create_command = CreateMulticastGroupCommand {
            code: "test_group".to_string(),
            max_bandwidth: 1000,
            owner,
        };

        let create_invalid_command = CreateMulticastGroupCommand {
            code: "test/group".to_string(),
            ..create_command.clone()
        };

        let res = create_command.execute(&client);
        assert!(res.is_ok());
        assert_eq!(res.unwrap().1, get_multicastgroup_pda(&program_id, 1).0);

        let res = create_invalid_command.execute(&client);
        assert!(res.is_err());
    }
}
