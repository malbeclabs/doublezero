use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    processors::topology::clear::{clear_topology_account_metas, TopologyClearArgs},
};
use solana_sdk::{pubkey::Pubkey, signature::Signature};

/// Max link accounts per clear transaction. The binding constraint is the 1232-byte
/// packet limit on a legacy transaction (~35 account keys at 32 bytes each), not
/// `MAX_TX_ACCOUNT_LOCKS` (128); with 2 fixed accounts (topology PDA, globalstate)
/// plus the payer, system_program, and optional Permission account appended by the
/// client, 16 stays well clear (same constant as assign).
pub const CLEAR_BATCH_SIZE: usize = 16;

#[derive(Debug, PartialEq, Clone)]
pub struct ClearTopologyCommand {
    pub name: String,
    pub link_pubkeys: Vec<Pubkey>,
}

impl ClearTopologyCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Vec<Signature>> {
        // Pre-flight only: the builder derives the globalstate PDA itself.
        GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let mut signatures = Vec::new();
        for chunk in self.link_pubkeys.chunks(CLEAR_BATCH_SIZE) {
            // payer and system_program are appended by execute_authorized_transaction
            // after the variable-length link list, so they are not listed here.
            let accounts =
                clear_topology_account_metas(&client.get_program_id(), &self.name, chunk);

            let sig = client.execute_authorized_transaction(
                DoubleZeroInstruction::ClearTopology(TopologyClearArgs {
                    name: self.name.clone(),
                }),
                accounts,
            )?;
            signatures.push(sig);
        }

        Ok(signatures)
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::topology::clear::{ClearTopologyCommand, CLEAR_BATCH_SIZE},
        tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_topology_pda},
        processors::topology::clear::TopologyClearArgs,
    };
    use mockall::{predicate, Sequence};
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_topology_clear_command_no_links_sends_no_tx() {
        let client = create_test_client();

        let res = ClearTopologyCommand {
            name: "my-topology".to_string(),
            link_pubkeys: vec![],
        }
        .execute(&client);

        assert!(res.unwrap().is_empty());
    }

    #[test]
    fn test_commands_topology_clear_command_with_links() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let (topology_pda, _) = get_topology_pda(&client.get_program_id(), "my-topology");
        let link1 = Pubkey::new_unique();
        let link2 = Pubkey::new_unique();

        client
            .expect_execute_authorized_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::ClearTopology(TopologyClearArgs {
                    name: "my-topology".to_string(),
                })),
                // The topology PDA MUST be writable: the processor decrements its
                // reference_count for every link that drops a reference.
                predicate::eq(vec![
                    AccountMeta::new(topology_pda, false),
                    AccountMeta::new_readonly(globalstate_pubkey, false),
                    AccountMeta::new(link1, false),
                    AccountMeta::new(link2, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = ClearTopologyCommand {
            name: "my-topology".to_string(),
            link_pubkeys: vec![link1, link2],
        }
        .execute(&client);

        assert_eq!(res.unwrap().len(), 1);
    }

    #[test]
    fn test_commands_topology_clear_batches_at_16() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let (topology_pda, _) = get_topology_pda(&client.get_program_id(), "my-topology");

        let links: Vec<Pubkey> = (0..33).map(|_| Pubkey::new_unique()).collect();

        let fixed_accounts = vec![
            AccountMeta::new(topology_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ];

        let expected_args = DoubleZeroInstruction::ClearTopology(TopologyClearArgs {
            name: "my-topology".to_string(),
        });

        let mut seq = Sequence::new();
        for chunk in links.chunks(CLEAR_BATCH_SIZE) {
            let mut expected_accounts = fixed_accounts.clone();
            for link_pk in chunk {
                expected_accounts.push(AccountMeta::new(*link_pk, false));
            }
            client
                .expect_execute_authorized_transaction()
                .times(1)
                .in_sequence(&mut seq)
                .with(
                    predicate::eq(expected_args.clone()),
                    predicate::eq(expected_accounts),
                )
                .returning(|_, _| Ok(Signature::new_unique()));
        }

        let res = ClearTopologyCommand {
            name: "my-topology".to_string(),
            link_pubkeys: links,
        }
        .execute(&client);

        assert_eq!(res.unwrap().len(), 3);
    }
}
