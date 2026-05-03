use doublezero_program_common::validate_account_code;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_multicastgroup_pda, get_resource_extension_pda},
    processors::multicastgroup::create::MulticastGroupCreateArgs,
    resource::ResourceType,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

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

        let (globalstate_pubkey, globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, _) =
            get_multicastgroup_pda(&client.get_program_id(), globalstate.account_index + 1);
        let (multicast_group_block_ext, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::MulticastGroupBlock);

        let accounts = vec![
            AccountMeta::new(pda_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(multicast_group_block_ext, false),
        ];

        client
            .execute_transaction(
                DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
                    code,
                    max_bandwidth: self.max_bandwidth,
                    owner: self.owner,
                    use_onchain_allocation: true,
                }),
                accounts,
            )
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
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_multicastgroup_pda, get_resource_extension_pda},
        processors::multicastgroup::create::MulticastGroupCreateArgs,
        resource::ResourceType,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_multicastgroup_create() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
        let (pda_pubkey, _) = get_multicastgroup_pda(&program_id, 1);
        let (multicast_group_block_ext, _, _) =
            get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock);
        let owner = Pubkey::new_unique();

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::CreateMulticastGroup(
                    MulticastGroupCreateArgs {
                        code: "test_group".to_string(),
                        max_bandwidth: 1000,
                        owner,
                        use_onchain_allocation: true,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(multicast_group_block_ext, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

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

        let res = create_invalid_command.execute(&client);
        assert!(res.is_err());
    }
}
