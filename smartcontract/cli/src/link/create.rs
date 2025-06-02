use crate::doublezerocommand::CliCommand;
use crate::helpers::parse_pubkey;
use crate::requirements::{CHECK_BALANCE, CHECK_ID_JSON};
use clap::Args;
use doublezero_sdk::commands::device::get::GetDeviceCommand;
use doublezero_sdk::commands::link::create::CreateLinkCommand;
use doublezero_sdk::*;
use std::io::Write;

#[derive(Args, Debug)]
pub struct CreateLinkCliCommand {
    #[arg(long)]
    pub code: String,
    #[arg(long)]
    pub side_a: String,
    #[arg(long)]
    pub side_z: String,
    #[arg(long)]
    pub link_type: Option<String>,
    #[arg(long)]
    pub bandwidth: String,
    #[arg(long)]
    pub mtu: u32,
    #[arg(long)]
    pub delay_ms: f64,
    #[arg(long)]
    pub jitter_ms: f64,
}

impl CreateLinkCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let side_a_pk = match parse_pubkey(&self.side_a) {
            Some(pk) => pk,
            None => {
                let (pubkey, _) = client
                    .get_device(GetDeviceCommand {
                        pubkey_or_code: self.side_a.clone(),
                    })
                    .map_err(|_| eyre::eyre!("Device not found"))?;
                pubkey
            }
        };

        let side_z_pk = match parse_pubkey(&self.side_z) {
            Some(pk) => pk,
            None => {
                let (pubkey, _) = client
                    .get_device(GetDeviceCommand {
                        pubkey_or_code: self.side_z.clone(),
                    })
                    .map_err(|_| eyre::eyre!("Device not found"))?;
                pubkey
            }
        };

        let (signature, _pubkey) = client.create_tunnel(CreateLinkCommand {
            code: self.code.clone(),
            side_a_pk,
            side_z_pk,
            link_type: self
                .link_type
                .as_ref()
                .map(|t| t.parse().unwrap())
                .unwrap_or(LinkLinkType::L3),
            bandwidth: bandwidth_parse(&self.bandwidth),
            mtu: self.mtu,
            delay_ns: (self.delay_ms * 1000000.0) as u64,
            jitter_ns: (self.jitter_ms * 1000000.0) as u64,
        })?;

        writeln!(out, "Signature: {}", signature)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::doublezerocommand::CliCommand;
    use crate::link::create::CreateLinkCliCommand;
    use crate::requirements::{CHECK_BALANCE, CHECK_ID_JSON};
    use crate::tests::tests::create_test_client;
    use doublezero_sdk::commands::device::get::GetDeviceCommand;
    use doublezero_sdk::commands::link::create::CreateLinkCommand;
    use doublezero_sdk::get_device_pda;
    use doublezero_sdk::AccountType;
    use doublezero_sdk::Device;
    use doublezero_sdk::DeviceStatus;
    use doublezero_sdk::DeviceType;
    use doublezero_sdk::LinkLinkType;
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_cli_device_create() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_device_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let location1_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let exchange1_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkca");
        let device1_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcb");
        let device1 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            code: "test".to_string(),
            location_pk: location1_pk,
            exchange_pk: exchange1_pk,
            device_type: DeviceType::Switch,
            public_ip: [10, 0, 0, 1],
            dz_prefixes: vec![([10, 1, 0, 0], 16)],
            status: DeviceStatus::Activated,
            owner: pda_pubkey,
        };
        let location2_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let exchange2_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkce");
        let device2_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcf");
        let device2 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            code: "test".to_string(),
            location_pk: location2_pk,
            exchange_pk: exchange2_pk,
            device_type: DeviceType::Switch,
            public_ip: [10, 0, 0, 1],
            dz_prefixes: vec![([10, 1, 0, 0], 16)],
            status: DeviceStatus::Activated,
            owner: pda_pubkey,
        };

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_device()
            .with(predicate::eq(GetDeviceCommand {
                pubkey_or_code: device1_pk.to_string(),
            }))
            .returning(move |_| Ok((device1_pk, device1.clone())));
        client
            .expect_get_device()
            .with(predicate::eq(GetDeviceCommand {
                pubkey_or_code: device2_pk.to_string(),
            }))
            .returning(move |_| Ok((device2_pk, device2.clone())));
        client
            .expect_create_tunnel()
            .with(predicate::eq(CreateLinkCommand {
                code: "test".to_string(),
                side_a_pk: device1_pk,
                side_z_pk: device2_pk,
                link_type: LinkLinkType::L3,
                bandwidth: 1000000000,
                mtu: 1500,
                delay_ns: 10000000000,
                jitter_ns: 5000000000,
            }))
            .times(1)
            .returning(move |_| Ok((signature, pda_pubkey)));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let res = CreateLinkCliCommand {
            code: "test".to_string(),
            side_a: device1_pk.to_string(),
            side_z: device2_pk.to_string(),
            link_type: Some("L3".to_string()),
            bandwidth: "1Gbps".to_string(),
            mtu: 1500,
            delay_ms: 10000.0,
            jitter_ms: 5000.0,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }
}
