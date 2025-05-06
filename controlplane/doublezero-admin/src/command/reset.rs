use clap::Args;
use doublezero_sdk::DZClient;
use eyre;

#[derive(Args, Debug)]
pub struct ResetCommand {}

impl ResetCommand {
    pub fn execute<W: Write>(self, client: &dyn DoubleZeroClient, out: &mut W) -> eyre::Result<()> {

        client.reset()?;

        Ok(())
    }
}
