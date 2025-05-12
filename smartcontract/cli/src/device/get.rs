use clap::Args;
use doublezero_sdk::commands::device::get::GetDeviceCommand;
use doublezero_sdk::*;
use std::io::Write;
use crate::doublezerocommand::CliCommand;

#[derive(Args, Debug)]
pub struct GetDeviceCliCommand {
    #[arg(long)]
    pub code: String,
}

impl GetDeviceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (pubkey, device) = client.get_device(GetDeviceCommand {
            pubkey_or_code: self.code,
        })?;

        writeln!(out, 
            "account: {}\r\ncode: {}\r\nlocation: {}\r\nexchange: {}\r\ndevice_type: {}\r\npublic_ip: {}\r\ndz_prefixes: {}\r\nstatus: {}\r\nowner: {}",
            pubkey,
            device.code,
            device.location_pk,
            device.exchange_pk,
            device.device_type,
            ipv4_to_string(&device.public_ip),
            networkv4_list_to_string(&device.dz_prefixes),
            device.status,
            device.owner
            )?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use std::str::FromStr;
    use doublezero_sdk::commands::device::get::GetDeviceCommand;
    use doublezero_sdk::{AccountType, Device, DeviceStatus, DeviceType};
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;
    use crate::device::get::GetDeviceCliCommand;
    use crate::tests::tests::create_test_client;

    #[test]
    fn test_cli_device_get() {
        let mut client = create_test_client();

        let location_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let exchange_pk = Pubkey::from_str_const("GQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc");
        let device1_pubkey = Pubkey::from_str("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB").unwrap();
        let device1 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            code: "test".to_string(),
            location_pk: location_pk,
            exchange_pk: exchange_pk,
            device_type: DeviceType::Switch,
            public_ip: [1, 2, 3, 4],
            dz_prefixes: vec![([1, 2, 3, 4], 32)],
            status: DeviceStatus::Activated,
            owner: device1_pubkey,
        };

        client
            .expect_get_device()
            .with(predicate::eq(GetDeviceCommand {
                pubkey_or_code: device1_pubkey.to_string(),
            }))
            .returning(move |_| Ok((device1_pubkey, device1.clone())));
        client
            .expect_get_device()
            .returning(move |_| Err(eyre::eyre!("not found")));
        /*****************************************************************************************************/
        // Expected failure
        let mut output = Vec::new();
        let res = GetDeviceCliCommand {
            code: Pubkey::new_unique().to_string(),
        }
        .execute(&client, &mut output);
        assert!(!res.is_ok(), "I shouldn't find anything.");

        // Expected success
        let mut output = Vec::new();
        let res = GetDeviceCliCommand {
            code: device1_pubkey.to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by pubkey");
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "account: BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB\r\ncode: test\r\nlocation: HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx\r\nexchange: GQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc\r\ndevice_type: switch\r\npublic_ip: 1.2.3.4\r\ndz_prefixes: 1.2.3.4/32\r\nstatus: activated\r\nowner: BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB\n");

    }
}
