use clap::Args;
use clap::Subcommand;

use super::multicastgroup::MulticastGroupCliCommand;

#[derive(Args, Debug)]
pub struct MulticastCliCommand {
    #[command(subcommand)]
    pub command: MulticastCommands,
}

#[derive(Debug, Subcommand)]
pub enum MulticastCommands {
    Group(MulticastGroupCliCommand),
}
