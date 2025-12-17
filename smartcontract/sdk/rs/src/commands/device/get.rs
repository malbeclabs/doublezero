use crate::{utils::parse_pubkey, DoubleZeroClient};
use doublezero_serviceability::state::{
    accountdata::AccountData, accounttype::AccountType, device::Device,
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct GetDeviceCommand {
    pub pubkey_or_code: String,
}

impl GetDeviceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Pubkey, Device)> {
        match parse_pubkey(&self.pubkey_or_code) {
            Some(pk) => match client.get(pk)? {
                AccountData::Device(device) => Ok((pk, device)),
                _ => Err(eyre::eyre!("Invalid Account Type")),
            },
            None => client
                .gets(AccountType::Device)?
                .into_iter()
                .find(|(_, v)| match v {
                    AccountData::Device(device) => {
                        device.code.eq_ignore_ascii_case(&self.pubkey_or_code)
                    }
                    _ => false,
                })
                .map(|(pk, v)| match v {
                    AccountData::Device(device) => Ok((pk, device)),
                    _ => Err(eyre::eyre!("Invalid Account Type")),
                })
                .unwrap_or_else(|| {
                    Err(eyre::eyre!(
                        "Device with code {} not found",
                        self.pubkey_or_code
                    ))
                }),
        }
    }
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use crate::{commands::device::get::GetDeviceCommand, tests::utils::create_test_client};
    use doublezero_serviceability::state::{
        accountdata::AccountData, accounttype::AccountType, device::Device,
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_commands_device_get_command() {
        let mut client = create_test_client();

        let device_pubkey = Pubkey::new_unique();
        let device = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "device_code".to_string(),
            owner: Pubkey::new_unique(),
            ..Default::default()
        };

        let device2 = device.clone();
        client
            .expect_get()
            .with(predicate::eq(device_pubkey))
            .returning(move |_| Ok(AccountData::Device(device2.clone())));

        let device2 = device.clone();
        client
            .expect_gets()
            .with(predicate::eq(AccountType::Device))
            .returning(move |_| {
                Ok(HashMap::from([(
                    device_pubkey,
                    AccountData::Device(device2.clone()),
                )]))
            });

        // Search by pubkey
        let res = GetDeviceCommand {
            pubkey_or_code: device_pubkey.to_string(),
        }
        .execute(&client);

        assert!(res.is_ok());
        let res = res.unwrap();
        assert_eq!(res.1.code, "device_code".to_string());
        assert_eq!(res.1.owner, device.owner);

        // Search by code
        let res = GetDeviceCommand {
            pubkey_or_code: "device_code".to_string(),
        }
        .execute(&client);

        assert!(res.is_ok());
        let res = res.unwrap();
        assert_eq!(res.1.code, "device_code".to_string());
        assert_eq!(res.1.owner, device.owner);

        // Search by code UPPERCASE
        let res = GetDeviceCommand {
            pubkey_or_code: "DEVICE_CODE".to_string(),
        }
        .execute(&client);

        assert!(res.is_ok());
        let res = res.unwrap();
        assert_eq!(res.1.code, "device_code".to_string());
        assert_eq!(res.1.owner, device.owner);

        // Invalid search
        let res = GetDeviceCommand {
            pubkey_or_code: "ssssssssssss".to_string(),
        }
        .execute(&client);

        assert!(res.is_err());

        // Search by invalid code
        let res = GetDeviceCommand {
            pubkey_or_code: "s(%".to_string(),
        }
        .execute(&client);

        assert!(res.is_err());
    }
}
