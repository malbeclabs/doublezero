use clap::{Args, Subcommand};
use doublezero_cli::accesspass::{
    close::CloseAccessPassCliCommand, fund::FundAccessPassCliCommand,
    get::GetAccessPassCliCommand, list::ListAccessPassCliCommand, set::SetAccessPassCliCommand,
    user_balances::UserBalancesAccessPassCliCommand,
};

#[derive(Args, Debug)]
pub struct AccessPassCliCommand {
    #[command(subcommand)]
    pub command: AccessPassCommands,
}

#[derive(Debug, Subcommand)]
pub enum AccessPassCommands {
    /// Set access pass
    #[clap()]
    Set(SetAccessPassCliCommand),
    /// Close access pass
    #[clap()]
    Close(CloseAccessPassCliCommand),
    /// List access passes
    #[clap()]
    List(ListAccessPassCliCommand),
    /// Get access pass details
    #[clap()]
    Get(GetAccessPassCliCommand),
    /// Show balances and funding status per user payer
    #[clap()]
    UserBalances(UserBalancesAccessPassCliCommand),
    /// Fund user payers that are below required balance
    #[clap()]
    Fund(FundAccessPassCliCommand),
}
