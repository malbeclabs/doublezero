use anyhow::{Context, Result};
use clap::Args;
use doublezero_solana_client_tools::{
    account::zero_copy::ZeroCopyAccountOwnedData,
    rpc::{SolanaConnection, SolanaConnectionOptions},
};
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
    pub connection_options: SolanaConnectionOptions,
}

impl ShowCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let connection: SolanaConnection = self.connection_options.into();
        let pda = find_validator_publisher_rewards_address(&self.node_id).0;

        let account = connection.0.get_account(&pda).await.with_context(|| {
            format!(
                "no validator publisher rewards account found for node {} (PDA {pda})",
                self.node_id
            )
        })?;

        let vpr: ZeroCopyAccountOwnedData<ValidatorPublisherRewards> =
            account.try_into().with_context(|| {
                format!("validator publisher rewards account at {pda} is malformed")
            })?;
        let owner = vpr.rewards_token_owner_key;
        let mint = vpr.rewards_token_mint_key;
        let ata = get_associated_token_address(&owner, &mint);

        println!("Node ID:        {}", vpr.node_id);
        println!("Rewards owner:  {owner}");
        println!("Rewards mint:   {mint}");
        println!("Resolved ATA:   {ata}");

        // Rewards won't be distributed unless the ATA exists. `configure`
        // creates it idempotently, so this is just a status readout.
        match connection.0.get_account(&ata).await {
            Ok(_) => println!("ATA status:     exists"),
            Err(_) => println!(
                "ATA status:     missing — rewards won't be distributed until it's created. \
                 Re-run `doublezero-solana shreds publisher-rewards configure` to create it, \
                 or run `spl-token create-account {mint} --owner {owner}` manually."
            ),
        }
        Ok(())
    }
}
