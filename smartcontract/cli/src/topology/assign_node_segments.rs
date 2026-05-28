use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_sdk::commands::topology::assign_node_segments::AssignTopologyNodeSegmentsCommand;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;

#[derive(Args, Debug)]
pub struct AssignTopologyNodeSegmentsCliCommand {
    /// Name of the topology to assign node segments for
    #[arg(long)]
    pub name: String,
    /// Device account pubkeys (one or more)
    #[arg(long = "device", value_name = "PUBKEY")]
    pub device_pubkeys: Vec<Pubkey>,
}

impl AssignTopologyNodeSegmentsCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        if self.device_pubkeys.is_empty() {
            return Err(eyre::eyre!("at least one --device pubkey is required"));
        }

        let name = self.name.to_uppercase();

        let sigs = client.assign_topology_node_segments(AssignTopologyNodeSegmentsCommand {
            name: name.clone(),
            device_pubkeys: self.device_pubkeys,
        })?;

        writeln!(
            out,
            "Assigned node segments for topology '{}' across {} transaction(s).",
            name,
            sigs.len()
        )?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use doublezero_cli_core::testing::cli_context_default_for_tests;
    use tokio::runtime::Builder;

    fn block_on<F: std::future::Future>(f: F) -> F::Output {
        Builder::new_current_thread()
            .enable_all()
            .build()
            .unwrap()
            .block_on(f)
    }

    use super::*;
    use crate::doublezerocommand::MockCliCommand;
    use doublezero_sdk::commands::topology::assign_node_segments::AssignTopologyNodeSegmentsCommand;
    use mockall::predicate::eq;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::io::Cursor;

    #[test]
    fn test_assign_topology_node_segments_execute_success() {
        let mut mock = MockCliCommand::new();
        let device1 = Pubkey::new_unique();

        mock.expect_check_requirements().returning(|_| Ok(()));
        mock.expect_assign_topology_node_segments()
            .with(eq(AssignTopologyNodeSegmentsCommand {
                name: "UNICAST-DEFAULT".to_string(),
                device_pubkeys: vec![device1],
            }))
            .returning(|_| Ok(vec![Signature::new_unique()]));

        let cmd = AssignTopologyNodeSegmentsCliCommand {
            name: "unicast-default".to_string(),
            device_pubkeys: vec![device1],
        };
        let ctx = cli_context_default_for_tests();
        let mut out = Cursor::new(Vec::new());
        let result = block_on(cmd.execute(&ctx, &mock, &mut out));
        assert!(result.is_ok());
        let output = String::from_utf8(out.into_inner()).unwrap();
        assert!(output.contains(
            "Assigned node segments for topology 'UNICAST-DEFAULT' across 1 transaction(s)."
        ));
    }

    #[test]
    fn test_assign_topology_node_segments_requires_at_least_one_device() {
        let mut mock = MockCliCommand::new();
        mock.expect_check_requirements().returning(|_| Ok(()));

        let cmd = AssignTopologyNodeSegmentsCliCommand {
            name: "unicast-default".to_string(),
            device_pubkeys: vec![],
        };
        let ctx = cli_context_default_for_tests();
        let mut out = Cursor::new(Vec::new());
        let result = block_on(cmd.execute(&ctx, &mock, &mut out));
        assert!(result.is_err());
        assert!(result
            .unwrap_err()
            .to_string()
            .contains("at least one --device pubkey is required"));
    }
}
