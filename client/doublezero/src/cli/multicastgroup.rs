use clap::Args;
use clap::Subcommand;

use doublezero_cli::multicastgroup::allowlist::publisher::add::AddMulticastGroupPubAllowlistCliCommand;
use doublezero_cli::multicastgroup::allowlist::publisher::list::ListMulticastGroupPubAllowlistCliCommand;
use doublezero_cli::multicastgroup::allowlist::publisher::remove::RemoveMulticastGroupPubAllowlistCliCommand;
use doublezero_cli::multicastgroup::allowlist::subscriber::add::AddMulticastGroupSubAllowlistCliCommand;
use doublezero_cli::multicastgroup::allowlist::subscriber::list::ListMulticastGroupSubAllowlistCliCommand;
use doublezero_cli::multicastgroup::allowlist::subscriber::remove::RemoveMulticastGroupSubAllowlistCliCommand;
use doublezero_cli::multicastgroup::create::CreateMulticastGroupCliCommand;
use doublezero_cli::multicastgroup::delete::DeleteMulticastGroupCliCommand;
use doublezero_cli::multicastgroup::get::GetMulticastGroupCliCommand;
use doublezero_cli::multicastgroup::list::ListMulticastGroupCliCommand;
use doublezero_cli::multicastgroup::update::UpdateMulticastGroupCliCommand;

#[derive(Args, Debug)]
pub struct MulticastGroupCliCommand {
    #[command(subcommand)]
    pub command: MulticastGroupCommands,
}

#[derive(Debug, Subcommand)]
pub enum MulticastGroupCommands {
    Allowlist(MulticastGroupAllowlistCliCommand),
    Create(CreateMulticastGroupCliCommand),
    Update(UpdateMulticastGroupCliCommand),
    List(ListMulticastGroupCliCommand),
    Get(GetMulticastGroupCliCommand),
    Delete(DeleteMulticastGroupCliCommand),
}

#[derive(Args, Debug)]
pub struct MulticastGroupAllowlistCliCommand {
    #[command(subcommand)]
    pub command: MulticastGroupAllowlistCommands,
}

#[derive(Debug, Subcommand)]
pub enum MulticastGroupAllowlistCommands {
    Publisher(MulticastGroupPubAllowlistCliCommand),
    Subscriber(MulticastGroupSubAllowlistCliCommand),
}

#[derive(Args, Debug)]
pub struct MulticastGroupPubAllowlistCliCommand {
    #[command(subcommand)]
    pub command: MulticastGroupPubAllowlistCommands,
}

#[derive(Debug, Subcommand)]
pub enum MulticastGroupPubAllowlistCommands {
    List(ListMulticastGroupPubAllowlistCliCommand),
    Add(AddMulticastGroupPubAllowlistCliCommand),
    Remove(RemoveMulticastGroupPubAllowlistCliCommand),
}

#[derive(Args, Debug)]
pub struct MulticastGroupSubAllowlistCliCommand {
    #[command(subcommand)]
    pub command: MulticastGroupSubAllowlistCommands,
}

#[derive(Debug, Subcommand)]
pub enum MulticastGroupSubAllowlistCommands {
    List(ListMulticastGroupSubAllowlistCliCommand),
    Add(AddMulticastGroupSubAllowlistCliCommand),
    Remove(RemoveMulticastGroupSubAllowlistCliCommand),
}
