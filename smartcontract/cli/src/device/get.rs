use crate::{doublezerocommand::CliCommand, formatters::DisplayVec, validators::validate_code};
use clap::Args;
use doublezero_sdk::commands::device::get::GetDeviceCommand;
use std::io::Write;

#[derive(Args, Debug)]
pub struct GetDeviceCliCommand {
    /// Device Pubkey or code to retrieve
    #[arg(long, value_parser = validate_code)]
    pub code: String,
}

impl GetDeviceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (pubkey, device) = client.get_device(GetDeviceCommand {
            pubkey_or_code: self.code,
        })?;

        writeln!(
            out,
            "account: {}\r\n\
code: {}\r\n\
contributor: {}\r\n\
location: {}\r\n\
exchange: {}\r\n\
device_type: {}\r\n\
public_ip: {}\r\n\
dz_prefixes: {}\r\n\
metrics_publisher: {}\r\n\
bgp_asn: {}\r\n\
dia_bgp_asn: {}\r\n\
mgmt_vrf: {}\r\n\
dns_servers: {}\r\n\
ntp_servers: {}\r\n\
status: {}\r\n\
owner: {}",
            pubkey,
            device.code,
            device.contributor_pk,
            device.location_pk,
            device.exchange_pk,
            device.device_type,
            &device.public_ip,
            &device.dz_prefixes,
            device.metrics_publisher_pk,
            device.bgp_asn,
            device.dia_bgp_asn,
            device.mgmt_vrf,
            DisplayVec(&device.dns_servers),
            DisplayVec(&device.ntp_servers),
            device.status,
            device.owner
        )?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{device::get::GetDeviceCliCommand, tests::utils::create_test_client};
    use doublezero_sdk::{
        commands::device::get::GetDeviceCommand, AccountType, Device, DeviceStatus, DeviceType,
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;
    use std::str::FromStr;

    #[test]
    fn test_cli_device_get() {
        let mut client = create_test_client();

        let contributor_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let location_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let exchange_pk = Pubkey::from_str_const("GQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc");
        let device1_pubkey =
            Pubkey::from_str("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB").unwrap();
        let device1 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "test".to_string(),
            contributor_pk,
            location_pk,
            exchange_pk,
            device_type: DeviceType::Switch,
            public_ip: [1, 2, 3, 4].into(),
            dz_prefixes: "1.2.3.4/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::from_str_const(
                "1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR",
            ),
            owner: device1_pubkey,
            bgp_asn: 0,
            dia_bgp_asn: 0,
            mgmt_vrf: "default".to_string(),
            dns_servers: vec![[8, 8, 8, 8].into(), [8, 8, 4, 4].into()],
            ntp_servers: vec![[192, 168, 1, 1].into(), [192, 168, 1, 2].into()],
            interfaces: vec![],
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
        assert!(res.is_err(), "I shouldn't find anything.");

        // Expected success
        let mut output = Vec::new();
        let res = GetDeviceCliCommand {
            code: device1_pubkey.to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by pubkey");
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "account: BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB\r\ncode: test\r\ncontributor: HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx\r\nlocation: HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx\r\nexchange: GQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc\r\ndevice_type: switch\r\npublic_ip: 1.2.3.4\r\ndz_prefixes: 1.2.3.4/32\r\nmetrics_publisher: 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR\r\nbgp_asn: 0\r\ndia_bgp_asn: 0\r\nmgmt_vrf: default\r\ndns_servers: 8.8.8.8,8.8.4.4\r\nntp_servers: 192.168.1.1,192.168.1.2\r\nstatus: activated\r\nowner: BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB\n");
    }
}
