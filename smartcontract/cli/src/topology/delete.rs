use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_sdk::commands::topology::delete::DeleteTopologyCommand;
use std::io::Write;

#[derive(Args, Debug)]
pub struct DeleteTopologyCliCommand {
    /// Name of the topology to delete
    #[arg(long)]
    pub name: String,
}

impl DeleteTopologyCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        client.delete_topology(DeleteTopologyCommand {
            name: self.name.clone(),
        })?;
        writeln!(out, "Deleted topology '{}' successfully.", self.name)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::doublezerocommand::MockCliCommand;
    use mockall::predicate::eq;
    use solana_sdk::signature::Signature;
    use std::io::Cursor;

    #[test]
    fn test_delete_topology_execute_success() {
        let mut mock = MockCliCommand::new();

        mock.expect_check_requirements().returning(|_| Ok(()));
        mock.expect_delete_topology()
            .with(eq(DeleteTopologyCommand {
                name: "unicast-default".to_string(),
            }))
            .returning(|_| Ok(Signature::new_unique()));

        let cmd = DeleteTopologyCliCommand {
            name: "unicast-default".to_string(),
        };
        let mut out = Cursor::new(Vec::new());
        let result = cmd.execute(&mock, &mut out);
        assert!(result.is_ok());
        let output = String::from_utf8(out.into_inner()).unwrap();
        assert!(output.contains("Deleted topology 'unicast-default' successfully."));
    }
}
