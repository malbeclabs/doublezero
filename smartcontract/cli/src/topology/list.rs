use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::commands::{link::list::ListLinkCommand, topology::list::ListTopologyCommand};
use serde::Serialize;
use std::io::Write;

#[derive(Args, Debug)]
pub struct ListTopologyCliCommand {
    /// Output as pretty JSON.
    #[arg(long, default_value_t = false)]
    pub json: bool,
}

#[derive(Serialize)]
pub struct TopologyDisplay {
    pub name: String,
    pub bit: u8,
    pub algo: u8,
    pub color: u16,
    pub constraint: String,
    pub links: usize,
}

impl ListTopologyCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let topologies = client.list_topology(ListTopologyCommand)?;

        if topologies.is_empty() {
            writeln!(out, "No topologies found.")?;
            return Ok(());
        }

        let links = client.list_link(ListLinkCommand)?;

        let mut entries: Vec<_> = topologies.into_iter().collect();
        entries.sort_by_key(|(_, t)| t.admin_group_bit);

        let displays: Vec<TopologyDisplay> = entries
            .iter()
            .map(|(pda, t)| {
                let link_count = links
                    .values()
                    .filter(|link| link.link_topologies.contains(pda))
                    .count();
                TopologyDisplay {
                    name: t.name.clone(),
                    bit: t.admin_group_bit,
                    algo: t.flex_algo_number,
                    color: t.admin_group_bit as u16 + 1,
                    constraint: format!("{:?}", t.constraint),
                    links: link_count,
                }
            })
            .collect();

        if self.json {
            serde_json::to_writer_pretty(&mut *out, &displays)?;
            writeln!(out)?;
            return Ok(());
        }

        writeln!(
            out,
            "{:<32}  {:>3}  {:>4}  {:>5}  {:>5}  {:?}",
            "NAME", "BIT", "ALGO", "COLOR", "LINKS", "CONSTRAINT"
        )?;
        for d in &displays {
            writeln!(
                out,
                "{:<32}  {:>3}  {:>4}  {:>5}  {:>5}  {}",
                d.name, d.bit, d.algo, d.color, d.links, d.constraint,
            )?;
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{doublezerocommand::MockCliCommand, tests::utils::create_test_client};
    use doublezero_sdk::{get_topology_pda, Link, LinkLinkType, LinkStatus};
    use doublezero_serviceability::state::{
        accounttype::AccountType,
        link::{LinkDesiredStatus, LinkHealth},
        topology::{TopologyConstraint, TopologyInfo},
    };
    use solana_sdk::pubkey::Pubkey;
    use std::{collections::HashMap, io::Cursor};

    #[test]
    fn test_list_topology_empty() {
        let mut mock = MockCliCommand::new();

        mock.expect_list_topology()
            .with(mockall::predicate::eq(ListTopologyCommand))
            .returning(|_| Ok(HashMap::new()));

        let cmd = ListTopologyCliCommand { json: false };
        let mut out = Cursor::new(Vec::new());
        let result = cmd.execute(&mock, &mut out);
        assert!(result.is_ok());
        let output = String::from_utf8(out.into_inner()).unwrap();
        assert!(output.contains("No topologies found."));
    }

    #[test]
    fn test_list_topology_with_entries() {
        let mut client = create_test_client();

        let program_id = Pubkey::from_str_const("GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah");
        let (topology_pda, _) = get_topology_pda(&program_id, "unicast-default");

        let topology = TopologyInfo {
            account_type: AccountType::Topology,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            name: "unicast-default".to_string(),
            admin_group_bit: 0,
            flex_algo_number: 128,
            constraint: TopologyConstraint::IncludeAny,
        };

        client
            .expect_list_topology()
            .with(mockall::predicate::eq(ListTopologyCommand))
            .returning(move |_| {
                let mut map = HashMap::new();
                map.insert(topology_pda, topology.clone());
                Ok(map)
            });

        client.expect_list_link().returning(|_| Ok(HashMap::new()));

        let cmd = ListTopologyCliCommand { json: false };
        let mut out = Cursor::new(Vec::new());
        let result = cmd.execute(&client, &mut out);
        assert!(result.is_ok());
        let output = String::from_utf8(out.into_inner()).unwrap();
        assert!(output.contains("unicast-default"));
        assert!(output.contains("128"));
        assert!(output.contains("LINKS"));
    }

    #[test]
    fn test_list_topology_link_count() {
        let mut client = create_test_client();

        let program_id = Pubkey::from_str_const("GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah");
        let (topology_pda, _) = get_topology_pda(&program_id, "unicast-default");

        let topology = TopologyInfo {
            account_type: AccountType::Topology,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            name: "unicast-default".to_string(),
            admin_group_bit: 0,
            flex_algo_number: 128,
            constraint: TopologyConstraint::IncludeAny,
        };

        client
            .expect_list_topology()
            .with(mockall::predicate::eq(ListTopologyCommand))
            .returning(move |_| {
                let mut map = HashMap::new();
                map.insert(topology_pda, topology.clone());
                Ok(map)
            });

        let link = Link {
            account_type: AccountType::Link,
            index: 1,
            bump_seed: 2,
            code: "link1".to_string(),
            contributor_pk: Pubkey::new_unique(),
            side_a_pk: Pubkey::new_unique(),
            side_z_pk: Pubkey::new_unique(),
            link_type: LinkLinkType::WAN,
            bandwidth: 10_000_000_000,
            mtu: 4500,
            delay_ns: 0,
            jitter_ns: 0,
            delay_override_ns: 0,
            tunnel_id: 1,
            tunnel_net: "10.0.0.0/30".parse().unwrap(),
            status: LinkStatus::Activated,
            owner: Pubkey::new_unique(),
            side_a_iface_name: "eth0".to_string(),
            side_z_iface_name: "eth1".to_string(),
            link_health: LinkHealth::ReadyForService,
            desired_status: LinkDesiredStatus::Activated,
            link_topologies: vec![topology_pda],
            link_flags: 0,
        };

        client.expect_list_link().returning(move |_| {
            let mut links = HashMap::new();
            links.insert(Pubkey::new_unique(), link.clone());
            Ok(links)
        });

        let cmd = ListTopologyCliCommand { json: false };
        let mut out = Cursor::new(Vec::new());
        let result = cmd.execute(&client, &mut out);
        assert!(result.is_ok());
        let output = String::from_utf8(out.into_inner()).unwrap();
        assert!(
            output.contains("    1"),
            "expected link count 1 in output: {output}"
        );
    }

    #[test]
    fn test_list_topology_json_output() {
        let mut client = create_test_client();

        let program_id = Pubkey::from_str_const("GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah");
        let (topology_pda, _) = get_topology_pda(&program_id, "unicast-default");

        let topology = TopologyInfo {
            account_type: AccountType::Topology,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            name: "unicast-default".to_string(),
            admin_group_bit: 0,
            flex_algo_number: 128,
            constraint: TopologyConstraint::IncludeAny,
        };

        client
            .expect_list_topology()
            .with(mockall::predicate::eq(ListTopologyCommand))
            .returning(move |_| {
                let mut map = HashMap::new();
                map.insert(topology_pda, topology.clone());
                Ok(map)
            });

        client.expect_list_link().returning(|_| Ok(HashMap::new()));

        let cmd = ListTopologyCliCommand { json: true };
        let mut out = Cursor::new(Vec::new());
        let result = cmd.execute(&client, &mut out);
        assert!(result.is_ok());
        let output = String::from_utf8(out.into_inner()).unwrap();
        let parsed: serde_json::Value = serde_json::from_str(&output).expect("valid JSON");
        assert!(parsed.is_array());
        let arr = parsed.as_array().unwrap();
        assert_eq!(arr.len(), 1);
        assert_eq!(arr[0]["name"], "unicast-default");
        assert_eq!(arr[0]["links"], 0);
    }
}
