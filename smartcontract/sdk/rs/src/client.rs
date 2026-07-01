use backon::{BlockingRetryable, ExponentialBuilder};
use base64::{engine::general_purpose, Engine};
use chrono::{DateTime, NaiveDateTime, Utc};
use doublezero_config::Environment;
use std::time::Duration;

use crate::config::default_program_id;
use doublezero_serviceability::{
    error::DoubleZeroError, instructions::*, pda::get_permission_pda,
    state::accounttype::AccountType,
};
use eyre::{bail, eyre, OptionExt};
use log::debug;
use solana_account_decoder::UiAccountEncoding;
use solana_client::{
    pubsub_client::PubsubClient,
    rpc_client::RpcClient,
    rpc_config::{
        RpcAccountInfoConfig, RpcProgramAccountsConfig, RpcSendTransactionConfig,
        RpcTransactionConfig,
    },
    rpc_filter::{Memcmp, MemcmpEncodedBytes, RpcFilterType},
};
use solana_commitment_config::CommitmentConfig;
use solana_compute_budget_interface::ComputeBudgetInstruction;
use solana_rpc_client_api::client_error::{Error as ClientError, ErrorKind as ClientErrorKind};
use solana_sdk::{
    account::Account,
    instruction::{AccountMeta, Instruction, InstructionError},
    program_error::ProgramError,
    pubkey::Pubkey,
    signature::{Keypair, Signature, Signer},
    transaction::{Transaction, TransactionError},
};
use solana_system_interface::program;
use solana_transaction_status::{
    option_serializer::OptionSerializer, EncodedTransaction, TransactionBinaryEncoding,
    UiTransactionEncoding,
};
use std::{
    collections::HashMap,
    path::PathBuf,
    str::FromStr,
    sync::{
        atomic::{AtomicBool, Ordering},
        Arc, Mutex,
    },
};

use crate::{
    config::*,
    doublezeroclient::DoubleZeroClient,
    dztransaction::DZTransaction,
    errors::{SimulationError, SimulationTransactionError},
    keypair::load_keypair,
    rpckeyedaccount_decode::rpckeyedaccount_decode,
    AccountData,
};

// Serviceability runs on a dedicated private Solana cluster, so every
// transaction requests the protocol-max compute and heap budget. Values mirror
// `solana-compute-budget`'s `MAX_COMPUTE_UNIT_LIMIT` and `MAX_HEAP_FRAME_BYTES`,
// which are not re-exported through `solana-sdk`.
const MAX_COMPUTE_UNIT_LIMIT: u32 = 1_400_000;
const MAX_HEAP_FRAME_BYTES: u32 = 256 * 1024;

pub struct DZClient {
    rpc_url: String,
    client: RpcClient,
    rpc_ws_url: String,
    payer: Option<Keypair>,
    pub(crate) program_id: Pubkey,
    /// Memoizes the payer's Permission PDA lookup so authorized transactions
    /// resolve it at most once per client (the payer is fixed for the client's
    /// lifetime). `None` = not yet resolved; `Some(None)` = resolved, no
    /// on-chain Permission account; `Some(Some(meta))` = resolved and present.
    permission_account_cache: Mutex<Option<Option<AccountMeta>>>,
}

impl DZClient {
    pub fn new(
        rpc_url: Option<String>,
        websocket_url: Option<String>,
        program_id: Option<String>,
        keypair: Option<PathBuf>,
    ) -> eyre::Result<DZClient> {
        let (_, config) = read_doublezero_config()?;

        let rpc_url = convert_url_moniker(rpc_url.unwrap_or(config.json_rpc_url));
        let ws_url = convert_url_to_ws(&rpc_url.to_string())?;
        let rpc_ws_url =
            convert_ws_moniker(websocket_url.unwrap_or(config.websocket_url.unwrap_or(ws_url)));

        let client = RpcClient::new_with_commitment(rpc_url.clone(), CommitmentConfig::confirmed());
        let payer = load_keypair(keypair, None, config.keypair_path)
            .ok()
            .map(|r| r.keypair);

        let program_id = match program_id {
            None => match config.program_id.as_ref() {
                None => default_program_id(),
                Some(config_pg_id) => {
                    Pubkey::from_str(config_pg_id).map_err(|_| eyre!("Invalid program ID"))?
                }
            },
            Some(pg_id) => {
                let converted_id = convert_program_moniker(pg_id);
                Pubkey::from_str(&converted_id).map_err(|_| eyre!("Invalid program ID"))?
            }
        };

        Ok(DZClient {
            rpc_url,
            client,
            rpc_ws_url,
            payer,
            program_id,
            permission_account_cache: Mutex::new(None),
        })
    }

    /// Build a `DZClient` from a resolved RFC-20 [`CliContext`].
    ///
    /// Unlike [`DZClient::new`], this performs no `config.yml` read and no
    /// moniker conversion: the context already carries the fully resolved
    /// ledger RPC/WS URLs and serviceability program ID, so they are consumed
    /// verbatim. This makes the context the single source of truth and removes
    /// the double-resolution the binary previously incurred.
    ///
    /// `keypair` is the raw `--keypair` CLI flag (or `None`). It is passed as
    /// the highest-precedence source to [`load_keypair`] so the standard chain
    /// (CLI flag > `DOUBLEZERO_KEYPAIR` > stdin > config path > default) is
    /// preserved. The context's `keypair_path` is used only as the lower-
    /// precedence config/default path; passing it as the CLI source would mask
    /// the env var.
    #[cfg(feature = "cli-context")]
    pub fn from_context(
        ctx: &doublezero_cli_core::CliContext,
        keypair: Option<PathBuf>,
    ) -> eyre::Result<DZClient> {
        let rpc_url = ctx.ledger_rpc_url.clone();
        let rpc_ws_url = ctx.ledger_ws_rpc_url.clone();

        let client = RpcClient::new_with_commitment(rpc_url.clone(), CommitmentConfig::confirmed());

        let default_path = ctx
            .keypair_path
            .clone()
            .unwrap_or_else(default_keypair_path);
        let payer = load_keypair(keypair, None, default_path)
            .ok()
            .map(|r| r.keypair);

        Ok(DZClient {
            rpc_url,
            client,
            rpc_ws_url,
            payer,
            program_id: ctx.serviceability_program_id,
            permission_account_cache: Mutex::new(None),
        })
    }

    pub fn get_rpc(&self) -> &String {
        &self.rpc_url
    }

    pub fn rpc_client(&self) -> &RpcClient {
        &self.client
    }

    pub fn payer_keypair(&self) -> Option<&Keypair> {
        self.payer.as_ref()
    }

    pub fn get_ws(&self) -> &String {
        &self.rpc_ws_url
    }

    pub fn get_program_id(&self) -> &Pubkey {
        &self.program_id
    }

    pub fn get_environment(&self) -> Environment {
        Environment::from_program_id(&self.program_id.to_string()).unwrap_or_default()
    }

    fn rpc_retry_builder() -> ExponentialBuilder {
        ExponentialBuilder::new()
            .with_max_times(3)
            .with_min_delay(Duration::from_millis(500))
            .with_max_delay(Duration::from_secs(5))
    }

    /// Assemble the full instruction list for a serviceability transaction.
    ///
    /// Every transaction is prefixed with the protocol-max compute-unit and
    /// heap-frame requests (serviceability runs on a dedicated private cluster
    /// where this is always required — see the module-level constants). The main
    /// instruction's trailing accounts are always `[payer, system]`, optionally
    /// followed by the payer's Permission PDA. The Permission account MUST stay
    /// last because `authorize()` reads it as the final account after the
    /// expected ones have been consumed.
    fn assemble_instructions(
        program_id: &Pubkey,
        payer: &Pubkey,
        instruction: &DoubleZeroInstruction,
        accounts: Vec<AccountMeta>,
        permission: Option<AccountMeta>,
    ) -> Vec<Instruction> {
        let data = instruction.pack();

        let mut trailing = vec![
            AccountMeta::new(*payer, true),
            AccountMeta::new(program::id(), false),
        ];
        if let Some(permission) = permission {
            trailing.push(permission);
        }

        vec![
            ComputeBudgetInstruction::set_compute_unit_limit(MAX_COMPUTE_UNIT_LIMIT),
            ComputeBudgetInstruction::request_heap_frame(MAX_HEAP_FRAME_BYTES),
            Instruction::new_with_bytes(*program_id, &data, [accounts, trailing].concat()),
        ]
    }

    /// Resolve the payer's Permission PDA as a read-only [`AccountMeta`], or
    /// `None` when no Permission account exists on-chain. The lookup is retried
    /// on transient RPC errors and memoized for the client's lifetime.
    fn resolve_permission_account(&self, payer: &Pubkey) -> Option<AccountMeta> {
        let mut cache = self.permission_account_cache.lock().unwrap();
        if let Some(cached) = cache.as_ref() {
            return cached.clone();
        }
        let (permission_pda, _) = get_permission_pda(&self.program_id, payer);
        let exists = (|| self.client.get_account(&permission_pda))
            .retry(Self::rpc_retry_builder())
            .when(Self::is_retryable_rpc_error)
            .call()
            .is_ok();
        let meta = exists.then(|| AccountMeta::new_readonly(permission_pda, false));
        *cache = Some(meta.clone());
        meta
    }

    fn execute_transaction_inner(
        &self,
        instruction: DoubleZeroInstruction,
        accounts: Vec<AccountMeta>,
        quiet: bool,
        with_permission: bool,
    ) -> eyre::Result<Signature> {
        let payer = self
            .payer
            .as_ref()
            .ok_or_eyre("No default signer found, run \"doublezero keygen\" to create a new one")?;

        let permission = with_permission
            .then(|| self.resolve_permission_account(&payer.pubkey()))
            .flatten();

        let instructions = Self::assemble_instructions(
            &self.program_id,
            &payer.pubkey(),
            &instruction,
            accounts,
            permission,
        );

        let mut transaction = Transaction::new_with_payer(&instructions, Some(&payer.pubkey()));

        let blockhash = self.client.get_latest_blockhash().map_err(|e| eyre!(e))?;
        transaction.sign(&[&payer], blockhash);

        debug!("Sending transaction: {transaction:?}");

        let signature = transaction.signatures[0];
        let send_config = RpcSendTransactionConfig {
            skip_preflight: true,
            ..RpcSendTransactionConfig::default()
        };

        match self
            .client
            .send_and_confirm_transaction_with_spinner_and_config(
                &transaction,
                self.client.commitment(),
                send_config,
            ) {
            Ok(sig) => Ok(sig),
            Err(client_err) => {
                let tx_err = match client_err.kind.as_ref() {
                    ClientErrorKind::TransactionError(e) => Some(e.clone()),
                    ClientErrorKind::RpcError(
                        solana_rpc_client_api::request::RpcError::RpcResponseError {
                            data:
                                solana_rpc_client_api::request::RpcResponseErrorData::SendTransactionPreflightFailure(
                                    res,
                                ),
                            ..
                        },
                    ) => res.err.clone().map(Into::into),
                    _ => None,
                };

                let Some(err) = tx_err else {
                    return Err(eyre!(client_err));
                };

                // The tx may have landed onchain (skip_preflight=true means failing
                // txs still land). Fetch logs from the confirmed tx if available.
                let program_logs = self
                    .client
                    .get_transaction_with_config(
                        &signature,
                        RpcTransactionConfig {
                            encoding: Some(UiTransactionEncoding::Base64),
                            commitment: Some(self.client.commitment()),
                            max_supported_transaction_version: Some(0),
                        },
                    )
                    .ok()
                    .and_then(|tx| tx.transaction.meta)
                    .and_then(|meta| match meta.log_messages {
                        OptionSerializer::Some(logs) => Some(logs),
                        _ => None,
                    })
                    .unwrap_or_default();

                if quiet {
                    if let TransactionError::InstructionError(
                        _index,
                        InstructionError::Custom(number),
                    ) = err
                    {
                        return Err(eyre!(SimulationError {
                            source: DoubleZeroError::from(number),
                            program_logs,
                        }));
                    }
                    return Err(eyre!(SimulationTransactionError {
                        source: err,
                        program_logs,
                    }));
                }

                eprintln!("Program Logs:");
                for log in &program_logs {
                    eprintln!("{log}");
                }

                if let TransactionError::InstructionError(
                    _index,
                    InstructionError::Custom(number),
                ) = err
                {
                    return Err(eyre!(DoubleZeroError::from(number)));
                }
                Err(eyre!(err))
            }
        }
    }

    /// Returns true for transient network errors that are worth retrying.
    /// Returns false for permanent errors like AccountNotFound or RPC response errors.
    fn is_retryable_rpc_error(err: &ClientError) -> bool {
        matches!(
            err.kind.as_ref(),
            ClientErrorKind::Io(_) | ClientErrorKind::Reqwest(_) | ClientErrorKind::Middleware(_)
        )
    }

    pub fn get_balance(&self) -> eyre::Result<u64> {
        let payer = self
            .payer
            .as_ref()
            .ok_or_else(|| eyre!("No payer configured for client!"))?;

        let pubkey = payer.pubkey();
        (|| self.client.get_balance(&pubkey))
            .retry(Self::rpc_retry_builder())
            .when(Self::is_retryable_rpc_error)
            .call()
            .map_err(|e| eyre!(e))
    }

    pub fn get_epoch(&self) -> eyre::Result<u64> {
        (|| self.client.get_epoch_info())
            .retry(Self::rpc_retry_builder())
            .when(Self::is_retryable_rpc_error)
            .call()
            .map_err(|e| eyre!(e))
            .map(|info| info.epoch)
    }

    pub fn get_account(&self, pubkey: Pubkey) -> eyre::Result<Account> {
        (|| self.client.get_account(&pubkey))
            .retry(Self::rpc_retry_builder())
            .when(Self::is_retryable_rpc_error)
            .call()
            .map_err(|e| eyre!(e))
    }

    pub fn get_minimum_balance_for_rent_exemption(&self, data_len: usize) -> eyre::Result<u64> {
        (|| self.client.get_minimum_balance_for_rent_exemption(data_len))
            .retry(Self::rpc_retry_builder())
            .when(Self::is_retryable_rpc_error)
            .call()
            .map_err(|e| eyre!(e))
    }

    pub fn transfer_sol(&self, to: Pubkey, lamports: u64) -> eyre::Result<Signature> {
        let payer = self
            .payer
            .as_ref()
            .ok_or_eyre("No default signer found, run \"doublezero keygen\" to create a new one")?;
        let ix = solana_system_interface::instruction::transfer(&payer.pubkey(), &to, lamports);
        let mut transaction =
            solana_sdk::transaction::Transaction::new_with_payer(&[ix], Some(&payer.pubkey()));
        let blockhash = self.client.get_latest_blockhash().map_err(|e| eyre!(e))?;
        transaction.sign(&[payer], blockhash);
        self.client
            .send_and_confirm_transaction(&transaction)
            .map_err(|e| eyre!(e))
    }

    pub fn get_multiple_accounts(&self, pubkeys: &[Pubkey]) -> eyre::Result<Vec<Option<Account>>> {
        let mut results = Vec::with_capacity(pubkeys.len());
        for chunk in pubkeys.chunks(100) {
            let accounts = (|| self.client.get_multiple_accounts(chunk))
                .retry(Self::rpc_retry_builder())
                .when(Self::is_retryable_rpc_error)
                .call()
                .map_err(|e| eyre!(e))?;
            results.extend(accounts);
        }
        Ok(results)
    }

    /******************************************************************************************************************************************/

    pub fn gets_and_subscribe<F>(
        &self,
        mut action: F,
        stop_signal: Arc<AtomicBool>,
    ) -> eyre::Result<()>
    where
        F: FnMut(&DZClient, Box<Pubkey>, Box<AccountData>),
    {
        while !stop_signal.load(Ordering::Relaxed) {
            match self.get_all() {
                Ok(accounts) => {
                    for (pubkey, account) in accounts {
                        action(self, pubkey, account);
                    }
                }
                Err(e) => {
                    eprintln!("Error: {e}");
                }
            }

            _ = self
                .subscribe(&mut action, stop_signal.clone())
                .inspect_err(|e| eprintln!("Error: {e}"));
        }

        Ok(())
    }

    #[allow(clippy::collapsible_match)]
    pub fn subscribe<F>(&self, mut action: F, stop_signal: Arc<AtomicBool>) -> eyre::Result<()>
    where
        F: FnMut(&DZClient, Box<Pubkey>, Box<AccountData>),
    {
        while !stop_signal.load(Ordering::Relaxed) {
            let options = RpcProgramAccountsConfig {
                filters: None,
                account_config: RpcAccountInfoConfig {
                    encoding: Some(UiAccountEncoding::Base64),
                    data_slice: None,
                    commitment: Some(CommitmentConfig::confirmed()),
                    min_context_slot: None,
                },
                with_context: None,
                sort_results: None,
            };
            let (mut _client, receiver) =
                PubsubClient::program_subscribe(&self.rpc_ws_url, &self.program_id, Some(options))
                    .map_err(|_| eyre!("Unable to program_subscribe"))?;

            for response in receiver {
                let event = response.value;
                if let Some(pubkey_account) = rpckeyedaccount_decode(event)? {
                    action(self, pubkey_account.0, pubkey_account.1);
                }
            }
        }

        Ok(())
    }

    pub fn get_logs(&self, pubkey: &Pubkey) -> eyre::Result<Vec<String>> {
        let mut errors: Vec<String> = Vec::new();

        let signatures = (|| self.client.get_signatures_for_address(pubkey))
            .retry(Self::rpc_retry_builder())
            .when(Self::is_retryable_rpc_error)
            .call()?;

        for signature_info in signatures {
            let signature = Signature::from_str(&signature_info.signature)?;

            if let Ok(trans) = (|| {
                self.client
                    .get_transaction(&signature, UiTransactionEncoding::Base64)
            })
            .retry(Self::rpc_retry_builder())
            .when(Self::is_retryable_rpc_error)
            .call()
            {
                if let EncodedTransaction::Binary(_, base) = trans.transaction.transaction {
                    if base == TransactionBinaryEncoding::Base64 {
                        if let Some(meta) = trans.transaction.meta {
                            if let OptionSerializer::Some(msgs) = meta.log_messages {
                                for msg in msgs {
                                    errors.push(msg.to_string());
                                }
                            }
                        }
                    }
                }
            }
        }

        Ok(errors)
    }
}

impl DoubleZeroClient for DZClient {
    fn get_program_id(&self) -> Pubkey {
        self.program_id
    }

    fn get_payer(&self) -> Pubkey {
        match self.payer.as_ref() {
            Some(keypair) => keypair.pubkey(),
            None => Pubkey::default(),
        }
    }

    fn get_balance(&self) -> eyre::Result<u64> {
        let payer = self.get_payer();
        (|| self.client.get_balance(&payer))
            .retry(Self::rpc_retry_builder())
            .when(Self::is_retryable_rpc_error)
            .call()
            .map_err(|e| eyre!(e))
    }

    fn get_epoch(&self) -> eyre::Result<u64> {
        (|| self.client.get_epoch_info())
            .retry(Self::rpc_retry_builder())
            .when(Self::is_retryable_rpc_error)
            .call()
            .map_err(|e| eyre!(e))
            .map(|info| info.epoch)
    }

    fn get_block_time(&self, slot: u64) -> eyre::Result<Option<i64>> {
        match self.client.get_block_time(slot) {
            Ok(ts) => Ok(Some(ts)),
            Err(e) => {
                let msg = e.to_string();
                if msg.contains("Block not available") || msg.contains("was skipped") {
                    Ok(None)
                } else {
                    Err(eyre!(e))
                }
            }
        }
    }

    fn get_all(&self) -> eyre::Result<HashMap<Box<Pubkey>, Box<AccountData>>> {
        let options = RpcProgramAccountsConfig {
            filters: None,
            account_config: RpcAccountInfoConfig {
                encoding: Some(UiAccountEncoding::Base64),
                data_slice: None,
                commitment: Some(CommitmentConfig::confirmed()),
                min_context_slot: None,
            },
            with_context: None,
            sort_results: None,
        };

        let mut list: HashMap<Box<Pubkey>, Box<AccountData>> = HashMap::new();

        let accounts = (|| {
            self.client
                .get_program_accounts_with_config(&self.program_id, options.clone())
        })
        .retry(Self::rpc_retry_builder())
        .when(Self::is_retryable_rpc_error)
        .call()?;

        for (pubkey, account) in accounts {
            let account = match AccountData::try_from(&account.data[..]) {
                Ok(data) => data,
                Err(ProgramError::InvalidAccountData) => {
                    continue;
                }
                Err(e) => {
                    return Err(e.into());
                }
            };
            list.insert(Box::new(pubkey), Box::new(account));
        }

        Ok(list)
    }

    fn execute_transaction(
        &self,
        instruction: DoubleZeroInstruction,
        accounts: Vec<AccountMeta>,
    ) -> eyre::Result<Signature> {
        self.execute_transaction_inner(instruction, accounts, false, false)
    }

    fn execute_transaction_quiet(
        &self,
        instruction: DoubleZeroInstruction,
        accounts: Vec<AccountMeta>,
    ) -> eyre::Result<Signature> {
        self.execute_transaction_inner(instruction, accounts, true, false)
    }

    fn execute_authorized_transaction(
        &self,
        instruction: DoubleZeroInstruction,
        accounts: Vec<AccountMeta>,
    ) -> eyre::Result<Signature> {
        self.execute_transaction_inner(instruction, accounts, false, true)
    }

    fn execute_authorized_transaction_quiet(
        &self,
        instruction: DoubleZeroInstruction,
        accounts: Vec<AccountMeta>,
    ) -> eyre::Result<Signature> {
        self.execute_transaction_inner(instruction, accounts, true, true)
    }

    fn gets(&self, account_type: AccountType) -> eyre::Result<HashMap<Pubkey, AccountData>> {
        let account_type = account_type as u8;
        let filters = vec![RpcFilterType::Memcmp(Memcmp::new(
            0,
            MemcmpEncodedBytes::Bytes(vec![account_type]),
        ))];
        let options = RpcProgramAccountsConfig {
            filters: Some(filters),
            account_config: RpcAccountInfoConfig {
                encoding: Some(UiAccountEncoding::Base64),
                data_slice: None,
                commitment: Some(CommitmentConfig::confirmed()),
                min_context_slot: None,
            },
            with_context: None,
            sort_results: None,
        };

        let mut list: HashMap<Pubkey, AccountData> = HashMap::new();
        let program_id = self.get_program_id();
        let accounts = (|| {
            self.client
                .get_program_accounts_with_config(program_id, options.clone())
        })
        .retry(Self::rpc_retry_builder())
        .when(Self::is_retryable_rpc_error)
        .call()?;

        for (pubkey, account) in accounts {
            assert!(account.data[0] == account_type, "Invalid account type");
            list.insert(pubkey, AccountData::try_from(&account.data[..])?);
        }

        Ok(list)
    }

    fn get(&self, pubkey: Pubkey) -> eyre::Result<AccountData> {
        let account = (|| self.client.get_account(&pubkey))
            .retry(Self::rpc_retry_builder())
            .when(Self::is_retryable_rpc_error)
            .call()
            .map_err(|e| eyre!(e))?;

        if account.owner == self.program_id {
            let data = account.data;
            Ok(AccountData::try_from(&data[..])?)
        } else {
            Ok(AccountData::None)
        }
    }

    fn get_account(&self, pubkey: Pubkey) -> eyre::Result<Account> {
        (|| self.client.get_account(&pubkey))
            .retry(Self::rpc_retry_builder())
            .when(Self::is_retryable_rpc_error)
            .call()
            .map_err(|e| eyre!(e))
    }

    fn get_minimum_balance_for_rent_exemption(&self, data_len: usize) -> eyre::Result<u64> {
        self.get_minimum_balance_for_rent_exemption(data_len)
    }

    fn get_multiple_accounts(&self, pubkeys: Vec<Pubkey>) -> eyre::Result<Vec<Option<Account>>> {
        self.get_multiple_accounts(&pubkeys)
    }

    fn transfer_sol(&self, to: Pubkey, lamports: u64) -> eyre::Result<Signature> {
        self.transfer_sol(to, lamports)
    }

    fn get_program_accounts(
        &self,
        program_id: &Pubkey,
        config: RpcProgramAccountsConfig,
    ) -> eyre::Result<Vec<(Pubkey, Account)>> {
        (|| {
            self.client
                .get_program_accounts_with_config(program_id, config.clone())
        })
        .retry(Self::rpc_retry_builder())
        .when(Self::is_retryable_rpc_error)
        .call()
        .map_err(|e| eyre!(e))
    }

    #[allow(deprecated)]
    fn get_transactions(&self, pubkey: Pubkey) -> eyre::Result<Vec<DZTransaction>> {
        let mut transactions: Vec<DZTransaction> = Vec::new();

        let signatures = (|| self.client.get_signatures_for_address(&pubkey))
            .retry(Self::rpc_retry_builder())
            .when(Self::is_retryable_rpc_error)
            .call()?;

        for signature_info in signatures.into_iter() {
            let signature = Signature::from_str(&signature_info.signature)?;
            let enc_transaction = (|| {
                self.client
                    .get_transaction(&signature, UiTransactionEncoding::Base64)
            })
            .retry(Self::rpc_retry_builder())
            .when(Self::is_retryable_rpc_error)
            .call()?;

            let time = enc_transaction.block_time.unwrap_or_default();

            let time = match NaiveDateTime::from_timestamp_opt(time, 0) {
                Some(dt) => DateTime::<Utc>::from_naive_utc_and_offset(dt, Utc),
                None => DateTime::<Utc>::from_timestamp_nanos(0),
            };

            let trans = enc_transaction.transaction.transaction;

            if let EncodedTransaction::Binary(data, _enc) = trans {
                let data: &[u8] = &general_purpose::STANDARD.decode(data)?;

                let tx: Transaction =
                    match bincode::serde::decode_from_slice(data, bincode::config::legacy()) {
                        Ok((tx, _)) => tx,
                        Err(e) => {
                            bail!("Error deserializing txn: {:?}", e);
                        }
                    };

                for instr in tx.message.instructions.iter() {
                    let program_id = instr.program_id(&tx.message.account_keys);
                    let account = instr.accounts[instr.accounts.len() - 2];
                    let account = tx.message.account_keys[account as usize];

                    let instruction = {
                        if program_id == &self.program_id {
                            DoubleZeroInstruction::unpack(&instr.data)?
                        } else {
                            DoubleZeroInstruction::InitGlobalState()
                        }
                    };

                    let log_messages = match &enc_transaction.transaction.meta {
                        None => vec![],
                        Some(meta) => {
                            if let OptionSerializer::Some(msgs) = &meta.log_messages {
                                msgs.clone()
                            } else {
                                vec![]
                            }
                        }
                    };

                    transactions.push(DZTransaction {
                        time,
                        account,
                        instruction,
                        signature,
                        log_messages,
                    });
                }
            }
        }

        Ok(transactions)
    }
}

#[cfg(test)]
mod assemble_instructions_tests {
    use super::*;

    // Compute-budget instruction borsh discriminants.
    const SET_COMPUTE_UNIT_LIMIT: u8 = 2;
    const REQUEST_HEAP_FRAME: u8 = 1;

    fn base_accounts() -> Vec<AccountMeta> {
        vec![
            AccountMeta::new(Pubkey::new_unique(), false),
            AccountMeta::new_readonly(Pubkey::new_unique(), false),
        ]
    }

    /// C1 regression: every transaction must carry the protocol-max compute and
    /// heap-frame requests as its first two instructions.
    #[test]
    fn prepends_compute_budget_instructions() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();

        let ixs = DZClient::assemble_instructions(
            &program_id,
            &payer,
            &DoubleZeroInstruction::InitGlobalState(),
            base_accounts(),
            None,
        );

        assert_eq!(ixs.len(), 3);
        // First two target the compute-budget program (not the serviceability one).
        assert_eq!(ixs[0].program_id, ixs[1].program_id);
        assert_ne!(ixs[0].program_id, program_id);
        assert_eq!(ixs[0].data[0], SET_COMPUTE_UNIT_LIMIT);
        assert_eq!(ixs[1].data[0], REQUEST_HEAP_FRAME);
        // Third is the actual serviceability instruction.
        assert_eq!(ixs[2].program_id, program_id);
    }

    #[test]
    fn trailing_accounts_without_permission_are_payer_then_system() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let base = base_accounts();

        let ixs = DZClient::assemble_instructions(
            &program_id,
            &payer,
            &DoubleZeroInstruction::InitGlobalState(),
            base.clone(),
            None,
        );

        let metas = &ixs[2].accounts;
        assert_eq!(metas.len(), base.len() + 2);

        let payer_meta = &metas[base.len()];
        assert_eq!(payer_meta.pubkey, payer);
        assert!(payer_meta.is_signer && payer_meta.is_writable);

        assert_eq!(metas[base.len() + 1].pubkey, program::id());
    }

    /// H2 regression: when present, the Permission account MUST be the trailing
    /// account — after payer + system — because `authorize()` reads it last.
    #[test]
    fn permission_account_is_appended_after_payer_and_system() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let (permission_pda, _) = get_permission_pda(&program_id, &payer);
        let base = base_accounts();

        let ixs = DZClient::assemble_instructions(
            &program_id,
            &payer,
            &DoubleZeroInstruction::InitGlobalState(),
            base.clone(),
            Some(AccountMeta::new_readonly(permission_pda, false)),
        );

        let metas = &ixs[2].accounts;
        assert_eq!(metas.len(), base.len() + 3);
        assert_eq!(metas[base.len()].pubkey, payer);
        assert_eq!(metas[base.len() + 1].pubkey, program::id());

        let perm = &metas[base.len() + 2];
        assert_eq!(perm.pubkey, permission_pda);
        assert!(!perm.is_signer && !perm.is_writable);
    }
}

#[cfg(all(test, feature = "cli-context"))]
mod cli_context_tests {
    use super::*;
    use doublezero_cli_core::CliContextBuilder;
    use doublezero_config::Environment;
    use serial_test::serial;
    use std::io::Write;

    const ENV_KEYPAIR: &str = "DOUBLEZERO_KEYPAIR";

    #[test]
    #[serial(doublezero_keypair_env)]
    fn from_context_uses_resolved_values_without_config_read() {
        let pid = Pubkey::new_unique();
        let ctx = CliContextBuilder::new()
            .with_env(Environment::Devnet)
            .with_ledger_rpc_url("http://localhost:8899/")
            .with_serviceability_program_id(pid)
            .build()
            .unwrap();

        let client = DZClient::from_context(&ctx, None).unwrap();

        // Resolved values consumed verbatim from the context.
        assert_eq!(client.get_rpc().as_str(), "http://localhost:8899/");
        // WS derived from the RPC override by scheme swap (no env-default WS).
        assert_eq!(client.get_ws().as_str(), "ws://localhost:8899/");
        assert_eq!(client.get_program_id(), &pid);
    }

    /// Guards the masking hazard: `from_context` must pass the context's
    /// keypair path only as the low-precedence fallback, never as the CLI
    /// source, so `DOUBLEZERO_KEYPAIR` still wins over it.
    #[test]
    #[serial(doublezero_keypair_env)]
    fn from_context_env_keypair_wins_over_context_path() {
        let kp = Keypair::new();
        let dir = tempfile::tempdir().unwrap();
        let kp_path = dir.path().join("env-key.json");
        let json = serde_json::to_string(&kp.to_bytes().to_vec()).unwrap();
        std::fs::File::create(&kp_path)
            .unwrap()
            .write_all(json.as_bytes())
            .unwrap();

        // Context carries a bogus keypair path. If it were used as the CLI
        // source it would win and fail to load; correct behavior is for the
        // env var to win.
        let ctx = CliContextBuilder::new()
            .with_env(Environment::Devnet)
            .with_ledger_rpc_url("http://localhost:8899/")
            .with_serviceability_program_id(Pubkey::new_unique())
            .with_keypair_path(PathBuf::from("/nonexistent/bogus.json"))
            .build()
            .unwrap();

        std::env::set_var(ENV_KEYPAIR, &kp_path);
        let client = DZClient::from_context(&ctx, None).unwrap();
        std::env::remove_var(ENV_KEYPAIR);

        assert_eq!(
            client.payer_keypair().map(|k| k.pubkey()),
            Some(kp.pubkey())
        );
    }
}
