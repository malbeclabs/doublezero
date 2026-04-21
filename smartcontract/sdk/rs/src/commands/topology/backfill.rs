use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_resource_extension_pda, get_topology_pda},
    processors::topology::backfill::TopologyBackfillArgs,
    resource::ResourceType,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

/// Max device accounts per backfill transaction. Solana caps transactions at
/// 32 accounts; with 4 fixed accounts (topology PDA, segment_routing_ids PDA,
/// globalstate, payer) we stay well under that limit at 16.
pub const BACKFILL_BATCH_SIZE: usize = 16;

#[derive(Debug, PartialEq, Clone)]
pub struct BackfillTopologyCommand {
    pub name: String,
    pub device_pubkeys: Vec<Pubkey>,
}

impl BackfillTopologyCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Vec<Signature>> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (topology_pda, _) = get_topology_pda(&client.get_program_id(), &self.name);
        let (segment_routing_ids_pda, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::SegmentRoutingIds);

        let payer = client.get_payer();

        let fixed_accounts = [
            AccountMeta::new_readonly(topology_pda, false),
            AccountMeta::new(segment_routing_ids_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
            AccountMeta::new(payer, true),
        ];

        let mut signatures = Vec::new();
        for chunk in self.device_pubkeys.chunks(BACKFILL_BATCH_SIZE) {
            let mut accounts = fixed_accounts.to_vec();
            for device_pk in chunk {
                accounts.push(AccountMeta::new(*device_pk, false));
            }

            let sig = client.execute_transaction(
                DoubleZeroInstruction::BackfillTopology(TopologyBackfillArgs {
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
        commands::topology::backfill::{BackfillTopologyCommand, BACKFILL_BATCH_SIZE},
        tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_resource_extension_pda, get_topology_pda},
        processors::topology::backfill::TopologyBackfillArgs,
        resource::ResourceType,
    };
    use mockall::{predicate, Sequence};
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_topology_backfill_no_devices_sends_no_tx() {
        let client = create_test_client();

        let res = BackfillTopologyCommand {
            name: "unicast-default".to_string(),
            device_pubkeys: vec![],
        }
        .execute(&client);

        assert!(res.unwrap().is_empty());
    }

    #[test]
    fn test_commands_topology_backfill_with_devices() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let (topology_pda, _) = get_topology_pda(&client.get_program_id(), "algo128");
        let (sr_ids_pda, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::SegmentRoutingIds);
        let payer = client.get_payer();
        let device1 = Pubkey::new_unique();
        let device2 = Pubkey::new_unique();

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::BackfillTopology(
                    TopologyBackfillArgs {
                        name: "algo128".to_string(),
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new_readonly(topology_pda, false),
                    AccountMeta::new(sr_ids_pda, false),
                    AccountMeta::new_readonly(globalstate_pubkey, false),
                    AccountMeta::new(payer, true),
                    AccountMeta::new(device1, false),
                    AccountMeta::new(device2, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = BackfillTopologyCommand {
            name: "algo128".to_string(),
            device_pubkeys: vec![device1, device2],
        }
        .execute(&client);

        assert_eq!(res.unwrap().len(), 1);
    }

    #[test]
    fn test_commands_topology_backfill_batches_at_16() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let (topology_pda, _) = get_topology_pda(&client.get_program_id(), "algo128");
        let (sr_ids_pda, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::SegmentRoutingIds);
        let payer = client.get_payer();

        let devices: Vec<Pubkey> = (0..33).map(|_| Pubkey::new_unique()).collect();

        let fixed_accounts = vec![
            AccountMeta::new_readonly(topology_pda, false),
            AccountMeta::new(sr_ids_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
            AccountMeta::new(payer, true),
        ];

        let expected_args = DoubleZeroInstruction::BackfillTopology(TopologyBackfillArgs {
            name: "algo128".to_string(),
        });

        let mut seq = Sequence::new();
        for chunk in devices.chunks(BACKFILL_BATCH_SIZE) {
            let mut expected_accounts = fixed_accounts.clone();
            for device_pk in chunk {
                expected_accounts.push(AccountMeta::new(*device_pk, false));
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

        let res = BackfillTopologyCommand {
            name: "algo128".to_string(),
            device_pubkeys: devices,
        }
        .execute(&client);

        assert_eq!(res.unwrap().len(), 3);
    }
}
