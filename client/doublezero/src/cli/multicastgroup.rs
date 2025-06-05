use clap::{Args, Subcommand};

use doublezero_cli::multicastgroup::{
    allowlist::{
        publisher::{
            add::AddMulticastGroupPubAllowlistCliCommand,
            list::ListMulticastGroupPubAllowlistCliCommand,
            remove::RemoveMulticastGroupPubAllowlistCliCommand,
        },
        subscriber::{
            add::AddMulticastGroupSubAllowlistCliCommand,
            list::ListMulticastGroupSubAllowlistCliCommand,
            remove::RemoveMulticastGroupSubAllowlistCliCommand,
        },
    },
    create::CreateMulticastGroupCliCommand,
    delete::DeleteMulticastGroupCliCommand,
    get::GetMulticastGroupCliCommand,
    list::ListMulticastGroupCliCommand,
    update::UpdateMulticastGroupCliCommand,
};

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
