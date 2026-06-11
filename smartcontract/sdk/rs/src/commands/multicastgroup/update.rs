use crate::{DoubleZeroClient, GetGlobalStateCommand};
use doublezero_program_common::validate_account_code;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_resource_extension_pda,
    processors::multicastgroup::update::MulticastGroupUpdateArgs, resource::ResourceType,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateMulticastGroupCommand {
    pub pubkey: Pubkey,
    pub code: Option<String>,
    pub multicast_ip: Option<Ipv4Addr>,
    pub max_bandwidth: Option<u64>,
    pub publisher_count: Option<u32>,
    pub subscriber_count: Option<u32>,
    pub owner: Option<Pubkey>,
}

impl UpdateMulticastGroupCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let code = self
            .code
            .as_ref()
            .map(|code| validate_account_code(code))
            .transpose()
            .map_err(|err| eyre::eyre!("invalid code: {err}"))?;
        let (globalstate_pubkey, _) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        // multicast_ip changes require the resource extension; other field updates skip it.
        let updating_multicast_ip = self.multicast_ip.is_some();

        let mut accounts = vec![
            AccountMeta::new(self.pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ];

        if updating_multicast_ip {
            let (multicast_group_block_ext, _, _) = get_resource_extension_pda(
                &client.get_program_id(),
                ResourceType::MulticastGroupBlock,
            );
            accounts.push(AccountMeta::new(multicast_group_block_ext, false));
        }

        client.execute_transaction(
            DoubleZeroInstruction::UpdateMulticastGroup(MulticastGroupUpdateArgs {
                code,
                multicast_ip: self.multicast_ip,
                max_bandwidth: self.max_bandwidth,
                publisher_count: self.publisher_count,
                subscriber_count: self.subscriber_count,
                use_onchain_allocation: updating_multicast_ip,
                owner: self.owner,
            }),
            accounts,
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::multicastgroup::update::UpdateMulticastGroupCommand,
        tests::utils::create_test_client, DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_location_pda, get_resource_extension_pda},
        processors::multicastgroup::update::MulticastGroupUpdateArgs,
        resource::ResourceType,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, signature::Signature};

    #[test]
    fn test_commands_multicastgroup_update_with_multicast_ip() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
        let (pda_pubkey, _) = get_location_pda(&program_id, 1);
        let (multicast_group_block_ext, _, _) =
            get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::UpdateMulticastGroup(
                    MulticastGroupUpdateArgs {
                        code: Some("test_group".to_string()),
                        multicast_ip: Some("239.0.0.1".parse().unwrap()),
                        max_bandwidth: Some(1000),
                        publisher_count: Some(10),
                        subscriber_count: Some(100),
                        use_onchain_allocation: true,
                        owner: None,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(multicast_group_block_ext, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let update_command = UpdateMulticastGroupCommand {
            pubkey: pda_pubkey,
            code: Some("test_group".to_string()),
            multicast_ip: Some("239.0.0.1".parse().unwrap()),
            max_bandwidth: Some(1000),
            publisher_count: Some(10),
            subscriber_count: Some(100),
            owner: None,
        };

        let update_invalid_command = UpdateMulticastGroupCommand {
            code: Some("test/group".to_string()),
            ..update_command.clone()
        };

        let res = update_command.execute(&client);
        assert!(res.is_ok());

        let res = update_invalid_command.execute(&client);
        assert!(res.is_err());
    }

    #[test]
    fn test_commands_multicastgroup_update_without_multicast_ip() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
        let (pda_pubkey, _) = get_location_pda(&program_id, 1);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::UpdateMulticastGroup(
                    MulticastGroupUpdateArgs {
                        code: None,
                        multicast_ip: None,
                        max_bandwidth: Some(2000),
                        publisher_count: None,
                        subscriber_count: None,
                        use_onchain_allocation: false,
                        owner: None,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = UpdateMulticastGroupCommand {
            pubkey: pda_pubkey,
            code: None,
            multicast_ip: None,
            max_bandwidth: Some(2000),
            publisher_count: None,
            subscriber_count: None,
            owner: None,
        }
        .execute(&client);
        assert!(res.is_ok());
    }
}
