use std::io::Write;

use anyhow::{Context, Result};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_solana_client_tools::rpc::SolanaConnectionOptions;
use doublezero_solana_sdk::{
    Pubkey,
    shred_subscription::state::{
        ValidatorPublisherRewards, find_validator_publisher_rewards_address,
    },
};
use spl_associated_token_account_interface::address::get_associated_token_address;

/*
   doublezero-solana shreds publisher-rewards show --node-id <PUBKEY>
*/

#[derive(Debug, Args)]
pub struct ShowCommand {
    #[arg(long)]
    pub node_id: Pubkey,

    #[command(flatten)]
    connection_options: SolanaConnectionOptions,
}

impl ShowCommand {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        let connection = crate::command::solana_connection(ctx, &self.connection_options);
        let commitment = connection.0.commitment();
        let pda = find_validator_publisher_rewards_address(&self.node_id).0;

        // Distinguish RPC failure (propagated `?`) from absent account
        // ("Failed to fetch account {pda}").
        let vpr = connection
            .try_fetch_zero_copy_data_with_commitment::<ValidatorPublisherRewards>(&pda, commitment)
            .await
            .with_context(|| {
                format!(
                    "failed to read validator publisher rewards for node {} (PDA {pda})",
                    self.node_id
                )
            })?;
        let owner = vpr.rewards_token_owner_key;
        let mint = vpr.rewards_token_mint_key;
        let ata = get_associated_token_address(&owner, &mint);

        writeln!(out, "Node ID:        {}", vpr.node_id)?;
        writeln!(out, "Rewards owner:  {owner}")?;
        writeln!(out, "Rewards mint:   {mint}")?;
        writeln!(out, "Resolved ATA:   {ata}")?;

        // Rewards won't be distributed unless the ATA exists. `configure`
        // creates it idempotently, so this is a status line (None) rather
        // than an error. RPC failures propagate so a transient network blip
        // is not silently reported as "missing".
        let ata_account = connection
            .0
            .get_account_with_commitment(&ata, commitment)
            .await
            .with_context(|| format!("failed to query ATA {ata} status"))?
            .value;
        match ata_account {
            Some(_) => writeln!(out, "ATA status:     exists")?,
            None => writeln!(
                out,
                "ATA status:     missing — rewards won't be distributed until it's created. \
                 Re-run `doublezero-solana shreds publisher-rewards configure` to create it, \
                 or run `spl-token create-account {mint} --owner {owner}` manually."
            )?,
        }
        Ok(())
    }
}
