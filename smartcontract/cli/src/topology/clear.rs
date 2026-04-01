use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_sdk::commands::topology::clear::ClearTopologyCommand;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;

#[derive(Args, Debug)]
pub struct ClearTopologyCliCommand {
    /// Name of the topology to clear from links
    #[arg(long)]
    pub name: String,
    /// Comma-separated list of link pubkeys to clear the topology from
    #[arg(long, value_delimiter = ',')]
    pub links: Vec<String>,
}

impl ClearTopologyCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let link_pubkeys: Vec<Pubkey> = self
            .links
            .iter()
            .map(|s| {
                s.parse::<Pubkey>()
                    .map_err(|_| eyre::eyre!("invalid link pubkey: {}", s))
            })
            .collect::<eyre::Result<Vec<_>>>()?;

        let n = link_pubkeys.len();
        client.clear_topology(ClearTopologyCommand {
            name: self.name.clone(),
            link_pubkeys,
        })?;
        writeln!(out, "Cleared topology '{}' from {} link(s).", self.name, n)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::doublezerocommand::MockCliCommand;
    use mockall::predicate::eq;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::io::Cursor;

    #[test]
    fn test_clear_topology_execute_no_links() {
        let mut mock = MockCliCommand::new();

        mock.expect_check_requirements().returning(|_| Ok(()));
        mock.expect_clear_topology()
            .with(eq(ClearTopologyCommand {
                name: "unicast-default".to_string(),
                link_pubkeys: vec![],
            }))
            .returning(|_| Ok(Signature::new_unique()));

        let cmd = ClearTopologyCliCommand {
            name: "unicast-default".to_string(),
            links: vec![],
        };
        let mut out = Cursor::new(Vec::new());
        let result = cmd.execute(&mock, &mut out);
        assert!(result.is_ok());
        let output = String::from_utf8(out.into_inner()).unwrap();
        assert!(output.contains("Cleared topology 'unicast-default' from 0 link(s)."));
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
}
