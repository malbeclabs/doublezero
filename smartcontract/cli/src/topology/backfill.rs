use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_sdk::commands::topology::backfill::BackfillTopologyCommand;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;

#[derive(Args, Debug)]
pub struct BackfillTopologyCliCommand {
    /// Name of the topology to backfill
    #[arg(long)]
    pub name: String,
    /// Device account pubkeys to backfill (one or more)
    #[arg(long = "device", value_name = "PUBKEY")]
    pub device_pubkeys: Vec<Pubkey>,
}

impl BackfillTopologyCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        if self.device_pubkeys.is_empty() {
            return Err(eyre::eyre!(
                "at least one --device pubkey is required for backfill"
            ));
        }

        let sigs = client.backfill_topology(BackfillTopologyCommand {
            name: self.name.clone(),
            device_pubkeys: self.device_pubkeys,
        })?;

        writeln!(
            out,
            "Backfilled topology '{}' across {} transaction(s).",
            self.name,
            sigs.len()
        )?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::doublezerocommand::MockCliCommand;
    use doublezero_sdk::commands::topology::backfill::BackfillTopologyCommand;
    use mockall::predicate::eq;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::io::Cursor;

    #[test]
    fn test_backfill_topology_execute_success() {
        let mut mock = MockCliCommand::new();
        let device1 = Pubkey::new_unique();

        mock.expect_check_requirements().returning(|_| Ok(()));
        mock.expect_backfill_topology()
            .with(eq(BackfillTopologyCommand {
                name: "unicast-default".to_string(),
                device_pubkeys: vec![device1],
            }))
            .returning(|_| Ok(vec![Signature::new_unique()]));

        let cmd = BackfillTopologyCliCommand {
            name: "unicast-default".to_string(),
            device_pubkeys: vec![device1],
        };
        let mut out = Cursor::new(Vec::new());
        let result = cmd.execute(&mock, &mut out);
        assert!(result.is_ok());
        let output = String::from_utf8(out.into_inner()).unwrap();
        assert!(output.contains("Backfilled topology 'unicast-default' across 1 transaction(s)."));
    }

    #[test]
    fn test_backfill_topology_requires_at_least_one_device() {
        let mut mock = MockCliCommand::new();
        mock.expect_check_requirements().returning(|_| Ok(()));

        let cmd = BackfillTopologyCliCommand {
            name: "unicast-default".to_string(),
            device_pubkeys: vec![],
        };
        let mut out = Cursor::new(Vec::new());
        let result = cmd.execute(&mock, &mut out);
        assert!(result.is_err());
        assert!(result
            .unwrap_err()
            .to_string()
            .contains("at least one --device pubkey is required"));
    }
}
