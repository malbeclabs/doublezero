use clap::Args;
use doublezero_sdk::DZClient;
use eyre;

#[derive(Args, Debug)]
pub struct ResetCommand {}

impl ResetCommand {
    pub fn execute(self, client: &DZClient) -> eyre::Result<()> {
        client.reset()?;

        Ok(())
    }
}
