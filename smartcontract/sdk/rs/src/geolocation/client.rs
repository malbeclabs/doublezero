use crate::{
    config::{
        convert_geo_program_moniker, convert_url_moniker, default_geolocation_program_id,
        read_doublezero_config,
    },
    keypair::load_keypair,
};
use doublezero_geolocation::instructions::GeolocationInstruction;
use eyre::{eyre, OptionExt};
use log::debug;
use mockall::automock;
use solana_client::rpc_client::RpcClient;
use solana_rpc_client_api::config::RpcProgramAccountsConfig;
use solana_sdk::{
    account::Account,
    commitment_config::CommitmentConfig,
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
    signature::{Keypair, Signature, Signer},
    transaction::Transaction,
};
use solana_system_interface::program;
use std::{path::PathBuf, str::FromStr};

#[automock]
pub trait GeolocationClient {
    fn get_program_id(&self) -> Pubkey;
    fn get_payer(&self) -> Pubkey;
    fn get_account(&self, pubkey: Pubkey) -> eyre::Result<Account>;
    fn get_program_accounts(
        &self,
        program_id: &Pubkey,
        config: RpcProgramAccountsConfig,
    ) -> eyre::Result<Vec<(Pubkey, Account)>>;
    fn execute_transaction(
        &self,
        instruction: GeolocationInstruction,
        accounts: Vec<AccountMeta>,
    ) -> eyre::Result<Signature>;
}

pub struct GeoClient {
    client: RpcClient,
    payer: Option<Keypair>,
    pub(crate) program_id: Pubkey,
}

impl GeoClient {
    pub fn new(
        rpc_url: Option<String>,
        program_id: Option<String>,
        keypair: Option<PathBuf>,
    ) -> eyre::Result<GeoClient> {
        let (_, config) = read_doublezero_config()?;

        let rpc_url = convert_url_moniker(rpc_url.unwrap_or(config.json_rpc_url));
        let client = RpcClient::new_with_commitment(rpc_url, CommitmentConfig::confirmed());
        let payer = load_keypair(keypair, None, config.keypair_path)
            .ok()
            .map(|r| r.keypair);

        let program_id = match program_id {
            None => default_geolocation_program_id(),
            Some(pg_id) => {
                let converted_id = convert_geo_program_moniker(pg_id);
                Pubkey::from_str(&converted_id).map_err(|_| eyre!("Invalid program ID"))?
            }
        };

        Ok(GeoClient {
            client,
            payer,
            program_id,
        })
    }
}

impl GeolocationClient for GeoClient {
    fn get_program_id(&self) -> Pubkey {
        self.program_id
    }

    fn get_payer(&self) -> Pubkey {
        self.payer.as_ref().map(|k| k.pubkey()).unwrap_or_default()
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

    fn execute_transaction(
        &self,
        instruction: GeolocationInstruction,
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
        transaction.sign(&[payer], blockhash);

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

        if let Some(err) = result.value.err {
            return Err(eyre!(err));
        }

        self.client
            .send_and_confirm_transaction(&transaction)
            .map_err(|e| eyre!(e))
    }
}
