use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_topology_pda,
    processors::topology::clear::TopologyClearArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

/// Max link accounts per clear transaction. Solana caps transactions at 32
/// accounts; with 3 fixed accounts (topology PDA, globalstate, payer) we
/// stay well under that limit at 16 (same constant as backfill).
pub const CLEAR_BATCH_SIZE: usize = 16;

#[derive(Debug, PartialEq, Clone)]
pub struct ClearTopologyCommand {
    pub name: String,
    pub link_pubkeys: Vec<Pubkey>,
}

impl ClearTopologyCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Vec<Signature>> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (topology_pda, _) = get_topology_pda(&client.get_program_id(), &self.name);

        let payer = client.get_payer();

        let fixed_accounts = [
            AccountMeta::new_readonly(topology_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
            AccountMeta::new(payer, true),
        ];

        let mut signatures = Vec::new();
        for chunk in self.link_pubkeys.chunks(CLEAR_BATCH_SIZE) {
            let mut accounts = fixed_accounts.to_vec();
            for link_pk in chunk {
                accounts.push(AccountMeta::new(*link_pk, false));
            }

            let sig = client.execute_transaction(
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
        let payer = client.get_payer();
        let link1 = Pubkey::new_unique();
        let link2 = Pubkey::new_unique();

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::ClearTopology(TopologyClearArgs {
                    name: "my-topology".to_string(),
                })),
                predicate::eq(vec![
                    AccountMeta::new_readonly(topology_pda, false),
                    AccountMeta::new_readonly(globalstate_pubkey, false),
                    AccountMeta::new(payer, true),
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
        let payer = client.get_payer();

        let links: Vec<Pubkey> = (0..33).map(|_| Pubkey::new_unique()).collect();

        let fixed_accounts = vec![
            AccountMeta::new_readonly(topology_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
            AccountMeta::new(payer, true),
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
                .expect_execute_transaction()
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
