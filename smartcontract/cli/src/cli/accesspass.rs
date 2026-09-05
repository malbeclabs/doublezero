use crate::accesspass::{
    apply::ApplyAccessPassCliCommand,
    close::CloseAccessPassCliCommand,
    dzf_lock::{DzfLockAccessPassCliCommand, DzfUnlockAccessPassCliCommand},
    fund::FundAccessPassCliCommand,
    get::GetAccessPassCliCommand,
    list::ListAccessPassCliCommand,
    plan::PlanAccessPassCliCommand,
    set::SetAccessPassCliCommand,
    user_balances::UserBalancesAccessPassCliCommand,
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
    /// Show what a definition document would change, without writing
    #[clap()]
    Plan(PlanAccessPassCliCommand),
    /// Converge the ledger onto a definition document
    #[clap()]
    Apply(ApplyAccessPassCliCommand),
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
    /// Mark an access pass as DZF-locked (foundation-managed; ignored by automated reconcilers)
    #[clap()]
    DzfLock(DzfLockAccessPassCliCommand),
    /// Clear the DZF-locked mark on an access pass
    #[clap()]
    DzfUnlock(DzfUnlockAccessPassCliCommand),
}
