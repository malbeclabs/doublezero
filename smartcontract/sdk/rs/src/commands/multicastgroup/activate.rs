use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    processors::multicastgroup::activate::MulticastGroupActivateArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct ActivateMulticastGroupCommand {
    pub mgroup_pubkey: Pubkey,
    pub multicast_ip: Ipv4Addr,
    /// Optional ResourceExtension pubkey for on-chain IP allocation.
    /// When provided, multicast_ip is ignored and an IP is allocated from the ResourceExtension bitmap.
    pub resource_extension_pubkey: Option<Pubkey>,
}

impl ActivateMulticastGroupCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let accounts = vec![
            AccountMeta::new(self.mgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ];

        let extra_accounts = match self.resource_extension_pubkey {
            Some(pubkey) => vec![AccountMeta::new(pubkey, false)],
            None => vec![],
        };

        client.execute_transaction_with_extra_accounts(
            DoubleZeroInstruction::ActivateMulticastGroup(MulticastGroupActivateArgs {
                multicast_ip: self.multicast_ip,
            }),
            accounts,
            extra_accounts,
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::multicastgroup::activate::ActivateMulticastGroupCommand,
        tests::utils::create_test_client, DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_multicastgroup_pda, get_resource_extension_pda},
        processors::multicastgroup::activate::MulticastGroupActivateArgs,
        resource::ResourceType,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, signature::Signature};
    use std::net::Ipv4Addr;

    #[test]
    fn test_commands_multicastgroup_activate_without_resource_extension() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let (mgroup_pubkey, _) = get_multicastgroup_pda(&client.get_program_id(), 1);

        client
            .expect_execute_transaction_with_extra_accounts()
            .with(
                predicate::eq(DoubleZeroInstruction::ActivateMulticastGroup(
                    MulticastGroupActivateArgs {
                        multicast_ip: [1, 2, 3, 4].into(),
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(mgroup_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
                predicate::eq(vec![]), // No extra accounts
            )
            .returning(|_, _, _| Ok(Signature::new_unique()));

        let res = ActivateMulticastGroupCommand {
            mgroup_pubkey,
            multicast_ip: [1, 2, 3, 4].into(),
            resource_extension_pubkey: None,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_multicastgroup_activate_with_resource_extension() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let (mgroup_pubkey, _) = get_multicastgroup_pda(&client.get_program_id(), 1);
        let (resource_ext_pubkey, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::MulticastGroupBlock);

        client
            .expect_execute_transaction_with_extra_accounts()
            .with(
                predicate::eq(DoubleZeroInstruction::ActivateMulticastGroup(
                    MulticastGroupActivateArgs {
                        multicast_ip: Ipv4Addr::UNSPECIFIED,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(mgroup_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
                predicate::eq(vec![AccountMeta::new(resource_ext_pubkey, false)]),
            )
            .returning(|_, _, _| Ok(Signature::new_unique()));

        let res = ActivateMulticastGroupCommand {
            mgroup_pubkey,
            multicast_ip: Ipv4Addr::UNSPECIFIED,
            resource_extension_pubkey: Some(resource_ext_pubkey),
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
