use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::commands::topology::list::ListTopologyCommand;
use std::io::Write;

#[derive(Args, Debug)]
pub struct ListTopologyCliCommand {}

impl ListTopologyCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let topologies = client.list_topology(ListTopologyCommand)?;

        if topologies.is_empty() {
            writeln!(out, "No topologies found.")?;
            return Ok(());
        }

        let mut entries: Vec<_> = topologies.into_values().collect();
        entries.sort_by_key(|t| t.admin_group_bit);

        writeln!(
            out,
            "{:<32}  {:>3}  {:>4}  {:>5}  {:?}",
            "NAME", "BIT", "ALGO", "COLOR", "CONSTRAINT"
        )?;
        for t in &entries {
            writeln!(
                out,
                "{:<32}  {:>3}  {:>4}  {:>5}  {:?}",
                t.name,
                t.admin_group_bit,
                t.flex_algo_number,
                t.admin_group_bit as u16 + 1,
                t.constraint
            )?;
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::doublezerocommand::MockCliCommand;
    use doublezero_serviceability::state::{
        accounttype::AccountType,
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

        let cmd = ListTopologyCliCommand {};
        let mut out = Cursor::new(Vec::new());
        let result = cmd.execute(&mock, &mut out);
        assert!(result.is_ok());
        let output = String::from_utf8(out.into_inner()).unwrap();
        assert!(output.contains("No topologies found."));
    }

    #[test]
    fn test_list_topology_with_entries() {
        let mut mock = MockCliCommand::new();

        let topology = TopologyInfo {
            account_type: AccountType::Topology,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            name: "unicast-default".to_string(),
            admin_group_bit: 0,
            flex_algo_number: 128,
            constraint: TopologyConstraint::IncludeAny,
        };

        mock.expect_list_topology()
            .with(mockall::predicate::eq(ListTopologyCommand))
            .returning(move |_| {
                let mut map = HashMap::new();
                map.insert(Pubkey::new_unique(), topology.clone());
                Ok(map)
            });

        let cmd = ListTopologyCliCommand {};
        let mut out = Cursor::new(Vec::new());
        let result = cmd.execute(&mock, &mut out);
        assert!(result.is_ok());
        let output = String::from_utf8(out.into_inner()).unwrap();
        assert!(output.contains("unicast-default"));
        assert!(output.contains("128"));
    }
}
