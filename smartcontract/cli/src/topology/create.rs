use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_sdk::commands::topology::create::CreateTopologyCommand;
use doublezero_serviceability::state::topology::TopologyConstraint;
use std::io::Write;

#[derive(Args, Debug)]
pub struct CreateTopologyCliCommand {
    /// Name of the topology (max 32 bytes)
    #[arg(long)]
    pub name: String,
    /// Constraint type: include-any or exclude
    #[arg(long, value_parser = parse_constraint)]
    pub constraint: TopologyConstraint,
}

fn parse_constraint(s: &str) -> Result<TopologyConstraint, String> {
    match s {
        "include-any" => Ok(TopologyConstraint::IncludeAny),
        "exclude" => Ok(TopologyConstraint::Exclude),
        _ => Err(format!(
            "invalid constraint '{}': expected 'include-any' or 'exclude'",
            s
        )),
    }
}

impl CreateTopologyCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        if self.name.len() > 32 {
            eyre::bail!(
                "topology name must be 32 characters or fewer (got {})",
                self.name.len()
            );
        }

        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let result = client.create_topology(CreateTopologyCommand {
            name: self.name.clone(),
            constraint: self.constraint,
        })?;
        writeln!(
            out,
            "Created topology '{}' successfully. PDA: {}. Backfilled {} transaction(s).",
            self.name,
            result.topology_pda,
            result.backfill_signatures.len()
        )?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::doublezerocommand::MockCliCommand;
    use doublezero_sdk::commands::topology::create::CreateTopologyResult;
    use doublezero_serviceability::state::topology::TopologyConstraint;
    use mockall::predicate::eq;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::io::Cursor;

    #[test]
    fn test_create_topology_execute_success() {
        let mut mock = MockCliCommand::new();
        let topology_pda = Pubkey::new_unique();

        mock.expect_check_requirements().returning(|_| Ok(()));
        mock.expect_create_topology()
            .with(eq(CreateTopologyCommand {
                name: "unicast-default".to_string(),
                constraint: TopologyConstraint::IncludeAny,
            }))
            .returning(move |_| {
                Ok(CreateTopologyResult {
                    signature: Signature::new_unique(),
                    topology_pda,
                    backfill_signatures: vec![],
                })
            });

        let cmd = CreateTopologyCliCommand {
            name: "unicast-default".to_string(),
            constraint: TopologyConstraint::IncludeAny,
        };
        let mut out = Cursor::new(Vec::new());
        let result = cmd.execute(&mock, &mut out);
        assert!(result.is_ok());
        let output = String::from_utf8(out.into_inner()).unwrap();
        assert!(output.contains("Created topology 'unicast-default' successfully."));
        assert!(output.contains(&topology_pda.to_string()));
        assert!(output.contains("Backfilled 0 transaction(s)."));
    }

    #[test]
    fn test_parse_constraint_include_any() {
        assert_eq!(
            parse_constraint("include-any"),
            Ok(TopologyConstraint::IncludeAny)
        );
    }

    #[test]
    fn test_parse_constraint_exclude() {
        assert_eq!(parse_constraint("exclude"), Ok(TopologyConstraint::Exclude));
    }

    #[test]
    fn test_parse_constraint_invalid() {
        assert!(parse_constraint("unknown").is_err());
    }

    #[test]
    fn test_create_topology_name_too_long() {
        let cmd = CreateTopologyCliCommand {
            name: "a".repeat(33),
            constraint: TopologyConstraint::IncludeAny,
        };
        let mock = MockCliCommand::new();
        let mut out = Cursor::new(Vec::new());
        let result = cmd.execute(&mock, &mut out);
        assert!(result.is_err());
        assert!(result
            .unwrap_err()
            .to_string()
            .contains("32 characters or fewer"));
    }
}
