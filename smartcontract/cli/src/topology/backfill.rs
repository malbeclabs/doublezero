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

        let sig = client.backfill_topology(BackfillTopologyCommand {
            name: self.name.clone(),
            device_pubkeys: self.device_pubkeys,
        })?;

        writeln!(
            out,
            "Backfilled topology '{}'. Signature: {}",
            self.name, sig
        )?;

        Ok(())
    }
}
