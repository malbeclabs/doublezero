use crate::DoubleZeroClient;
use doublezero_serviceability::processors::topology::assign_node_segments::AssignTopologyNodeSegmentsArgs;
use doublezero_serviceability_instruction::topology::assign_topology_node_segments_batched;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

/// Max device accounts per backfill transaction. Solana caps transactions at
/// 32 accounts; with 5 non-device accounts (3 fixed PDAs + payer + system_program
/// appended by the builder) we stay well under that limit at 4.
pub use doublezero_serviceability_instruction::topology::BACKFILL_BATCH_SIZE;

#[derive(Debug, PartialEq, Clone)]
pub struct AssignTopologyNodeSegmentsCommand {
    pub name: String,
    pub device_pubkeys: Vec<Pubkey>,
}

impl AssignTopologyNodeSegmentsCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Vec<Signature>> {
        // The builder derives the topology, segment-routing-ids and globalstate
        // PDAs and chunks the device list into per-transaction batches (empty
        // devices -> empty vec).
        let ixs = assign_topology_node_segments_batched(
            &client.get_program_id(),
            &client.get_payer(),
            &self.device_pubkeys,
            AssignTopologyNodeSegmentsArgs {
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
        commands::topology::assign_node_segments::AssignTopologyNodeSegmentsCommand,
        tests::utils::create_test_client, DoubleZeroClient,
    };
    use doublezero_serviceability::processors::topology::assign_node_segments::AssignTopologyNodeSegmentsArgs;
    use doublezero_serviceability_instruction::topology::assign_topology_node_segments_batched;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_topology_backfill_no_devices_sends_no_tx() {
        let client = create_test_client();

        let res = AssignTopologyNodeSegmentsCommand {
            name: "unicast-default".to_string(),
            device_pubkeys: vec![],
        }
        .execute(&client);

        assert!(res.unwrap().is_empty());
    }

    #[test]
    fn test_commands_topology_backfill_with_devices() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let device1 = Pubkey::new_unique();
        let device2 = Pubkey::new_unique();

        let expected = assign_topology_node_segments_batched(
            &program_id,
            &payer,
            &[device1, device2],
            AssignTopologyNodeSegmentsArgs {
                name: "algo128".to_string(),
            },
        );
        assert_eq!(expected.len(), 1);
        client
            .expect_send_transaction()
            .with(predicate::eq(expected[0].clone()))
            .returning(|_| Ok(Signature::new_unique()));

        let res = AssignTopologyNodeSegmentsCommand {
            name: "algo128".to_string(),
            device_pubkeys: vec![device1, device2],
        }
        .execute(&client);

        assert_eq!(res.unwrap().len(), 1);
    }

    #[test]
    fn test_commands_topology_backfill_batches_at_16() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();

        let devices: Vec<Pubkey> = (0..33).map(|_| Pubkey::new_unique()).collect();

        // 33 devices -> 9 chunks (8 * 4 + 1). Register each expected instruction.
        let expected = assign_topology_node_segments_batched(
            &program_id,
            &payer,
            &devices,
            AssignTopologyNodeSegmentsArgs {
                name: "algo128".to_string(),
            },
        );
        assert_eq!(expected.len(), 9);
        for ix in expected {
            client
                .expect_send_transaction()
                .times(1)
                .with(predicate::eq(ix))
                .returning(|_| Ok(Signature::new_unique()));
        }

        let res = AssignTopologyNodeSegmentsCommand {
            name: "algo128".to_string(),
            device_pubkeys: devices,
        }
        .execute(&client);

        assert_eq!(res.unwrap().len(), 9);
    }
}
