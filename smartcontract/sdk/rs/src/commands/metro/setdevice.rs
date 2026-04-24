use crate::{
    commands::{globalstate::get::GetGlobalStateCommand, metro::get::GetMetroCommand},
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    processors::metro::setdevice::{MetroSetDeviceArgs, SetDeviceOption},
    state::metro::MetroStatus,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct SetDeviceMetroCommand {
    pub pubkey: Pubkey,
    // Device pubkey for the exchange (to be set or unset)
    pub device1_pubkey: Option<Pubkey>,
    pub device2_pubkey: Option<Pubkey>,
}

impl SetDeviceMetroCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (_, metro) = GetMetroCommand {
            pubkey_or_code: self.pubkey.to_string(),
        }
        .execute(client)?;

        if metro.status != MetroStatus::Activated {
            return Err(eyre::eyre!("Metro is not active"));
        }

        let mut signature: Signature = Signature::default();

        if (metro.device1_pk == Pubkey::default() && self.device1_pubkey.is_some())
            || (metro.device1_pk != Pubkey::default() && self.device1_pubkey.is_none())
        {
            signature = client.execute_transaction(
                DoubleZeroInstruction::SetDeviceMetro(MetroSetDeviceArgs {
                    index: 1,
                    set: if self.device1_pubkey.is_some() {
                        SetDeviceOption::Set
                    } else {
                        SetDeviceOption::Remove
                    },
                }),
                vec![
                    AccountMeta::new(self.pubkey, false),
                    AccountMeta::new(self.device1_pubkey.unwrap_or(metro.device1_pk), false),
                    AccountMeta::new(globalstate_pubkey, false),
                ],
            )?;
        }

        if (metro.device2_pk == Pubkey::default() && self.device2_pubkey.is_some())
            || (metro.device2_pk != Pubkey::default() && self.device2_pubkey.is_none())
        {
            signature = client.execute_transaction(
                DoubleZeroInstruction::SetDeviceMetro(MetroSetDeviceArgs {
                    index: 2,
                    set: if self.device2_pubkey.is_some() {
                        SetDeviceOption::Set
                    } else {
                        SetDeviceOption::Remove
                    },
                }),
                vec![
                    AccountMeta::new(self.pubkey, false),
                    AccountMeta::new(self.device2_pubkey.unwrap_or(metro.device2_pk), false),
                    AccountMeta::new(globalstate_pubkey, false),
                ],
            )?;
        }

        Ok(signature)
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::metro::setdevice::SetDeviceMetroCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_metro_pda},
        processors::metro::setdevice::{MetroSetDeviceArgs, SetDeviceOption},
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            metro::{Metro, MetroStatus},
        },
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_metro_setdevice1_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (metro_pubkey, _) = get_metro_pda(&client.get_program_id(), 1);
        let device_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");

        let metro = Metro {
            account_type: AccountType::Metro,
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 42,
            reference_count: 0,
            lat: 50.0,
            lng: 20.0,
            bgp_community: 1234,
            unused: 0,
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            status: MetroStatus::Activated,
            code: "TestExchange".to_string(),
            name: "TestName".to_string(),
        };

        let mut seq = mockall::Sequence::new();
        client
            .expect_get()
            .times(1)
            .in_sequence(&mut seq)
            .with(predicate::eq(metro_pubkey))
            .returning(move |_| Ok(AccountData::Metro(metro.clone())));

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::SetDeviceMetro(MetroSetDeviceArgs {
                    index: 1,
                    set: SetDeviceOption::Set,
                })),
                predicate::eq(vec![
                    AccountMeta::new(metro_pubkey, false),
                    AccountMeta::new(device_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = SetDeviceMetroCommand {
            pubkey: metro_pubkey,
            device1_pubkey: Some(device_pubkey),
            device2_pubkey: None,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_metro_setdevice2_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (metro_pubkey, _) = get_metro_pda(&client.get_program_id(), 1);
        let device_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");

        let metro = Metro {
            account_type: AccountType::Metro,
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 42,
            reference_count: 0,
            lat: 50.0,
            lng: 20.0,
            bgp_community: 1234,
            unused: 0,
            device1_pk: device_pubkey,
            device2_pk: Pubkey::default(),
            status: MetroStatus::Activated,
            code: "TestExchange".to_string(),
            name: "TestName".to_string(),
        };

        let mut seq = mockall::Sequence::new();
        client
            .expect_get()
            .times(1)
            .in_sequence(&mut seq)
            .with(predicate::eq(metro_pubkey))
            .returning(move |_| Ok(AccountData::Metro(metro.clone())));

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::SetDeviceMetro(MetroSetDeviceArgs {
                    index: 1,
                    set: SetDeviceOption::Remove,
                })),
                predicate::eq(vec![
                    AccountMeta::new(metro_pubkey, false),
                    AccountMeta::new(device_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = SetDeviceMetroCommand {
            pubkey: metro_pubkey,
            device1_pubkey: None,
            device2_pubkey: None,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_metro_setdevice3_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (metro_pubkey, _) = get_metro_pda(&client.get_program_id(), 1);

        let device1_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");
        let device2_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzdvYqNpaRAUo");

        let metro = Metro {
            account_type: AccountType::Metro,
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 42,
            reference_count: 0,
            lat: 50.0,
            lng: 20.0,
            bgp_community: 1234,
            unused: 0,
            device1_pk: device1_pubkey,
            device2_pk: device2_pubkey,
            status: MetroStatus::Activated,
            code: "TestExchange".to_string(),
            name: "TestName".to_string(),
        };

        let mut seq = mockall::Sequence::new();
        client
            .expect_get()
            .times(1)
            .in_sequence(&mut seq)
            .with(predicate::eq(metro_pubkey))
            .returning(move |_| Ok(AccountData::Metro(metro.clone())));

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::SetDeviceMetro(MetroSetDeviceArgs {
                    index: 1,
                    set: SetDeviceOption::Remove,
                })),
                predicate::eq(vec![
                    AccountMeta::new(metro_pubkey, false),
                    AccountMeta::new(device1_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::SetDeviceMetro(MetroSetDeviceArgs {
                    index: 2,
                    set: SetDeviceOption::Remove,
                })),
                predicate::eq(vec![
                    AccountMeta::new(metro_pubkey, false),
                    AccountMeta::new(device2_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = SetDeviceMetroCommand {
            pubkey: metro_pubkey,
            device1_pubkey: None,
            device2_pubkey: None,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_metro_setdevice4_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (metro_pubkey, _) = get_metro_pda(&client.get_program_id(), 1);

        let device1_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");
        let device2_pubkey = Pubkey::new_unique();

        let metro = Metro {
            account_type: AccountType::Metro,
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 42,
            reference_count: 0,
            lat: 50.0,
            lng: 20.0,
            bgp_community: 1234,
            unused: 0,
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            status: MetroStatus::Activated,
            code: "TestExchange".to_string(),
            name: "TestName".to_string(),
        };

        let mut seq = mockall::Sequence::new();
        client
            .expect_get()
            .times(1)
            .in_sequence(&mut seq)
            .with(predicate::eq(metro_pubkey))
            .returning(move |_| Ok(AccountData::Metro(metro.clone())));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::SetDeviceMetro(MetroSetDeviceArgs {
                    index: 1,
                    set: SetDeviceOption::Set,
                })),
                predicate::eq(vec![
                    AccountMeta::new(metro_pubkey, false),
                    AccountMeta::new(device1_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::SetDeviceMetro(MetroSetDeviceArgs {
                    index: 2,
                    set: SetDeviceOption::Set,
                })),
                predicate::eq(vec![
                    AccountMeta::new(metro_pubkey, false),
                    AccountMeta::new(device2_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = SetDeviceMetroCommand {
            pubkey: metro_pubkey,
            device1_pubkey: Some(device1_pubkey),
            device2_pubkey: Some(device2_pubkey),
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
