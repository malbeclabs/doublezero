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
use doublezero_serviceability_instruction::compute_budget_prelude;
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
use solana_rpc_client_api::client_error::{Error as ClientError, ErrorKind as ClientErrorKind};
use solana_sdk::{
    account::Account,
    instruction::{Instruction, InstructionError},
    program_error::ProgramError,
    pubkey::Pubkey,
    signature::{Keypair, Signature, Signer},
    transaction::{Transaction, TransactionError},
};
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
    config::*, doublezeroclient::DoubleZeroClient, dztransaction::DZTransaction,
    keypair::load_keypair, rpckeyedaccount_decode::rpckeyedaccount_decode, AccountData,
};

enum PermissionAccountCache {
    Unresolved,
    Absent,
    Present(Account),
}

pub struct DZClient {
    rpc_url: String,
    client: RpcClient,
    rpc_ws_url: String,
    payer: Option<Keypair>,
    pub(crate) program_id: Pubkey,
    permission_account_cache: Mutex<PermissionAccountCache>,
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
            permission_account_cache: Mutex::new(PermissionAccountCache::Unresolved),
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
            permission_account_cache: Mutex::new(PermissionAccountCache::Unresolved),
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

    fn maybe_invalidate_permission_cache(&self, ix: &Instruction) {
        if let Ok(decoded) = DoubleZeroInstruction::unpack(&ix.data) {
            if matches!(
                decoded,
                DoubleZeroInstruction::CreatePermission(_)
                    | DoubleZeroInstruction::DeletePermission(_)
            ) {
                *self.permission_account_cache.lock().unwrap() = PermissionAccountCache::Unresolved;
            }
        }
    }

    /// Send pre-built serviceability [`Instruction`]s (RFC-26): prepend the
    /// compute-budget prelude, sign with the payer, and send. The builders own the
    /// account layout (including the trailing `[payer, system]`), so this path does
    /// no account assembly and no permission resolution. Single send attempt.
    ///
    /// Almost every caller passes one instruction. RFC-27 user creation passes two:
    /// the native `Ed25519SigVerify` instruction and the creation it authorizes,
    /// which have to land together or not at all.
    fn send_transaction_inner(&self, ixs: Vec<Instruction>) -> eyre::Result<Signature> {
        // Without this, an empty list would submit the compute-budget prelude on its own and
        // pay a fee for a transaction that does nothing.
        if ixs.is_empty() {
            bail!("No instructions to send");
        }

        let payer = self
            .payer
            .as_ref()
            .ok_or_eyre("No default signer found, run \"doublezero keygen\" to create a new one")?;

        for ix in &ixs {
            self.maybe_invalidate_permission_cache(ix);
        }

        // Prepend the shared RFC-26 compute-budget prelude (protocol-max compute
        // and heap) over the built instructions — same helper the builders document.
        let mut instructions = compute_budget_prelude().to_vec();
        instructions.extend(ixs);

        let mut transaction = Transaction::new_with_payer(&instructions, Some(&payer.pubkey()));
        let blockhash = self.client.get_latest_blockhash().map_err(|e| eyre!(e))?;
        transaction.sign(&[&payer], blockhash);

        debug!("Sending transaction: {transaction:?}");

        let signature = transaction.signatures[0];
        let send_config = RpcSendTransactionConfig {
            skip_preflight: true,
            ..RpcSendTransactionConfig::default()
        };

        let client_err = match self
            .client
            .send_and_confirm_transaction_with_spinner_and_config(
                &transaction,
                self.client.commitment(),
                send_config,
            ) {
            Ok(sig) => return Ok(sig),
            Err(client_err) => client_err,
        };

        let Some(err) = Self::parse_transaction_error(&client_err) else {
            return Err(eyre!(client_err));
        };

        // skip_preflight=true means a failing tx can still land; fetch its logs.
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

        eprintln!("Program Logs:");
        for log in &program_logs {
            eprintln!("{log}");
        }

        // `Custom` numbers are defined by whichever program raised them. An RFC-27 transaction
        // also carries the native Ed25519 instruction, whose `PrecompileError` shares that space,
        // so mapping every `Custom` through `DoubleZeroError` would print an unrelated
        // serviceability error for a precompile failure. Only serviceability's own instructions
        // are mapped; anything else is reported as the runtime described it.
        if let TransactionError::InstructionError(index, InstructionError::Custom(number)) = err {
            if transaction.message.program_id(index as usize) == Some(&self.program_id) {
                return Err(eyre!(DoubleZeroError::from(number)));
            }
        }
        Err(eyre!(err))
    }

    /// Extract the on-chain [`TransactionError`] from a send error, whether it surfaced
    /// as a confirmed `TransactionError` or as a preflight-failure RPC response. Returns
    /// `None` for transport/RPC errors that carry no program-level result.
    fn parse_transaction_error(client_err: &ClientError) -> Option<TransactionError> {
        match client_err.kind.as_ref() {
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
        }
    }

    /// Returns `true` for transient failures worth another attempt, `false` for a
    /// permanent one (`AccountNotFound`, a bad request, a deterministic program
    /// rejection).
    ///
    /// Transport errors (`Io` / `Reqwest` / `Middleware`) were already covered.
    /// The gap this closes — the shape the 2026-07-28 incident and
    /// `malbeclabs/infra#2100` arrived in for Go, and which Rust survived only
    /// because those particular 503s happened to surface as `Reqwest` — is a
    /// transient status carried *inside* a decoded JSON-RPC envelope
    /// (`RpcResponseError`), which was never retried:
    ///
    /// - an HTTP status a provider LB put in the envelope `code` (429/5xx),
    /// - a "busy, retry later" node code (`-32005`, `-32004`, `-32429`),
    /// - transient wording with no machine-readable code
    ///   (`-32603 "Service unavailable, please try again later."`).
    ///
    /// Mirrors the Go classifier from #4100 (`tools/solana/pkg/jsonrpc`).
    /// Onchain/request-level rejections (`-32002` preflight, `-32602`, `-32601`),
    /// `-32003` (signature-verification failure) and `-32011` (no history) stay
    /// non-retryable — an identical later request gets the same answer. This
    /// predicate gates only read RPCs here; no `send_transaction` path is wrapped
    /// with it, so an accepted-but-unacknowledged send is never resent.
    fn is_retryable_rpc_error(err: &ClientError) -> bool {
        match err.kind.as_ref() {
            ClientErrorKind::Io(_)
            | ClientErrorKind::Reqwest(_)
            | ClientErrorKind::Middleware(_) => true,
            ClientErrorKind::RpcError(rpc_err) => Self::is_retryable_rpc_response(rpc_err),
            _ => false,
        }
    }

    /// HTTP statuses that mean the endpoint (or a proxy in front of it) is
    /// shedding load, not that the request is wrong.
    fn is_retryable_http_status(code: i64) -> bool {
        matches!(code, 429 | 500 | 502 | 503 | 504)
    }

    /// JSON-RPC codes for a node that cannot serve this request right now but
    /// could serve an identical one later: `-32005` NODE_UNHEALTHY, `-32004`
    /// BLOCK_NOT_AVAILABLE, and the provider-minted `-32429` (HTTP 429 wrapped in
    /// an envelope). `-32003` (signature verification) and `-32011` (no history)
    /// are deterministic and deliberately absent.
    fn is_retryable_rpc_code(code: i64) -> bool {
        matches!(code, -32005 | -32004 | -32429)
    }

    /// Transient wording from providers that return no machine-readable code — a
    /// load balancer shedding load is the same failure whether it labels itself
    /// 503 or just says so.
    fn message_is_transient(message: &str) -> bool {
        let msg = message.to_ascii_lowercase();
        [
            "service unavailable",
            "too many requests",
            "bad gateway",
            "gateway timeout",
            "gateway time-out",
            "rate limited",
        ]
        .iter()
        .any(|needle| msg.contains(needle))
    }

    fn is_retryable_rpc_response(err: &solana_rpc_client_api::request::RpcError) -> bool {
        use solana_rpc_client_api::request::RpcError;
        match err {
            RpcError::RpcResponseError { code, message, .. } => {
                Self::is_retryable_http_status(*code)
                    || Self::is_retryable_rpc_code(*code)
                    || Self::message_is_transient(message)
            }
            // No code to classify; only retry on explicitly transient wording.
            RpcError::RpcRequestError(msg) | RpcError::ForUser(msg) => {
                Self::message_is_transient(msg)
            }
            RpcError::ParseError(_) => false,
        }
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
        let (permission_pda, _) = get_permission_pda(&self.program_id, &self.get_payer());
        let is_permission_lookup = pubkeys == [permission_pda];
        if is_permission_lookup {
            match *self.permission_account_cache.lock().unwrap() {
                PermissionAccountCache::Present(ref account) => {
                    return Ok(vec![Some(account.clone())]);
                }
                PermissionAccountCache::Absent => {
                    return Ok(vec![None; pubkeys.len()]);
                }
                PermissionAccountCache::Unresolved => {}
            }
        }

        let mut results = Vec::with_capacity(pubkeys.len());
        for chunk in pubkeys.chunks(100) {
            let accounts = (|| self.client.get_multiple_accounts(chunk))
                .retry(Self::rpc_retry_builder())
                .when(Self::is_retryable_rpc_error)
                .call()
                .map_err(|e| eyre!(e))?;
            results.extend(accounts);
        }

        if is_permission_lookup {
            let mut cache = self.permission_account_cache.lock().unwrap();
            *cache = match results.first() {
                Some(Some(account)) => PermissionAccountCache::Present(account.clone()),
                Some(None) => PermissionAccountCache::Absent,
                None => PermissionAccountCache::Unresolved,
            };
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

    fn send_transaction(&self, instruction: Instruction) -> eyre::Result<Signature> {
        self.send_transaction_inner(vec![instruction])
    }

    fn send_instructions(&self, instructions: Vec<Instruction>) -> eyre::Result<Signature> {
        self.send_transaction_inner(instructions)
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
mod client_tests {
    use super::*;

    /// `parse_transaction_error` must surface the program-level `TransactionError`
    /// from the confirmed-error shape and yield `None` for transport errors that
    /// carry no on-chain result.
    #[test]
    fn parse_transaction_error_extracts_program_result() {
        let tx_err = TransactionError::InstructionError(0, InstructionError::InvalidAccountData);
        let client_err = ClientError::from(ClientErrorKind::TransactionError(tx_err.clone()));
        assert_eq!(DZClient::parse_transaction_error(&client_err), Some(tx_err));

        // A bare custom (non-transaction) client error has no program result.
        let transport = ClientError::from(ClientErrorKind::Custom("connection reset".into()));
        assert_eq!(DZClient::parse_transaction_error(&transport), None);
    }

    fn rpc_response_error(code: i64, message: &str) -> ClientError {
        ClientError::from(ClientErrorKind::RpcError(
            solana_rpc_client_api::request::RpcError::RpcResponseError {
                code,
                message: message.to_string(),
                data: solana_rpc_client_api::request::RpcResponseErrorData::Empty,
            },
        ))
    }

    /// Transport errors stay retryable.
    #[test]
    fn is_retryable_rpc_error_covers_transport() {
        let io = ClientError::from(ClientErrorKind::Io(std::io::Error::other("reset")));
        assert!(DZClient::is_retryable_rpc_error(&io));
    }

    /// A transient status carried inside a decoded JSON-RPC envelope is now
    /// retried — the shape that reached Go as `*RPCError` on 2026-07-28.
    #[test]
    fn is_retryable_rpc_error_covers_envelope_status_and_busy_codes() {
        // HTTP status a provider LB put in the envelope code.
        assert!(DZClient::is_retryable_rpc_error(&rpc_response_error(
            503,
            "backend unavailable"
        )));
        assert!(DZClient::is_retryable_rpc_error(&rpc_response_error(
            429,
            "slow down"
        )));
        // "busy, retry later" node codes.
        for code in [-32005, -32004, -32429] {
            assert!(
                DZClient::is_retryable_rpc_error(&rpc_response_error(code, "node is behind")),
                "code {code} should retry"
            );
        }
        // Transient wording with no recognizable code (the Helius `-32603` shape).
        assert!(DZClient::is_retryable_rpc_error(&rpc_response_error(
            -32603,
            "Service unavailable, please try again later."
        )));
    }

    /// Deterministic rejections and request-level errors do not retry.
    #[test]
    fn is_retryable_rpc_error_excludes_permanent_rpc_errors() {
        for code in [-32002, -32602, -32601, -32003, -32011] {
            assert!(
                !DZClient::is_retryable_rpc_error(&rpc_response_error(code, "rejected")),
                "code {code} must not retry"
            );
        }
        // A generic -32603 with no transient wording is not retryable either.
        assert!(!DZClient::is_retryable_rpc_error(&rpc_response_error(
            -32603,
            "Internal error"
        )));
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
