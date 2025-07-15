use clap::Args;
use doublezero_sdk::DZClient;
use std::{
    io::Write,
    sync::{atomic::AtomicBool, Arc},
};

#[derive(Args, Debug)]
pub struct SubscribeCliCommand;

impl SubscribeCliCommand {
    pub fn execute<W: Write>(self, client: &DZClient, out: &mut W) -> eyre::Result<()> {
        println!("Waiting for events...");

        client.subscribe(
            |_, pubkey, account| {
                if let Err(e) = writeln!(out, "{pubkey} -> {account:?}") {
                    eprintln!("Failed to write output: {e}");
                }
            },
            Arc::new(AtomicBool::new(false)),
        )?;

        Ok(())
    }
}
