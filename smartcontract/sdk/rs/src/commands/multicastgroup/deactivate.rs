use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_resource_extension_pda,
    processors::multicastgroup::closeaccount::MulticastGroupDeactivateArgs, resource::ResourceType,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeactivateMulticastGroupCommand {
    pub pubkey: Pubkey,
    pub owner: Pubkey,
    /// When true, SDK computes ResourceExtension PDAs and includes them for on-chain deallocation.
    /// When false, uses legacy behavior without resource deallocation.
    pub use_onchain_deallocation: bool,
}

impl DeactivateMulticastGroupCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let mut accounts = vec![
            AccountMeta::new(self.pubkey, false),
            AccountMeta::new(self.owner, false),
            AccountMeta::new(globalstate_pubkey, false),
        ];

        if self.use_onchain_deallocation {
            // Global MulticastGroupBlock (for multicast_ip deallocation)
            let (multicast_group_block_ext, _, _) = get_resource_extension_pda(
                &client.get_program_id(),
                ResourceType::MulticastGroupBlock,
            );
            accounts.push(AccountMeta::new(multicast_group_block_ext, false));
        }

        client.execute_transaction(
            DoubleZeroInstruction::DeactivateMulticastGroup(MulticastGroupDeactivateArgs {
                use_onchain_deallocation: self.use_onchain_deallocation,
            }),
            accounts,
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::multicastgroup::deactivate::DeactivateMulticastGroupCommand,
        tests::utils::create_test_client, DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_location_pda, get_resource_extension_pda},
        processors::multicastgroup::closeaccount::MulticastGroupDeactivateArgs,
        resource::ResourceType,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, signature::Signature};

    #[test]
    fn test_commands_multicastgroup_deactivate_without_resource_extension() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, _) = get_location_pda(&client.get_program_id(), 1);
        let payer = client.get_payer();

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeactivateMulticastGroup(
                    MulticastGroupDeactivateArgs {
                        use_onchain_deallocation: false,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(payer, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = DeactivateMulticastGroupCommand {
            pubkey: pda_pubkey,
            owner: payer,
            use_onchain_deallocation: false,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_multicastgroup_deactivate_with_onchain_deallocation() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, _) = get_location_pda(&client.get_program_id(), 1);
        let payer = client.get_payer();

        // Compute ResourceExtension PDA
        let (multicast_group_block_ext, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::MulticastGroupBlock);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeactivateMulticastGroup(
                    MulticastGroupDeactivateArgs {
                        use_onchain_deallocation: true,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(payer, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(multicast_group_block_ext, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = DeactivateMulticastGroupCommand {
            pubkey: pda_pubkey,
            owner: payer,
            use_onchain_deallocation: true,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
