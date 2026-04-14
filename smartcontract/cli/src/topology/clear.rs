use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_sdk::commands::topology::clear::ClearTopologyCommand;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;

// Solana transactions have a 32-account limit. With 3 fixed accounts (topology PDA,
// globalstate, payer), we can fit at most 29 link accounts per transaction.
const CLEAR_BATCH_SIZE: usize = 29;

#[derive(Args, Debug)]
pub struct ClearTopologyCliCommand {
    /// Name of the topology to clear from links
    #[arg(long)]
    pub name: String,
    /// Comma-separated list of link pubkeys to clear the topology from.
    /// If omitted, all links currently tagged with this topology are discovered automatically.
    #[arg(long, value_delimiter = ',')]
    pub links: Vec<String>,
}

impl ClearTopologyCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let name = self.name.to_lowercase();

        let link_pubkeys: Vec<Pubkey> = if self.links.is_empty() {
            // Auto-discover: find all links tagged with this topology.
            let topology_map = client
                .list_topology(doublezero_sdk::commands::topology::list::ListTopologyCommand)?;
            let topology_pk = topology_map
                .iter()
                .find(|(_, t)| t.name.to_lowercase() == name)
                .map(|(pk, _)| *pk)
                .ok_or_else(|| eyre::eyre!("Topology '{}' not found", name))?;

            let links = client.list_link(doublezero_sdk::commands::link::list::ListLinkCommand)?;
            links
                .into_iter()
                .filter(|(_, link)| link.link_topologies.contains(&topology_pk))
                .map(|(pk, _)| pk)
                .collect()
        } else {
            self.links
                .iter()
                .map(|s| {
                    s.parse::<Pubkey>()
                        .map_err(|_| eyre::eyre!("invalid link pubkey: {}", s))
                })
                .collect::<eyre::Result<Vec<_>>>()?
        };

        let total = link_pubkeys.len();

        if total == 0 {
            writeln!(
                out,
                "No links tagged with topology '{}'. Nothing to clear.",
                name
            )?;
            return Ok(());
        }

        // Batch into chunks that fit within Solana's account limit.
        for chunk in link_pubkeys.chunks(CLEAR_BATCH_SIZE) {
            client.clear_topology(ClearTopologyCommand {
                name: name.clone(),
                link_pubkeys: chunk.to_vec(),
            })?;
        }

        writeln!(
            out,
            "Cleared topology '{}' from {} link(s).",
            name, total
        )?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::doublezerocommand::MockCliCommand;
    use doublezero_sdk::TopologyInfo;
    use mockall::predicate::eq;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::{collections::HashMap, io::Cursor};

    #[test]
    fn test_clear_topology_execute_no_links_auto_discover_empty() {
        let mut mock = MockCliCommand::new();
        let topology_pk = Pubkey::new_unique();

        let mut topology_map: HashMap<Pubkey, TopologyInfo> = HashMap::new();
        topology_map.insert(
            topology_pk,
            TopologyInfo {
                account_type: doublezero_sdk::AccountType::Topology,
                owner: Pubkey::default(),
                bump_seed: 0,
                name: "unicast-default".to_string(),
                admin_group_bit: 1,
                flex_algo_number: 129,
                constraint:
                    doublezero_serviceability::state::topology::TopologyConstraint::IncludeAny,
            },
        );

        mock.expect_check_requirements().returning(|_| Ok(()));
        mock.expect_list_topology()
            .returning(move |_| Ok(topology_map.clone()));
        mock.expect_list_link().returning(|_| Ok(HashMap::new()));

        let cmd = ClearTopologyCliCommand {
            name: "unicast-default".to_string(),
            links: vec![],
        };
        let mut out = Cursor::new(Vec::new());
        let result = cmd.execute(&mock, &mut out);
        assert!(result.is_ok());
        let output = String::from_utf8(out.into_inner()).unwrap();
        assert!(output.contains("No links tagged with topology 'unicast-default'."));
    }

    #[test]
    fn test_clear_topology_execute_with_links() {
        let mut mock = MockCliCommand::new();
        let link1 = Pubkey::new_unique();
        let link2 = Pubkey::new_unique();

        mock.expect_check_requirements().returning(|_| Ok(()));
        mock.expect_clear_topology()
            .with(eq(ClearTopologyCommand {
                name: "unicast-default".to_string(),
                link_pubkeys: vec![link1, link2],
            }))
            .returning(|_| Ok(Signature::new_unique()));

        let cmd = ClearTopologyCliCommand {
            name: "unicast-default".to_string(),
            links: vec![link1.to_string(), link2.to_string()],
        };
        let mut out = Cursor::new(Vec::new());
        let result = cmd.execute(&mock, &mut out);
        assert!(result.is_ok());
        let output = String::from_utf8(out.into_inner()).unwrap();
        assert!(output.contains("Cleared topology 'unicast-default' from 2 link(s)."));
    }

    #[test]
    fn test_clear_topology_invalid_pubkey() {
        let mut mock = MockCliCommand::new();

        mock.expect_check_requirements().returning(|_| Ok(()));

        let cmd = ClearTopologyCliCommand {
            name: "unicast-default".to_string(),
            links: vec!["not-a-pubkey".to_string()],
        };
        let mut out = Cursor::new(Vec::new());
        let result = cmd.execute(&mock, &mut out);
        assert!(result.is_err());
    }

    #[test]
    fn test_clear_topology_auto_discover_not_found() {
        let mut mock = MockCliCommand::new();

        mock.expect_check_requirements().returning(|_| Ok(()));
        mock.expect_list_topology()
            .returning(|_| Ok(HashMap::new()));

        let cmd = ClearTopologyCliCommand {
            name: "nonexistent".to_string(),
            links: vec![],
        };
        let mut out = Cursor::new(Vec::new());
        let result = cmd.execute(&mock, &mut out);
        assert!(result.is_err());
        assert!(result
            .unwrap_err()
            .to_string()
            .contains("Topology 'nonexistent' not found"));
    }
}
