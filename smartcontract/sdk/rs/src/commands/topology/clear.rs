use crate::DoubleZeroClient;
use doublezero_serviceability::processors::topology::clear::TopologyClearArgs;
use doublezero_serviceability_instruction::topology::clear_topology_batched;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

/// Max link accounts per clear transaction. Solana caps transactions at 32
/// accounts; with 2 fixed accounts (topology PDA, globalstate) plus the payer
/// and system_program appended by the builder, we stay well under that limit at
/// 16 (same constant as backfill).
pub use doublezero_serviceability_instruction::topology::CLEAR_BATCH_SIZE;

#[derive(Debug, PartialEq, Clone)]
pub struct ClearTopologyCommand {
    pub name: String,
    pub link_pubkeys: Vec<Pubkey>,
}

impl ClearTopologyCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Vec<Signature>> {
        // The builder derives the topology and globalstate PDAs and chunks the
        // link list into per-transaction batches (empty links -> empty vec).
        let ixs = clear_topology_batched(
            &client.get_program_id(),
            &client.get_payer(),
            &self.link_pubkeys,
            TopologyClearArgs {
                name: self.name.clone(),
            },
        );

        let mut signatures = Vec::new();
        for ix in ixs {
            signatures.push(client.send_transaction(ix)?);
        }

        Ok(signatures)
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::topology::clear::ClearTopologyCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::processors::topology::clear::TopologyClearArgs;
    use doublezero_serviceability_instruction::topology::clear_topology_batched;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

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

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let link1 = Pubkey::new_unique();
        let link2 = Pubkey::new_unique();

        let expected = clear_topology_batched(
            &program_id,
            &payer,
            &[link1, link2],
            TopologyClearArgs {
                name: "my-topology".to_string(),
            },
        );
        assert_eq!(expected.len(), 1);
        client
            .expect_send_transaction()
            .with(predicate::eq(expected[0].clone()))
            .returning(|_| Ok(Signature::new_unique()));

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

        let program_id = client.get_program_id();
        let payer = client.get_payer();

        let links: Vec<Pubkey> = (0..33).map(|_| Pubkey::new_unique()).collect();

        // 33 links -> 3 chunks (16 + 16 + 1). Register each expected instruction.
        let expected = clear_topology_batched(
            &program_id,
            &payer,
            &links,
            TopologyClearArgs {
                name: "my-topology".to_string(),
            },
        );
        assert_eq!(expected.len(), 3);
        for ix in expected {
            client
                .expect_send_transaction()
                .times(1)
                .with(predicate::eq(ix))
                .returning(|_| Ok(Signature::new_unique()));
        }

        let res = ClearTopologyCommand {
            name: "my-topology".to_string(),
            link_pubkeys: links,
        }
        .execute(&client);

        assert_eq!(res.unwrap().len(), 3);
    }
}
