use eyre::eyre;
use mockall::automock;
use std::collections::HashMap;

use crate::{doublezeroclient::DoubleZeroClient, DZClient};
use double_zero_sla_program::{
    instructions::DoubleZeroInstruction,
    pda::get_device_pda,
    processors::device::{
        activate::DeviceActivateArgs, create::DeviceCreateArgs, deactivate::DeviceDeactivateArgs,
        delete::DeviceDeleteArgs, reactivate::DeviceReactivateArgs, reject::DeviceRejectArgs,
        suspend::DeviceSuspendArgs, update::DeviceUpdateArgs,
    },
    state::{
        accountdata::AccountData,
        accounttype::AccountType,
        device::{Device, DeviceType},
    },
    types::{IpV4, NetworkV4List},
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[automock]
pub trait DeviceService {
    fn get_devices(&self) -> eyre::Result<HashMap<Pubkey, Device>>;
    fn get_device(&self, pubkey: &Pubkey) -> eyre::Result<Device>;
    fn create_device(
        &self,
        code: &str,
        location_pk: Pubkey,
        exchange_pk: Pubkey,
        device_type: DeviceType,
        public_ip: IpV4,
        dz_prefixes: NetworkV4List,
    ) -> eyre::Result<(Signature, Pubkey)>;
    fn update_device(
        &self,
        index: u128,
        code: Option<String>,
        device_type: Option<DeviceType>,
        public_ip: Option<IpV4>,
        dz_prefixes: Option<NetworkV4List>,
    ) -> eyre::Result<Signature>;
    fn activate_device(&self, index: u128) -> eyre::Result<Signature>;
    fn reject_device(&self, index: u128, error: String) -> eyre::Result<Signature>;
    fn suspend_device(&self, index: u128) -> eyre::Result<Signature>;
    fn reactivate_device(&self, index: u128) -> eyre::Result<Signature>;
    fn delete_device(&self, index: u128) -> eyre::Result<Signature>;
    fn deactivate_device(&self, index: u128, owner: Pubkey) -> eyre::Result<Signature>;
}

pub trait DeviceFinder {
    #![allow(dead_code)]
    fn find_device<P>(&self, predicate: P) -> eyre::Result<(Pubkey, Device)>
    where
        P: Fn(&Device) -> bool + Send;
}

impl DeviceService for DZClient {
    fn get_devices(&self) -> eyre::Result<HashMap<Pubkey, Device>> {
        Ok(self
            .gets(AccountType::Device)?
            .into_iter()
            .map(|(k, v)| match v {
                AccountData::Device(device) => (k, device),
                _ => panic!("Invalid Account Type"),
            })
            .collect())
    }

    fn get_device(&self, pubkey: &Pubkey) -> eyre::Result<Device> {
        let account = self.get(*pubkey)?;

        match account {
            AccountData::Device(device) => Ok(device),
            _ => Err(eyre!("Invalid Account Type")),
        }
    }

    fn create_device(
        &self,
        code: &str,
        location_pk: Pubkey,
        exchange_pk: Pubkey,
        device_type: DeviceType,
        public_ip: IpV4,
        dz_prefixes: NetworkV4List,
    ) -> eyre::Result<(Signature, Pubkey)> {
        match self.get_globalstate() {
            Ok((globalstate_pubkey, globalstate)) => {
                if !globalstate.device_allowlist.contains(&self.get_payer()) {
                    return Err(eyre!("Contributor not allowlisted"));
                }

                let (pda_pubkey, _) =
                    get_device_pda(&self.get_program_id(), globalstate.account_index + 1);

                self.execute_transaction(
                    DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
                        index: globalstate.account_index + 1,
                        code: code.to_owned(),
                        location_pk,
                        exchange_pk,
                        device_type,
                        public_ip,
                        dz_prefixes,
                    }),
                    vec![
                        AccountMeta::new(pda_pubkey, false),
                        AccountMeta::new(location_pk, false),
                        AccountMeta::new(exchange_pk, false),
                        AccountMeta::new(globalstate_pubkey, false),
                    ],
                )
                .map(|signature| (signature, pda_pubkey))
            }
            Err(e) => Err(e),
        }
    }

    fn update_device(
        &self,
        index: u128,
        code: Option<String>,
        device_type: Option<DeviceType>,
        public_ip: Option<IpV4>,
        dz_prefixes: Option<NetworkV4List>,
    ) -> eyre::Result<Signature> {
        match self.get_globalstate() {
            Ok((globalstate_pubkey, globalstate)) => {
                if !globalstate.foundation_allowlist.contains(&self.get_payer()) {
                    return Err(eyre!("User not allowlisted"));
                }

                let (pda_pubkey, _) = get_device_pda(&self.get_program_id(), index);

                self.execute_transaction(
                    DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
                        index,
                        code: code.to_owned(),
                        device_type,
                        public_ip,
                        dz_prefixes,
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

    fn activate_device(&self, index: u128) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_device_pda(&self.get_program_id(), index);

        match self.get_globalstate() {
            Ok((globalstate_pubkey, globalstate)) => {
                if !globalstate.foundation_allowlist.contains(&self.get_payer()) {
                    return Err(eyre!("User not allowlisted"));
                }

                self.execute_transaction(
                    DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs { index }),
                    vec![
                        AccountMeta::new(pda_pubkey, false),
                        AccountMeta::new(globalstate_pubkey, false),
                    ],
                )
            }
            Err(e) => Err(e),
        }
    }

    fn reject_device(&self, index: u128, error: String) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_device_pda(&self.get_program_id(), index);

        match self.get_globalstate() {
            Ok((globalstate_pubkey, globalstate)) => {
                if !globalstate.foundation_allowlist.contains(&self.get_payer()) {
                    return Err(eyre!("User not allowlisted"));
                }

                self.execute_transaction(
                    DoubleZeroInstruction::RejectDevice(DeviceRejectArgs { index, error }),
                    vec![
                        AccountMeta::new(pda_pubkey, false),
                        AccountMeta::new(globalstate_pubkey, false),
                    ],
                )
            }
            Err(e) => Err(e),
        }
    }

    fn suspend_device(&self, index: u128) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_device_pda(&self.get_program_id(), index);

        self.execute_transaction(
            DoubleZeroInstruction::SuspendDevice(DeviceSuspendArgs { index }),
            vec![AccountMeta::new(pda_pubkey, false)],
        )
    }

    fn reactivate_device(&self, index: u128) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_device_pda(&self.get_program_id(), index);

        self.execute_transaction(
            DoubleZeroInstruction::ReactivateDevice(DeviceReactivateArgs { index }),
            vec![AccountMeta::new(pda_pubkey, false)],
        )
    }

    fn delete_device(&self, index: u128) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_device_pda(&self.get_program_id(), index);

        self.execute_transaction(
            DoubleZeroInstruction::DeleteDevice(DeviceDeleteArgs { index }),
            vec![AccountMeta::new(pda_pubkey, false)],
        )
    }

    fn deactivate_device(&self, index: u128, owner: Pubkey) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_device_pda(&self.get_program_id(), index);

        match self.get_globalstate() {
            Ok((globalstate_pubkey, globalstate)) => {
                if !globalstate.foundation_allowlist.contains(&self.get_payer()) {
                    return Err(eyre!("User not allowlisted"));
                }

                self.execute_transaction(
                    DoubleZeroInstruction::DeactivateDevice(DeviceDeactivateArgs { index }),
                    vec![
                        AccountMeta::new(pda_pubkey, false),
                        AccountMeta::new(owner, false),
                        AccountMeta::new(globalstate_pubkey, false),
                    ],
                )
            }
            Err(e) => Err(e),
        }
    }
}

impl DeviceFinder for DZClient {
    fn find_device<P>(&self, predicate: P) -> eyre::Result<(Pubkey, Device)>
    where
        P: Fn(&Device) -> bool + Send,
    {
        let devices = self.get_devices()?;

        match devices.into_iter().find(|(_, device)| predicate(device)) {
            Some((pubkey, device)) => Ok((pubkey, device)),
            None => Err(eyre!("Device not found")),
        }
    }
}
