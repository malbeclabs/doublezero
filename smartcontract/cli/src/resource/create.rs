use super::ResourceType;
use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_sdk::{
    commands::{device::get::GetDeviceCommand, resource::create::CreateResourceCommand},
    ResourceType as SdkResourceType,
};
use std::io::Write;

#[derive(Args, Debug)]
pub struct CreateResourceCliCommand {
    // Type of resource extension to create
    #[arg(long)]
    pub resource_type: ResourceType,
    // Associated public key (only for DzPrefixBlock)
    #[arg(long)]
    pub associated_pubkey: Option<String>,
    // Index (only for DzPrefixBlock)
    #[arg(long)]
    pub index: Option<usize>,
}

impl From<CreateResourceCliCommand> for CreateResourceCommand {
    fn from(cmd: CreateResourceCliCommand) -> Self {
        let resource_type = super::resource_type_from(
            cmd.resource_type,
            cmd.associated_pubkey.as_ref().and_then(|s| s.parse().ok()),
            cmd.index,
        );

        CreateResourceCommand { resource_type }
    }
}

impl CreateResourceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let args: CreateResourceCommand = self.into();

        match args.resource_type {
            SdkResourceType::DzPrefixBlock(pk, index) | SdkResourceType::TunnelIds(pk, index) => {
                let get_device_cmd = GetDeviceCommand {
                    pubkey_or_code: pk.to_string(),
                };
                let (_device_pk, device) = client.get_device(get_device_cmd)?;
                if device.dz_prefixes.len() <= index {
                    return Err(eyre::eyre!(
                        "Device does not have a DzPrefixBlock at index {}",
                        index
                    ));
                }
            }
            _ => {}
        }

        let signature = client.create_resource(args)?;
        writeln!(out, "Signature: {signature}",)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::doublezerocommand::MockCliCommand;
    use doublezero_sdk::Device;
    use mockall::predicate::eq;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::io::Cursor;

    #[test]
    fn test_execute_success_dzprefixblock() {
        let mut mock = MockCliCommand::new();
        let device_pk = Pubkey::new_unique();
        let device_pk_clone = device_pk;
        let device = Device {
            dz_prefixes: "1.2.3.0/27".parse().unwrap(),
            ..Device::default()
        };
        let device_clone = device.clone();
        mock.expect_check_requirements().returning(|_| Ok(()));
        mock.expect_get_device()
            .returning(move |_| Ok((device_pk_clone, device_clone.clone())));
        let device_pk = Pubkey::new_unique();
        let args = CreateResourceCommand {
            resource_type: SdkResourceType::DzPrefixBlock(device_pk, 0),
        };

        let sig = Signature::new_unique();
        mock.expect_create_resource()
            .with(eq(args))
            .returning(move |_| Ok(sig));

        let cmd = CreateResourceCliCommand {
            resource_type: ResourceType::DzPrefixBlock,
            associated_pubkey: Some(device_pk.to_string()),
            index: Some(0),
        };
        let mut out = Cursor::new(Vec::new());
        let result = cmd.execute(&mock, &mut out);
        assert!(result.is_ok());
        let output = String::from_utf8(out.into_inner()).unwrap();
        assert!(output.contains("Signature:"));
    }

    #[test]
    fn test_execute_failure_dzprefixblock_index() {
        let mut mock = MockCliCommand::new();
        let device_pk = Pubkey::new_unique();
        let device_pk_clone = device_pk;
        let device = Device {
            dz_prefixes: "1.2.3.0/27".parse().unwrap(),
            ..Device::default()
        };
        let device_clone = device.clone();
        mock.expect_check_requirements().returning(|_| Ok(()));
        mock.expect_get_device()
            .returning(move |_| Ok((device_pk_clone, device_clone.clone())));
        let device_pk = Pubkey::new_unique();

        let cmd = CreateResourceCliCommand {
            resource_type: ResourceType::DzPrefixBlock,
            associated_pubkey: Some(device_pk.to_string()),
            index: Some(1),
        };
        let mut out = Cursor::new(Vec::new());
        let result = cmd.execute(&mock, &mut out);
        assert!(result.is_err());
        assert_eq!(
            result.unwrap_err().to_string(),
            "Device does not have a DzPrefixBlock at index 1".to_string()
        );
    }
}
