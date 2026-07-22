use crate::accesspass::{
    close::CloseAccessPassCliCommand, fund::FundAccessPassCliCommand, get::GetAccessPassCliCommand,
    list::ListAccessPassCliCommand, migrate_to_oracle::MigrateAccessPassToOracleCliCommand,
    set::SetAccessPassCliCommand, user_balances::UserBalancesAccessPassCliCommand,
};
use clap::{Args, Subcommand};

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
    /// List unique user_payer addresses and their SOL balances
    #[clap()]
    UserBalances(UserBalancesAccessPassCliCommand),
    /// Fund user payers that have insufficient balance
    #[clap()]
    Fund(FundAccessPassCliCommand),
    /// Re-own the shred oracle's validator-seeded access passes to the oracle (infra#2031)
    #[clap()]
    MigrateToOracle(MigrateAccessPassToOracleCliCommand),
}
