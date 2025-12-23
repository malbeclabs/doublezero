use super::ResourceExtensionType;
use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_program_common::types::NetworkV4;
use doublezero_sdk::{
    commands::{device::get::GetDeviceCommand, resource::deallocate::DeallocateResourceCommand},
    IdOrIp, ResourceBlockType,
};
use std::io::Write;

#[derive(Args, Debug)]
pub struct DeallocateResourceCliCommand {
    // Type of resource extension to allocate
    #[arg(long)]
    pub resource_extension_type: ResourceExtensionType,
    // Associated public key (only for DzPrefixBlock)
    #[arg(long)]
    pub associated_pubkey: Option<String>,
    // Index (only for DzPrefixBlock)
    #[arg(long)]
    pub index: Option<usize>,
    // Requested value to deallocate
    #[arg(long)]
    pub value: String,
}

impl From<DeallocateResourceCliCommand> for DeallocateResourceCommand {
    fn from(cmd: DeallocateResourceCliCommand) -> Self {
        let resource_block_type = super::resource_extension_to_resource_block(
            cmd.resource_extension_type,
            cmd.associated_pubkey.as_ref().and_then(|s| s.parse().ok()),
            cmd.index,
        );

        let value = match cmd.resource_extension_type {
            ResourceExtensionType::DeviceTunnelBlock
            | ResourceExtensionType::UserTunnelBlock
            | ResourceExtensionType::MulticastGroupBlock
            | ResourceExtensionType::DzPrefixBlock => IdOrIp::Ip(
                cmd.value
                    .parse::<NetworkV4>()
                    .expect("Failed to parse IP address")
                    .into(),
            ),
            ResourceExtensionType::TunnelIds
            | ResourceExtensionType::LinkIds
            | ResourceExtensionType::SegmentRoutingIds => {
                IdOrIp::Id(cmd.value.parse::<u16>().expect("Failed to parse ID").into())
            }
        };

        DeallocateResourceCommand {
            resource_block_type,
            value,
        }
    }
}

impl DeallocateResourceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let args: DeallocateResourceCommand = self.into();

        match args.resource_block_type {
            ResourceBlockType::DzPrefixBlock(pk, index)
            | ResourceBlockType::TunnelIds(pk, index) => {
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

        let signature = client.deallocate_resource(args)?;
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
        let device_pk_clone = device_pk.clone();
        let device = Device {
            dz_prefixes: "1.2.3.0/27".parse().unwrap(),
            ..Device::default()
        };
        let device_clone = device.clone();
        mock.expect_check_requirements().returning(|_| Ok(()));
        mock.expect_get_device()
            .returning(move |_| Ok((device_pk_clone, device_clone.clone())));
        let device_pk = Pubkey::new_unique();
        let args = DeallocateResourceCommand {
            resource_block_type: ResourceBlockType::DzPrefixBlock(device_pk, 0),
            value: IdOrIp::Ip("1.2.3.2/32".parse().unwrap()),
        };

        let sig = Signature::new_unique();
        mock.expect_deallocate_resource()
            .with(eq(args))
            .returning(move |_| Ok(sig));

        let cmd = DeallocateResourceCliCommand {
            resource_extension_type: ResourceExtensionType::DzPrefixBlock,
            associated_pubkey: Some(device_pk.to_string()),
            index: Some(0),
            value: "1.2.3.2/32".to_string(),
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
        let device_pk_clone = device_pk.clone();
        let device = Device {
            dz_prefixes: "1.2.3.0/27".parse().unwrap(),
            ..Device::default()
        };
        let device_clone = device.clone();
        mock.expect_check_requirements().returning(|_| Ok(()));
        mock.expect_get_device()
            .returning(move |_| Ok((device_pk_clone, device_clone.clone())));
        let device_pk = Pubkey::new_unique();

        let cmd = DeallocateResourceCliCommand {
            resource_extension_type: ResourceExtensionType::DzPrefixBlock,
            associated_pubkey: Some(device_pk.to_string()),
            index: Some(1),
            value: "1.2.3.2/32".to_string(),
        };
        let mut out = Cursor::new(Vec::new());
        let result = cmd.execute(&mock, &mut out);
        assert!(result.is_err());
        assert_eq!(
            result.unwrap_err().to_string(),
            "Device does not have a DzPrefixBlock at index 1".to_string()
        );
    }

    fn make_cmd(
        ext_type: ResourceExtensionType,
        value: &str,
        pubkey: Option<&str>,
        index: Option<usize>,
    ) -> DeallocateResourceCliCommand {
        DeallocateResourceCliCommand {
            resource_extension_type: ext_type,
            associated_pubkey: pubkey.map(|s| s.to_string()),
            index,
            value: value.to_string(),
        }
    }

    #[test]
    fn test_device_tunnel_block_ip() {
        let cmd = make_cmd(
            ResourceExtensionType::DeviceTunnelBlock,
            "10.1.2.3",
            None,
            None,
        );
        let dealloc: DeallocateResourceCommand = cmd.into();
        match dealloc.value {
            IdOrIp::Ip(ip) => assert_eq!(ip.to_string(), "10.1.2.3/32"),
            _ => panic!("Expected Ip variant"),
        }
    }

    #[test]
    fn test_tunnel_ids_id() {
        let cmd = make_cmd(ResourceExtensionType::TunnelIds, "123", None, None);
        let dealloc: DeallocateResourceCommand = cmd.into();
        match dealloc.value {
            IdOrIp::Id(id) => assert_eq!(u16::from(id), 123),
            _ => panic!("Expected Id variant"),
        }
    }

    #[test]
    #[should_panic(expected = "Failed to parse IP address")]
    fn test_invalid_ip_panics() {
        let cmd = make_cmd(
            ResourceExtensionType::DeviceTunnelBlock,
            "not_an_ip",
            None,
            None,
        );
        let _: DeallocateResourceCommand = cmd.into();
    }

    #[test]
    #[should_panic(expected = "Failed to parse ID")]
    fn test_invalid_id_panics() {
        let cmd = make_cmd(ResourceExtensionType::TunnelIds, "not_an_id", None, None);
        let _: DeallocateResourceCommand = cmd.into();
    }

    #[test]
    fn test_dz_prefix_block_with_pubkey_and_index() {
        let pubkey_str = Pubkey::new_unique().to_string();
        let cmd = make_cmd(
            ResourceExtensionType::DzPrefixBlock,
            "192.168.1.1",
            Some(&pubkey_str),
            Some(5),
        );
        let dealloc: DeallocateResourceCommand = cmd.into();
        match dealloc.value {
            IdOrIp::Ip(ip) => assert_eq!(ip.to_string(), "192.168.1.1/32"),
            _ => panic!("Expected Ip variant"),
        }
    }
}
