use crate::{
    commands::{exchange::get::GetExchangeCommand, globalstate::get::GetGlobalStateCommand},
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    processors::exchange::setdevice::{ExchangeSetDeviceArgs, SetDeviceOpption},
    state::exchange::ExchangeStatus,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct SetDeviceExchangeCommand {
    pub pubkey: Pubkey,
    // Device pubkey for the exchange (to be set or unset)
    pub device1_pubkey: Option<Pubkey>,
    pub device2_pubkey: Option<Pubkey>,
}

impl SetDeviceExchangeCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (_, exchange) = GetExchangeCommand {
            pubkey_or_code: self.pubkey.to_string(),
        }
        .execute(client)?;

        if exchange.status != ExchangeStatus::Activated {
            return Err(eyre::eyre!("Exchange is not active"));
        }

        let mut signature: Signature = Signature::default();

        if (exchange.device1_pk == Pubkey::default() && self.device1_pubkey.is_some())
            || (exchange.device1_pk != Pubkey::default() && self.device1_pubkey.is_none())
        {
            signature = client.execute_transaction(
                DoubleZeroInstruction::SetDeviceExchange(ExchangeSetDeviceArgs {
                    index: 1,
                    set: if self.device1_pubkey.is_some() {
                        SetDeviceOpption::Set
                    } else {
                        SetDeviceOpption::Remove
                    },
                }),
                vec![
                    AccountMeta::new(self.pubkey, false),
                    AccountMeta::new(self.device1_pubkey.unwrap_or(exchange.device1_pk), false),
                    AccountMeta::new(globalstate_pubkey, false),
                ],
            )?;
        }

        if (exchange.device2_pk == Pubkey::default() && self.device2_pubkey.is_some())
            || (exchange.device2_pk != Pubkey::default() && self.device2_pubkey.is_none())
        {
            signature = client.execute_transaction(
                DoubleZeroInstruction::SetDeviceExchange(ExchangeSetDeviceArgs {
                    index: 2,
                    set: if self.device2_pubkey.is_some() {
                        SetDeviceOpption::Set
                    } else {
                        SetDeviceOpption::Remove
                    },
                }),
                vec![
                    AccountMeta::new(self.pubkey, false),
                    AccountMeta::new(self.device2_pubkey.unwrap_or(exchange.device2_pk), false),
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
        commands::exchange::setdevice::SetDeviceExchangeCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_exchange_pda, get_globalstate_pda},
        processors::exchange::setdevice::{ExchangeSetDeviceArgs, SetDeviceOpption},
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            exchange::{Exchange, ExchangeStatus},
        },
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_exchange_setdevice1_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (exchange_pubkey, _) = get_exchange_pda(&client.get_program_id(), 1);
        let device_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");

        let exchange = Exchange {
            account_type: AccountType::Exchange,
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 42,
            reference_count: 0,
            lat: 50.0,
            lng: 20.0,
            loc_id: 1234,
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            status: ExchangeStatus::Activated,
            code: "TestExchange".to_string(),
            name: "TestName".to_string(),
        };

        let mut seq = mockall::Sequence::new();
        client
            .expect_get()
            .times(1)
            .in_sequence(&mut seq)
            .with(predicate::eq(exchange_pubkey))
            .returning(move |_| Ok(AccountData::Exchange(exchange.clone())));

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::SetDeviceExchange(
                    ExchangeSetDeviceArgs {
                        index: 1,
                        set: SetDeviceOpption::Set,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(exchange_pubkey, false),
                    AccountMeta::new(device_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = SetDeviceExchangeCommand {
            pubkey: exchange_pubkey,
            device1_pubkey: Some(device_pubkey),
            device2_pubkey: None,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_exchange_setdevice2_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (exchange_pubkey, _) = get_exchange_pda(&client.get_program_id(), 1);
        let device_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");

        let exchange = Exchange {
            account_type: AccountType::Exchange,
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 42,
            reference_count: 0,
            lat: 50.0,
            lng: 20.0,
            loc_id: 1234,
            device1_pk: device_pubkey,
            device2_pk: Pubkey::default(),
            status: ExchangeStatus::Activated,
            code: "TestExchange".to_string(),
            name: "TestName".to_string(),
        };

        let mut seq = mockall::Sequence::new();
        client
            .expect_get()
            .times(1)
            .in_sequence(&mut seq)
            .with(predicate::eq(exchange_pubkey))
            .returning(move |_| Ok(AccountData::Exchange(exchange.clone())));

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::SetDeviceExchange(
                    ExchangeSetDeviceArgs {
                        index: 1,
                        set: SetDeviceOpption::Remove,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(exchange_pubkey, false),
                    AccountMeta::new(device_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = SetDeviceExchangeCommand {
            pubkey: exchange_pubkey,
            device1_pubkey: None,
            device2_pubkey: None,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_exchange_setdevice3_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (exchange_pubkey, _) = get_exchange_pda(&client.get_program_id(), 1);

        let device1_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");
        let device2_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzdvYqNpaRAUo");

        let exchange = Exchange {
            account_type: AccountType::Exchange,
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 42,
            reference_count: 0,
            lat: 50.0,
            lng: 20.0,
            loc_id: 1234,
            device1_pk: device1_pubkey,
            device2_pk: device2_pubkey,
            status: ExchangeStatus::Activated,
            code: "TestExchange".to_string(),
            name: "TestName".to_string(),
        };

        let mut seq = mockall::Sequence::new();
        client
            .expect_get()
            .times(1)
            .in_sequence(&mut seq)
            .with(predicate::eq(exchange_pubkey))
            .returning(move |_| Ok(AccountData::Exchange(exchange.clone())));

        client
            .expect_execute_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(
                predicate::eq(DoubleZeroInstruction::SetDeviceExchange(
                    ExchangeSetDeviceArgs {
                        index: 1,
                        set: SetDeviceOpption::Remove,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(exchange_pubkey, false),
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
                predicate::eq(DoubleZeroInstruction::SetDeviceExchange(
                    ExchangeSetDeviceArgs {
                        index: 2,
                        set: SetDeviceOpption::Remove,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(exchange_pubkey, false),
                    AccountMeta::new(device2_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = SetDeviceExchangeCommand {
            pubkey: exchange_pubkey,
            device1_pubkey: None,
            device2_pubkey: None,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_exchange_setdevice4_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (exchange_pubkey, _) = get_exchange_pda(&client.get_program_id(), 1);

        let device1_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");
        let device2_pubkey = Pubkey::new_unique();

        let exchange = Exchange {
            account_type: AccountType::Exchange,
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 42,
            reference_count: 0,
            lat: 50.0,
            lng: 20.0,
            loc_id: 1234,
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            status: ExchangeStatus::Activated,
            code: "TestExchange".to_string(),
            name: "TestName".to_string(),
        };

        let mut seq = mockall::Sequence::new();
        client
            .expect_get()
            .times(1)
            .in_sequence(&mut seq)
            .with(predicate::eq(exchange_pubkey))
            .returning(move |_| Ok(AccountData::Exchange(exchange.clone())));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::SetDeviceExchange(
                    ExchangeSetDeviceArgs {
                        index: 1,
                        set: SetDeviceOpption::Set,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(exchange_pubkey, false),
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
                predicate::eq(DoubleZeroInstruction::SetDeviceExchange(
                    ExchangeSetDeviceArgs {
                        index: 2,
                        set: SetDeviceOpption::Set,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(exchange_pubkey, false),
                    AccountMeta::new(device2_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = SetDeviceExchangeCommand {
            pubkey: exchange_pubkey,
            device1_pubkey: Some(device1_pubkey),
            device2_pubkey: Some(device2_pubkey),
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
