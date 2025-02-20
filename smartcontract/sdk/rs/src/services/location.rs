use eyre::eyre;
use std::collections::HashMap;
use crate::{doublezeroclient::*, DZClient};
use double_zero_sla_program::{
    instructions::DoubleZeroInstruction,
    pda::get_location_pda,
    processors::location::{
        create::LocationCreateArgs, delete::LocationDeleteArgs, reactivate::LocationReactivateArgs,
        suspend::LocationSuspendArgs, update::LocationUpdateArgs,
    },
    state::{accountdata::AccountData, accounttype::AccountType, location::Location},
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

pub trait LocationService {
    fn get_locations(&self) -> eyre::Result<HashMap<Pubkey, Location>>;
    fn get_location(&self, pubkey: &Pubkey) -> eyre::Result<Location>;
    fn find_location<P>(&self, predicate: P) -> eyre::Result<(Pubkey, Location)>
    where
        P: Fn(&Location) -> bool + Send + Sync;
    fn create_location(
        &self,
        code: &str,
        name: &str,
        country: &str,
        lat: f64,
        lng: f64,
        loc_id: u32,
    ) -> eyre::Result<(Signature, Pubkey)>;
    #[allow(clippy::too_many_arguments)]
    fn update_location(
        &self,
        index: u128,
        code: Option<String>,
        name: Option<String>,
        country: Option<String>,
        lat: Option<f64>,
        lng: Option<f64>,
        loc_id: Option<u32>,
    ) -> eyre::Result<Signature>;
    fn suspend_location(&self, index: u128) -> eyre::Result<Signature>;
    fn reactivate_location(&self, index: u128) -> eyre::Result<Signature>;
    fn delete_location(&self, index: u128) -> eyre::Result<Signature>;
}

impl LocationService for DZClient {

    fn get_locations(&self) -> eyre::Result<HashMap<Pubkey, Location>> {
        Ok(self
            .gets(AccountType::Location)?
            .into_iter()
            .map(|(k, v)| match v {
                AccountData::Location(location) => (k, location),
                _ => panic!("Invalid Account Type"),
            })
            .collect())
    }

    fn get_location(&self, pubkey: &Pubkey) -> eyre::Result<Location> {
        let account = self.get(*pubkey)?;

        match account {
            AccountData::Location(location) => Ok(location),
            _ => Err(eyre!("Invalid Account Type")),
        }
    }

    fn find_location<P>(&self, predicate: P) -> eyre::Result<(Pubkey, Location)>
    where
        P: Fn(&Location) -> bool + Send + Sync,
    {
        let locations = self.get_locations()?;

        match locations
            .into_iter()
            .find(|(_, location)| predicate(location))
        {
            Some((pubkey, location)) => Ok((pubkey, location)),
            None => Err(eyre!("Location not found")),
        }
    }

    fn create_location(
        &self,
        code: &str,
        name: &str,
        country: &str,
        lat: f64,
        lng: f64,
        loc_id: u32,
    ) -> eyre::Result<(Signature, Pubkey)> {
        let (globalstate_pubkey, globalstate) = self.get_globalstate()?;
        let (pda_pubkey, _) = get_location_pda(&self.get_program_id(), globalstate.account_index + 1);
        self.execute_transaction(
            DoubleZeroInstruction::CreateLocation(LocationCreateArgs {
                index: globalstate.account_index + 1,
                code: code.to_owned(),
                name: name.to_string(),
                country: country.to_owned(),
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

    fn update_location(
        &self,
        index: u128,
        code: Option<String>,
        name: Option<String>,
        country: Option<String>,
        lat: Option<f64>,
        lng: Option<f64>,
        loc_id: Option<u32>,
    ) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_location_pda(&self.get_program_id(), index);

        self.execute_transaction(
            DoubleZeroInstruction::UpdateLocation(LocationUpdateArgs {
                index,
                code,
                name,
                country,
                lat,
                lng,
                loc_id,
            }),
            vec![AccountMeta::new(pda_pubkey, false)],
        )
    }

    fn suspend_location(&self, index: u128) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_location_pda(&self.get_program_id(), index);

        self.execute_transaction(
            DoubleZeroInstruction::SuspendLocation(LocationSuspendArgs { index }),
            vec![AccountMeta::new(pda_pubkey, false)],
        )
    }

    fn reactivate_location(&self, index: u128) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_location_pda(&self.get_program_id(), index);

        self.execute_transaction(
            DoubleZeroInstruction::ReactivateLocation(LocationReactivateArgs { index }),
            vec![AccountMeta::new(pda_pubkey, false)],
        )
    }

    fn delete_location(&self, index: u128) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_location_pda(&self.get_program_id(), index);

        self.execute_transaction(
            DoubleZeroInstruction::DeleteLocation(LocationDeleteArgs { index }),
            vec![AccountMeta::new(pda_pubkey, false)],
        )
    }
}
