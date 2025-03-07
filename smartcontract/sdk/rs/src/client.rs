use async_trait::async_trait;
use base64::prelude::*;
use base64::{engine::general_purpose, Engine};
use bincode::deserialize;
use chrono::{DateTime, NaiveDateTime, Utc};
use double_zero_sla_program::{
    instructions::*,
    pda::*,
    processors::globalconfig::set::SetGlobalConfigArgs,
    state::{accounttype::AccountType, globalconfig::GlobalConfig, globalstate::GlobalState},
    types::*,
};
use eyre::{eyre, OptionExt};
use solana_account_decoder::{UiAccountData, UiAccountEncoding};
use solana_client::{
    pubsub_client::PubsubClient,
    rpc_client::RpcClient,
    rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig},
    rpc_filter::{Memcmp, MemcmpEncodedBytes, RpcFilterType},
};
use solana_sdk::{
    commitment_config::CommitmentConfig,
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
    signature::{Keypair, Signature, Signer},
    system_program,
    transaction::Transaction,
};
use solana_transaction_status::{
    option_serializer::OptionSerializer, EncodedTransaction, TransactionBinaryEncoding,
    UiTransactionEncoding,
};
use std::collections::HashMap;
use std::str::FromStr;

use crate::dztransaction::DZTransaction;
use crate::utils::*;
use crate::{config::*, doublezeroclient::DoubleZeroClient, AccountData};

pub struct DZClient {
    rpc_url: String,
    client: RpcClient,
    rpc_ws_url: String,
    payer: Option<Keypair>,
    pub(crate) program_id: Pubkey,
}

impl DZClient {
    pub fn get_rpc(&self) -> &String {
        &self.rpc_url
    }
    pub fn get_ws(&self) -> &String {
        &self.rpc_ws_url
    }
}

#[async_trait]
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

    fn execute_transaction(
        &self,
        instruction: DoubleZeroInstruction,
        accounts: Vec<AccountMeta>,
    ) -> eyre::Result<Signature> {
        let payer = self
            .payer
            .as_ref()
            .ok_or_eyre("No default signer found, run \"doublezero keygen\" to create a new one")?;
        let data = borsh::to_vec(&instruction).unwrap();

        let mut transaction = Transaction::new_with_payer(
            &[Instruction::new_with_bytes(
                self.program_id,
                &data,
                [
                    accounts,
                    vec![
                        AccountMeta::new(payer.pubkey(), true),
                        AccountMeta::new(system_program::id(), false),
                    ],
                ]
                .concat(),
            )],
            Some(&payer.pubkey()),
        );

        let blockhash = self.client.get_latest_blockhash().map_err(|e| eyre!(e))?;

        transaction.sign(&[&payer], blockhash);

        let result = self
            .client
            .simulate_transaction(&transaction)
            .map_err(|e| eyre!(e))?;
        if result.value.err.is_some() {
            println!("Program Logs:");
            for log in result.value.logs.unwrap() {
                println!("{}", log);
            }
            return Err(eyre!("Error in transaction"));
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
        let config = RpcProgramAccountsConfig {
            filters: Some(filters),
            account_config: RpcAccountInfoConfig {
                encoding: Some(UiAccountEncoding::Base64),
                data_slice: None,
                commitment: None,
                min_context_slot: None,
            },
            with_context: None,
        };

        let mut list: HashMap<Pubkey, AccountData> = HashMap::new();
        let accounts = self
            .client
            .get_program_accounts_with_config(&self.program_id, config)?;

        for (pubkey, account) in accounts {
            assert!(account.data[0] == account_type, "Invalid account type");
            list.insert(pubkey, AccountData::from(&account.data[..]));
        }

        Ok(list)
    }

    fn get(&self, pubkey: Pubkey) -> eyre::Result<AccountData> {
        match self.client.get_account(&pubkey) {
            Ok(account) => {
                if account.owner == self.program_id {
                    let data = account.data;
                    Ok(AccountData::from(&data[..]))
                } else {
                    Ok(AccountData::None)
                }
            }
            Err(e) => Err(eyre!(e)),
        }
    }

    #[allow(deprecated)]
    fn get_transactions(&self, pubkey: Pubkey) -> eyre::Result<Vec<DZTransaction>> {
        let mut transactions: Vec<DZTransaction> = Vec::new();

        let signatures = self.client.get_signatures_for_address(&pubkey)?;

        for signature_info in signatures.into_iter() {
            let signature = Signature::from_str(&signature_info.signature).unwrap();
            let enc_transaction = self
                .client
                .get_transaction(&signature, UiTransactionEncoding::Base64)?;

            let time = enc_transaction.block_time.unwrap_or_default();

            let time = match NaiveDateTime::from_timestamp_opt(time, 0) {
                Some(dt) => DateTime::<Utc>::from_naive_utc_and_offset(dt, Utc),
                None => DateTime::<Utc>::from_timestamp_nanos(0),
            };

            let meta = enc_transaction.transaction.meta.unwrap();
            let trans = enc_transaction.transaction.transaction;

            if let EncodedTransaction::Binary(data, _enc) = trans {
                let data: &[u8] = &general_purpose::STANDARD.decode(data).unwrap();

                let tx: Transaction = match deserialize(data) {
                    Ok(tx) => tx,
                    Err(e) => {
                        eprintln!("Error al deserializar la transacción: {:?}", e);
                        panic!("");
                    }
                };

                for instr in tx.message.instructions.iter() {
                    let program_id = instr.program_id(&tx.message.account_keys);
                    let account = instr.accounts[instr.accounts.len() - 2];
                    let account = tx.message.account_keys[account as usize];

                    let instruction = {
                        if program_id == &self.program_id {
                            DoubleZeroInstruction::unpack(&instr.data).unwrap()
                        } else {
                            DoubleZeroInstruction::InitGlobalState()
                        }
                    };

                    let log_messages = {
                        if let OptionSerializer::Some(msgs) = &meta.log_messages {
                            msgs.clone()
                        } else {
                            vec![]
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

impl DZClient {
    pub fn new(
        rpc_url: Option<String>,
        websocket_url: Option<String>,
        program_id: Option<String>,
        kaypair: Option<String>,
    ) -> eyre::Result<DZClient> {
        let (_, config) = read_doublezero_config();

        let rpc_url = rpc_url.unwrap_or(config.json_rpc_url);
        let rpc_ws_url = websocket_url.unwrap_or(
            config
                .websocket_url
                .unwrap_or(convert_url_to_ws(&rpc_url.to_string())),
        );

        let client = RpcClient::new_with_commitment(rpc_url.clone(), CommitmentConfig::confirmed());

        let payer: Option<solana_sdk::signature::Keypair> =
            match read_keypair_from_file(kaypair.unwrap_or(config.keypair_path)) {
                Ok(keypair) => Some(keypair),
                Err(_) => None,
            };

        let program_id = {
            if program_id.is_none() {
                if config.program_id.is_none() {
                    double_zero_sla_program::addresses::testnet::program_id::id()
                } else {
                    Pubkey::from_str(&config.program_id.unwrap())
                        .map_err(|_| eyre!("Invalid program ID"))?
                }
            } else {
                Pubkey::from_str(&program_id.unwrap()).map_err(|_| eyre!("Invalid program ID"))?
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

    /******************************************************************************************************************************************/

    pub fn get_balance(&self) -> eyre::Result<u64> {
        self.client
            .get_balance(&self.payer.as_ref().unwrap().pubkey())
            .map_err(|e| eyre!(e))
    }

    pub fn get_globalstate(&self) -> eyre::Result<(Pubkey, GlobalState)> {
        let (pubkey, _) = get_globalstate_pda(&self.program_id);

        let account = self.get(pubkey)?;

        match account {
            AccountData::GlobalState(globalstate) => Ok((pubkey, globalstate)),
            _ => Err(eyre!("Invalid global state")),
        }
    }

    /******************************************************************************************************************************************/
    /******************************************************************************************************************************************/

    pub fn initialize_globalstate(&self) -> eyre::Result<(Pubkey, Signature)> {
        let (pda_pubkey, _) = get_globalstate_pda(&self.program_id);

        let signature = self.execute_transaction(
            DoubleZeroInstruction::InitGlobalState(),
            vec![AccountMeta::new(pda_pubkey, false)],
        )?;

        Ok((pda_pubkey, signature))
    }

    pub fn set_global_config(
        &self,
        local_asn: u32,
        remote_asn: u32,
        device_tunnel_block: NetworkV4,
        user_tunnel_block: NetworkV4,
    ) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_globalconfig_pda(&self.program_id);

        self.execute_transaction(
            DoubleZeroInstruction::SetGlobalConfig(SetGlobalConfigArgs {
                local_asn,
                remote_asn,
                tunnel_tunnel_block: device_tunnel_block,
                user_tunnel_block,
            }),
            vec![AccountMeta::new(pda_pubkey, false)],
        )
    }

    fn get_all(&self) -> eyre::Result<HashMap<Pubkey, AccountData>> {
        let config = RpcProgramAccountsConfig {
            filters: None,
            account_config: RpcAccountInfoConfig {
                encoding: Some(UiAccountEncoding::Base64),
                data_slice: None,
                commitment: None,
                min_context_slot: None,
            },
            with_context: None,
        };

        let mut list: HashMap<Pubkey, AccountData> = HashMap::new();

        let accounts = self
            .client
            .get_program_accounts_with_config(&self.program_id, config)?;

        for (pubkey, account) in accounts {
            list.insert(pubkey, AccountData::from(&account.data[..]));
        }

        Ok(list)
    }

    pub fn get_globalconfig(&self) -> eyre::Result<(Pubkey, GlobalConfig)> {
        let (pubkey, _) = get_globalconfig_pda(&self.program_id);
        let account = self.get(pubkey)?;

        match account {
            AccountData::GlobalConfig(config) => Ok((pubkey, config)),
            _ => Err(eyre!("Invalid Account Type")),
        }
    }

    pub fn gets_and_subscribe<F>(&self, mut action: F) -> eyre::Result<()>
    where
        F: FnMut(&DZClient, &Pubkey, &AccountData),
    {
        loop {
            match self.get_all() {
                Ok(accounts) => {
                    for (pubkey, account) in accounts {
                        action(self, &pubkey, &account);
                    }
                }
                Err(e) => {
                    eprintln!("Error: {}", e);
                }
            }

            match self.subscribe(&mut action) {
                Ok(_) => {}
                Err(e) => {
                    eprintln!("Error: {}", e);
                }
            }
        }
    }

    #[allow(clippy::collapsible_match)]
    pub fn subscribe<F>(&self, mut action: F) -> eyre::Result<()>
    where
        F: FnMut(&DZClient, &Pubkey, &AccountData),
    {
        loop {
            let options = RpcProgramAccountsConfig {
                filters: None, /*Some(vec![RpcFilterType::Memcmp(Memcmp::new(
                                   0,
                                   MemcmpEncodedBytes::Bytes(vec![AccountType::User as u8]),
                               ))]),*/
                account_config: RpcAccountInfoConfig {
                    encoding: Some(UiAccountEncoding::Base64),
                    data_slice: None,
                    commitment: Some(CommitmentConfig::confirmed()),
                    min_context_slot: None,
                },
                with_context: None,
            };

            let (mut _client, receiver) =
                PubsubClient::program_subscribe(&self.rpc_ws_url, &self.program_id, Some(options))
                    .map_err(|_| eyre!("Unable to program_subscribe"))?;

            for response in receiver {
                let event = response.value;

                if let UiAccountData::Binary(data, encoding) = event.account.data {
                    if let UiAccountEncoding::Base64 = encoding {
                        let pubkey = Pubkey::from_str(&event.pubkey)
                            .map_err(|e| eyre!("Unable to parse Pubkey:{}", e))?;
                        let bytes = BASE64_STANDARD
                            .decode(data.clone())
                            .map_err(|e| eyre!("Unable decode data: {}", e))?;
                        let account = AccountData::from(&bytes[..]);

                        action(self, &pubkey, &account);
                    }
                }
            }
        }
    }

    pub fn get_logs(&self, pubkey: &Pubkey) -> eyre::Result<Vec<String>> {
        let mut errors: Vec<String> = Vec::new();

        let signatures = self.client.get_signatures_for_address(pubkey)?;

        for signature_info in signatures {
            let signature = Signature::from_str(&signature_info.signature).unwrap();

            if let Ok(trans) = self
                .client
                .get_transaction(&signature, UiTransactionEncoding::Base64)
            {
                if let EncodedTransaction::Binary(_, base) = trans.transaction.transaction {
                    if base == TransactionBinaryEncoding::Base64 {
                        let meta = trans.transaction.meta.unwrap();

                        if let OptionSerializer::Some(msgs) = meta.log_messages {
                            for msg in msgs {
                                errors.push(msg.to_string());
                            }
                        }
                    }
                }
            }
        }

        Ok(errors)
    }
}
