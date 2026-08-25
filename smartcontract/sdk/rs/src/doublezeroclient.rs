use async_trait::async_trait;
use doublezero_serviceability::state::{accountdata::AccountData, accounttype::AccountType};
use futures::{future::BoxFuture, stream::BoxStream, Future};
use solana_client::{
    nonblocking::pubsub_client::PubsubClientResult, rpc_config::RpcProgramAccountsConfig,
};
use solana_rpc_client_api::response::{Response, RpcKeyedAccount};
use solana_sdk::{
    account::Account, instruction::Instruction, pubkey::Pubkey, signature::Signature,
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
    fn get_block_time(&self, slot: u64) -> eyre::Result<Option<i64>>;
    fn get_all(&self) -> eyre::Result<HashMap<Box<Pubkey>, Box<AccountData>>>;

    fn get(&self, pubkey: Pubkey) -> eyre::Result<AccountData>;
    fn gets(&self, account_type: AccountType) -> eyre::Result<HashMap<Pubkey, AccountData>>;
    fn get_account(&self, pubkey: Pubkey) -> eyre::Result<Account>;
    fn get_minimum_balance_for_rent_exemption(&self, data_len: usize) -> eyre::Result<u64>;
    fn get_multiple_accounts(&self, pubkeys: Vec<Pubkey>) -> eyre::Result<Vec<Option<Account>>>;
    fn transfer_sol(&self, to: Pubkey, lamports: u64) -> eyre::Result<Signature>;
    fn get_program_accounts(
        &self,
        program_id: &Pubkey,
        config: RpcProgramAccountsConfig,
    ) -> eyre::Result<Vec<(Pubkey, Account)>>;

    /// Prepend the compute-budget prelude to a pre-built serviceability
    /// `Instruction` (from a `doublezero-serviceability-instruction` builder),
    /// then sign and send. The builder owns the trailing `[payer, system]`
    /// accounts (RFC-26); the send path no longer touches account layout.
    fn send_transaction(&self, instruction: Instruction) -> eyre::Result<Signature>;

    /// The same path for a transaction that needs more than one instruction: the
    /// compute-budget prelude, then `instructions` in the order given, signed and
    /// sent as one atomic transaction.
    ///
    /// RFC-27 is why this exists — a user creation carrying an `IpOwnershipProof`
    /// must ride alongside the native `Ed25519SigVerify` instruction that the
    /// program introspects the Instructions sysvar to find, and the two only mean
    /// anything together.
    fn send_instructions(&self, instructions: Vec<Instruction>) -> eyre::Result<Signature>;

    fn get_transactions(&self, pubkey: Pubkey) -> eyre::Result<Vec<DZTransaction>>;
}

pub type RpcKeyedAccountResponse = Response<RpcKeyedAccount>;
pub type UnsubscribeFn = Box<dyn FnOnce() -> BoxFuture<'static, ()> + Send>;
pub type SubscribeResult<'a> =
    PubsubClientResult<(BoxStream<'a, RpcKeyedAccountResponse>, UnsubscribeFn)>;

#[async_trait]
pub trait AsyncDoubleZeroClient {
    fn subscribe<'a>(&'a self) -> impl Future<Output = SubscribeResult<'a>> + Send + 'a;
}
