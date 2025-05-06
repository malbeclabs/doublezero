use clap::Args;
use doublezero_sdk::DZClient;
use std::io::Write;

#[derive(Args, Debug)]
pub struct SubscribeArgs {}

impl SubscribeArgs {
    pub fn execute<W: Write>(self, client: &DZClient, out: &mut W) -> eyre::Result<()> {
        println!("Waiting for events...");

        client.subscribe(|_, pubkey, account| {
            writeln!(out, "{} -> {:?}", pubkey, account).unwrap();
        })?;

        Ok(())
    }
}
