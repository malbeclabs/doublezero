use eyre::eyre;
use std::collections::HashMap;

use crate::{doublezeroclient::DoubleZeroClient, DZClient};
use double_zero_sla_program::{
    instructions::DoubleZeroInstruction,
    pda::get_exchange_pda,
    processors::exchange::{
        create::ExchangeCreateArgs, delete::ExchangeDeleteArgs, reactivate::ExchangeReactivateArgs,
        suspend::ExchangeSuspendArgs, update::ExchangeUpdateArgs,
    },
    state::{accountdata::AccountData, accounttype::AccountType, exchange::Exchange},
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

pub trait ExchangeService {
    fn get_exchanges(&self) -> eyre::Result<HashMap<Pubkey, Exchange>>;
    fn get_exchange(&self, pubkey: &Pubkey) -> eyre::Result<Exchange>;
    fn find_exchange<P>(&self, predicate: P) -> eyre::Result<(Pubkey, Exchange)>
    where
        P: Fn(&Exchange) -> bool + Send + Sync;
    fn create_exchange(
        &self,
        code: &str,
        name: &str,
        lat: f64,
        lng: f64,
        loc_id: u32,
    ) -> eyre::Result<(Signature, Pubkey)>;
    fn update_exchange(
        &self,
        index: u128,
        code: Option<String>,
        name: Option<String>,
        lat: Option<f64>,
        lng: Option<f64>,
        loc_id: Option<u32>,
    ) -> eyre::Result<Signature>;
    fn suspend_exchange(&self, index: u128) -> eyre::Result<Signature>;
    fn reactivate_exchange(&self, index: u128) -> eyre::Result<Signature>;
    fn delete_exchange(&self, index: u128) -> eyre::Result<Signature>;
}

impl ExchangeService for DZClient {
    fn get_exchanges(&self) -> eyre::Result<HashMap<Pubkey, Exchange>> {
        Ok(self
            .gets(AccountType::Exchange)?
            .into_iter()
            .map(|(k, v)| match v {
                AccountData::Exchange(exchange) => (k, exchange),
                _ => panic!("Invalid Account Type"),
            })
            .collect())
    }

    fn get_exchange(&self, pubkey: &Pubkey) -> eyre::Result<Exchange> {
        let account = self.get(*pubkey)?;

        match account {
            AccountData::Exchange(exchange) => Ok(exchange),
            _ => Err(eyre!("Invalid Account Type")),
        }
    }

    fn find_exchange<P>(&self, predicate: P) -> eyre::Result<(Pubkey, Exchange)>
    where
        P: Fn(&Exchange) -> bool + Send + Sync,
    {
        let exchanges = self.get_exchanges()?;

        match exchanges
            .into_iter()
            .find(|(_, exchange)| predicate(exchange))
        {
            Some((pubkey, exchange)) => Ok((pubkey, exchange)),
            None => Err(eyre!("Exchange not found")),
        }
    }

    fn create_exchange(
        &self,
        code: &str,
        name: &str,
        lat: f64,
        lng: f64,
        loc_id: u32,
    ) -> eyre::Result<(Signature, Pubkey)> {
        let (globalstate_pubkey, globalstate) = self.get_globalstate()?;
        let (pda_pubkey, _) =
            get_exchange_pda(&self.get_program_id(), globalstate.account_index + 1);

        self.execute_transaction(
            DoubleZeroInstruction::CreateExchange(ExchangeCreateArgs {
                index: globalstate.account_index + 1,
                code: code.to_owned(),
                name: name.to_string(),
                lat,
                lng,
                loc_id,
            }),
            vec![
                AccountMeta::new(pda_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
        .map(|sig| (sig, pda_pubkey))
    }

    fn update_exchange(
        &self,
        index: u128,
        code: Option<String>,
        name: Option<String>,
        lat: Option<f64>,
        lng: Option<f64>,
        loc_id: Option<u32>,
    ) -> eyre::Result<Signature> {
        match self.get_globalstate() {
            Ok((globalstate_pubkey, globalstate)) => {
                if !globalstate.foundation_allowlist.contains(&self.get_payer()) {
                    return Err(eyre!("User not allowlisted"));
                }

                let (pda_pubkey, _) = get_exchange_pda(&self.get_program_id(), index);

                self.execute_transaction(
                    DoubleZeroInstruction::UpdateExchange(ExchangeUpdateArgs {
                        index,
                        code,
                        name,
                        lat,
                        lng,
                        loc_id,
                    }),
                    vec![
                        AccountMeta::new(pda_pubkey, false),
                        AccountMeta::new(globalstate_pubkey, false),
                    ],
                )
            }
            Err(e) => Err(e),
        }
    }

    fn suspend_exchange(&self, index: u128) -> eyre::Result<Signature> {
        match self.get_globalstate() {
            Ok((globalstate_pubkey, globalstate)) => {
                if !globalstate.foundation_allowlist.contains(&self.get_payer()) {
                    return Err(eyre!("User not allowlisted"));
                }

                let (pda_pubkey, _) = get_exchange_pda(&self.get_program_id(), index);

                self.execute_transaction(
                    DoubleZeroInstruction::SuspendExchange(ExchangeSuspendArgs { index }),
                    vec![
                        AccountMeta::new(pda_pubkey, false),
                        AccountMeta::new(globalstate_pubkey, false),
                    ],
                )
            }
            Err(e) => Err(e),
        }
    }

    fn reactivate_exchange(&self, index: u128) -> eyre::Result<Signature> {
        match self.get_globalstate() {
            Ok((globalstate_pubkey, globalstate)) => {
                if !globalstate.foundation_allowlist.contains(&self.get_payer()) {
                    return Err(eyre!("User not allowlisted"));
                }

                let (pda_pubkey, _) = get_exchange_pda(&self.get_program_id(), index);

                self.execute_transaction(
                    DoubleZeroInstruction::ReactivateExchange(ExchangeReactivateArgs { index }),
                    vec![
                        AccountMeta::new(pda_pubkey, false),
                        AccountMeta::new(globalstate_pubkey, false),
                    ],
                )
            }
            Err(e) => Err(e),
        }
    }

    fn delete_exchange(&self, index: u128) -> eyre::Result<Signature> {
        match self.get_globalstate() {
            Ok((globalstate_pubkey, globalstate)) => {
                if !globalstate.foundation_allowlist.contains(&self.get_payer()) {
                    return Err(eyre!("User not allowlisted"));
                }

                let (pda_pubkey, _) = get_exchange_pda(&self.get_program_id(), index);

                self.execute_transaction(
                    DoubleZeroInstruction::DeleteExchange(ExchangeDeleteArgs { index }),
                    vec![
                        AccountMeta::new(pda_pubkey, false),
                        AccountMeta::new(globalstate_pubkey, false),
                    ],
                )
            }
            Err(e) => Err(e),
        }
    }
}
