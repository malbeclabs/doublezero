use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_resource_extension_pda,
    processors::multicastgroup::activate::MulticastGroupActivateArgs, resource::ResourceType,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct ActivateMulticastGroupCommand {
    pub mgroup_pubkey: Pubkey,
    pub multicast_ip: Ipv4Addr,
    /// When true, SDK computes ResourceExtension PDA and includes it for on-chain allocation.
    /// When false, uses legacy behavior with caller-provided multicast_ip.
    pub use_onchain_allocation: bool,
}

impl ActivateMulticastGroupCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        // Build accounts list with optional ResourceExtension before payer
        // (payer and system_program are appended by execute_transaction)
        let mut accounts = vec![
            AccountMeta::new(self.mgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ];

        if self.use_onchain_allocation {
            let (resource_ext_pubkey, _, _) = get_resource_extension_pda(
                &client.get_program_id(),
                ResourceType::MulticastGroupBlock,
            );
            accounts.push(AccountMeta::new(resource_ext_pubkey, false));
        }

        client.execute_transaction(
            DoubleZeroInstruction::ActivateMulticastGroup(MulticastGroupActivateArgs {
                multicast_ip: self.multicast_ip,
            }),
            accounts,
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
            .expect_execute_transaction()
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
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = ActivateMulticastGroupCommand {
            mgroup_pubkey,
            multicast_ip: [1, 2, 3, 4].into(),
            use_onchain_allocation: false,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_multicastgroup_activate_with_onchain_allocation() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let (mgroup_pubkey, _) = get_multicastgroup_pda(&client.get_program_id(), 1);
        let (resource_ext_pubkey, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::MulticastGroupBlock);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::ActivateMulticastGroup(
                    MulticastGroupActivateArgs {
                        multicast_ip: Ipv4Addr::UNSPECIFIED,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(mgroup_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(resource_ext_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = ActivateMulticastGroupCommand {
            mgroup_pubkey,
            multicast_ip: Ipv4Addr::UNSPECIFIED,
            use_onchain_allocation: true,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
