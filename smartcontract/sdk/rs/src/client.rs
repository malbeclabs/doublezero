use base64::{engine::general_purpose, Engine};
use chrono::{DateTime, NaiveDateTime, Utc};
use doublezero_config::Environment;

use crate::config::default_program_id;
use doublezero_serviceability::{
    error::DoubleZeroError, instructions::*, state::accounttype::AccountType,
};
use eyre::{bail, eyre, OptionExt};
use log::debug;
use solana_account_decoder::UiAccountEncoding;
use solana_client::{
    pubsub_client::PubsubClient,
    rpc_client::RpcClient,
    rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig},
    rpc_filter::{Memcmp, MemcmpEncodedBytes, RpcFilterType},
};
use solana_sdk::{
    account::Account,
    commitment_config::CommitmentConfig,
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
        Arc,
    },
};

use crate::{
    config::*, doublezeroclient::DoubleZeroClient, dztransaction::DZTransaction,
    keypair::load_keypair, rpckeyedaccount_decode::rpckeyedaccount_decode, AccountData,
};

pub struct DZClient {
    rpc_url: String,
    client: RpcClient,
    rpc_ws_url: String,
    payer: Option<Keypair>,
    pub(crate) program_id: Pubkey,
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
        })
    }

    pub fn get_rpc(&self) -> &String {
        &self.rpc_url
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

    pub fn get_balance(&self) -> eyre::Result<u64> {
        let payer = self
            .payer
            .as_ref()
            .ok_or_else(|| eyre!("No payer configured for client!"))?;

        self.client
            .get_balance(&payer.pubkey())
            .map_err(|e| eyre!(e))
    }

    pub fn get_epoch(&self) -> eyre::Result<u64> {
        self.client
            .get_epoch_info()
            .map_err(|e| eyre!(e))
            .map(|info| info.epoch)
    }

    pub fn get_account(&self, pubkey: Pubkey) -> eyre::Result<Account> {
        self.client.get_account(&pubkey).map_err(|e| eyre!(e))
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

        let signatures = self.client.get_signatures_for_address(pubkey)?;

        for signature_info in signatures {
            let signature = Signature::from_str(&signature_info.signature)?;

            if let Ok(trans) = self
                .client
                .get_transaction(&signature, UiTransactionEncoding::Base64)
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
        self.client
            .get_balance(&self.get_payer())
            .map_err(|e| eyre!(e))
    }

    fn get_epoch(&self) -> eyre::Result<u64> {
        self.client
            .get_epoch_info()
            .map_err(|e| eyre!(e))
            .map(|info| info.epoch)
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

        let accounts = self
            .client
            .get_program_accounts_with_config(&self.program_id, options)?;

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
        let payer = self
            .payer
            .as_ref()
            .ok_or_eyre("No default signer found, run \"doublezero keygen\" to create a new one")?;
        let data = instruction.pack();

        let mut transaction = Transaction::new_with_payer(
            &[Instruction::new_with_bytes(
                self.program_id,
                &data,
                [
                    accounts,
                    vec![
                        AccountMeta::new(payer.pubkey(), true),
                        AccountMeta::new(program::id(), false),
                    ],
                ]
                .concat(),
            )],
            Some(&payer.pubkey()),
        );

        let blockhash = self.client.get_latest_blockhash().map_err(|e| eyre!(e))?;
        transaction.sign(&[&payer], blockhash);

        debug!("Simulating transaction: {transaction:?}");

        let result = self.client.simulate_transaction(&transaction)?;
        if result.value.err.is_some() {
            eprintln!("Program Logs:");
            if let Some(logs) = result.value.logs {
                for log in logs {
                    eprintln!("{log}");
                }
            }
        }

        if let Some(TransactionError::InstructionError(_index, InstructionError::Custom(number))) =
            result.value.err
        {
            return Err(eyre!(DoubleZeroError::from(number)));
        } else if let Some(err) = result.value.err {
            return Err(eyre!(err));
        }

        self.client
            .send_and_confirm_transaction(&transaction)
            .map_err(|e| eyre!(e))
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
        let accounts = self
            .client
            .get_program_accounts_with_config(self.get_program_id(), options)?;

        for (pubkey, account) in accounts {
            assert!(account.data[0] == account_type, "Invalid account type");
            list.insert(pubkey, AccountData::try_from(&account.data[..])?);
        }

        Ok(list)
    }

    fn get(&self, pubkey: Pubkey) -> eyre::Result<AccountData> {
        match self.client.get_account(&pubkey) {
            Ok(account) => {
                if account.owner == self.program_id {
                    let data = account.data;
                    Ok(AccountData::try_from(&data[..])?)
                } else {
                    Ok(AccountData::None)
                }
            }
            Err(e) => Err(eyre!(e)),
        }
    }

    fn get_account(&self, pubkey: Pubkey) -> eyre::Result<Account> {
        self.client.get_account(&pubkey).map_err(|e| eyre!(e))
    }

    fn get_program_accounts(
        &self,
        program_id: &Pubkey,
        config: RpcProgramAccountsConfig,
    ) -> eyre::Result<Vec<(Pubkey, Account)>> {
        self.client
            .get_program_accounts_with_config(program_id, config)
            .map_err(|e| eyre!(e))
    }

    #[allow(deprecated)]
    fn get_transactions(&self, pubkey: Pubkey) -> eyre::Result<Vec<DZTransaction>> {
        let mut transactions: Vec<DZTransaction> = Vec::new();

        let signatures = self.client.get_signatures_for_address(&pubkey)?;

        for signature_info in signatures.into_iter() {
            let signature = Signature::from_str(&signature_info.signature)?;
            let enc_transaction = self
                .client
                .get_transaction(&signature, UiTransactionEncoding::Base64)?;

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
