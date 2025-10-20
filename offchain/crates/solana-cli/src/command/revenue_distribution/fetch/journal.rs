use anyhow::{Context, Result};
use clap::Args;
use doublezero_program_tools::zero_copy;
use doublezero_revenue_distribution::state::Journal;
use doublezero_solana_client_tools::rpc::{SolanaConnection, SolanaConnectionOptions};

#[derive(Debug, Args)]
pub struct JournalCommand {
    #[command(flatten)]
    connection_options: SolanaConnectionOptions,
}

impl JournalCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let Self { connection_options } = self;

        let connection = SolanaConnection::try_from(connection_options)?;
        let journal_key = Journal::find_address().0;
        let journal_info = connection.get_account(&journal_key).await?;
        let (journal, _) =
            zero_copy::checked_from_bytes_with_discriminator::<Journal>(&journal_info.data)
                .context("Failed to deserialize journal")?;
        println!("Journal: {journal:?}");

        Ok(())
    }
}
