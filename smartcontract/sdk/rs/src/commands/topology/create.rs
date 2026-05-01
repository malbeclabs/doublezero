use crate::{
    commands::{
        device::list::ListDeviceCommand, globalstate::get::GetGlobalStateCommand,
        topology::assign_node_segments::AssignTopologyNodeSegmentsCommand,
    },
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_resource_extension_pda, get_topology_pda},
    processors::topology::create::TopologyCreateArgs,
    resource::ResourceType,
    state::{interface::LoopbackType, topology::TopologyConstraint},
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct CreateTopologyCommand {
    pub name: String,
    pub constraint: TopologyConstraint,
}

#[derive(Debug, Clone, PartialEq)]
pub struct CreateTopologyResult {
    pub signature: Signature,
    pub topology_pda: Pubkey,
    pub backfill_signatures: Vec<Signature>,
}

impl CreateTopologyCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<CreateTopologyResult> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (topology_pda, _) = get_topology_pda(&client.get_program_id(), &self.name);
        let (admin_group_bits_pda, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::AdminGroupBits);

        // Pre-flight: verify admin-group-bits resource account exists
        client.get_account(admin_group_bits_pda).map_err(|_| {
            eyre::eyre!(
                "admin-group-bits resource account not found ({}). \
                Run 'doublezero resource create --resource-type admin-group-bits' first.",
                admin_group_bits_pda
            )
        })?;

        let signature = client.execute_transaction(
            DoubleZeroInstruction::CreateTopology(TopologyCreateArgs {
                name: self.name.clone(),
                constraint: self.constraint,
            }),
            vec![
                AccountMeta::new(topology_pda, false),
                AccountMeta::new(admin_group_bits_pda, false),
                AccountMeta::new_readonly(globalstate_pubkey, false),
            ],
        )?;

        // Enumerate devices and backfill FlexAlgoNodeSegment entries on Vpnv4
        // loopbacks. Mirrors the processor's filter at
        // programs/doublezero-serviceability/src/processors/topology/backfill.rs
        let devices = ListDeviceCommand.execute(client)?;
        let mut device_pubkeys: Vec<Pubkey> = devices
            .into_iter()
            .filter(|(_, device)| {
                device
                    .interfaces
                    .iter()
                    .any(|i| i.into_current_version().loopback_type == LoopbackType::Vpnv4)
            })
            .map(|(pk, _)| pk)
            .collect();
        device_pubkeys.sort();

        let backfill_signatures = AssignTopologyNodeSegmentsCommand {
            name: self.name.clone(),
            device_pubkeys,
        }
        .execute(client)?;

        Ok(CreateTopologyResult {
            signature,
            topology_pda,
            backfill_signatures,
        })
    }
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use crate::{
        commands::topology::create::CreateTopologyCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_resource_extension_pda, get_topology_pda},
        processors::topology::{
            assign_node_segments::AssignTopologyNodeSegmentsArgs, create::TopologyCreateArgs,
        },
        resource::ResourceType,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            device::Device,
            interface::{Interface, InterfaceV3, LoopbackType},
            topology::TopologyConstraint,
        },
    };
    use mockall::{predicate, Sequence};
    use solana_sdk::{
        account::Account, instruction::AccountMeta, pubkey::Pubkey, signature::Signature,
    };

    #[test]
    fn test_commands_topology_create_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let (topology_pda, _) = get_topology_pda(&client.get_program_id(), "unicast-default");
        let (admin_group_bits_pda, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::AdminGroupBits);

        client
            .expect_get_account()
            .with(predicate::eq(admin_group_bits_pda))
            .returning(|_| Ok(Account::default()));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::CreateTopology(TopologyCreateArgs {
                    name: "unicast-default".to_string(),
                    constraint: TopologyConstraint::IncludeAny,
                })),
                predicate::eq(vec![
                    AccountMeta::new(topology_pda, false),
                    AccountMeta::new(admin_group_bits_pda, false),
                    AccountMeta::new_readonly(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        client
            .expect_gets()
            .with(predicate::eq(AccountType::Device))
            .returning(|_| Ok(HashMap::new()));

        let res = CreateTopologyCommand {
            name: "unicast-default".to_string(),
            constraint: TopologyConstraint::IncludeAny,
        }
        .execute(&client);

        assert!(res.is_ok());
        let result = res.unwrap();
        assert_eq!(result.topology_pda, topology_pda);
        assert!(result.backfill_signatures.is_empty());
    }

    #[test]
    fn test_commands_topology_create_runs_backfill_for_vpnv4_devices() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let (topology_pda, _) = get_topology_pda(&client.get_program_id(), "algo128");
        let (admin_group_bits_pda, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::AdminGroupBits);
        let (sr_ids_pda, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::SegmentRoutingIds);

        let vpnv4_device_pk = Pubkey::new_unique();
        let vpnv4_device = Device {
            interfaces: vec![Interface::V3(InterfaceV3 {
                loopback_type: LoopbackType::Vpnv4,
                ..Default::default()
            })],
            ..Default::default()
        };

        let other_device_pk = Pubkey::new_unique();
        let other_device = Device {
            interfaces: vec![Interface::V3(InterfaceV3 {
                loopback_type: LoopbackType::None,
                ..Default::default()
            })],
            ..Default::default()
        };

        client
            .expect_get_account()
            .with(predicate::eq(admin_group_bits_pda))
            .returning(|_| Ok(Account::default()));

        let mut seq = Sequence::new();

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::CreateTopology(TopologyCreateArgs {
                    name: "algo128".to_string(),
                    constraint: TopologyConstraint::IncludeAny,
                })),
                predicate::eq(vec![
                    AccountMeta::new(topology_pda, false),
                    AccountMeta::new(admin_group_bits_pda, false),
                    AccountMeta::new_readonly(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        client
            .expect_gets()
            .with(predicate::eq(AccountType::Device))
            .returning(move |_| {
                let mut devices = HashMap::new();
                devices.insert(vpnv4_device_pk, AccountData::Device(vpnv4_device.clone()));
                devices.insert(other_device_pk, AccountData::Device(other_device.clone()));
                Ok(devices)
            });

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::AssignTopologyNodeSegments(
                    AssignTopologyNodeSegmentsArgs {
                        name: "algo128".to_string(),
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new_readonly(topology_pda, false),
                    AccountMeta::new(sr_ids_pda, false),
                    AccountMeta::new_readonly(globalstate_pubkey, false),
                    AccountMeta::new(vpnv4_device_pk, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = CreateTopologyCommand {
            name: "algo128".to_string(),
            constraint: TopologyConstraint::IncludeAny,
        }
        .execute(&client);

        let result = res.unwrap();
        assert_eq!(result.topology_pda, topology_pda);
        assert_eq!(result.backfill_signatures.len(), 1);
    }
}
