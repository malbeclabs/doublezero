use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    state::{accountdata::AccountData, accounttype::AccountType},
};
use solana_client::rpc_config::RpcProgramAccountsConfig;
use solana_sdk::{
    account::Account, instruction::AccountMeta, pubkey::Pubkey, signature::Signature,
};
use std::collections::HashMap;

use crate::dztransaction::DZTransaction;
use mockall::automock;

#[automock]
pub trait DoubleZeroClient {
    fn get_program_id(&self) -> Pubkey;
    fn get_payer(&self) -> Pubkey;
    fn get_balance(&self) -> eyre::Result<u64>;
    fn get_epoch(&self) -> eyre::Result<u64>;

    fn get(&self, pubkey: Pubkey) -> eyre::Result<AccountData>;
    fn gets(&self, account_type: AccountType) -> eyre::Result<HashMap<Pubkey, AccountData>>;
    fn get_account(&self, pubkey: Pubkey) -> eyre::Result<Account>;
    fn get_program_accounts(
        &self,
        program_id: &Pubkey,
        config: RpcProgramAccountsConfig,
    ) -> eyre::Result<Vec<(Pubkey, Account)>>;

    fn execute_transaction(
        &self,
        instruction: DoubleZeroInstruction,
        accounts: Vec<AccountMeta>,
    ) -> eyre::Result<Signature>;
    fn get_transactions(&self, pubkey: Pubkey) -> eyre::Result<Vec<DZTransaction>>;
}
