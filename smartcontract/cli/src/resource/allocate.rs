use super::ResourceType;
use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_program_common::types::NetworkV4;
use doublezero_sdk::{commands::resource::allocate::AllocateResourceCommand, IdOrIp};
use std::io::Write;

#[derive(Args, Debug)]
pub struct AllocateResourceCliCommand {
    // Type of resource extension to allocate
    #[arg(long)]
    pub resource_type: ResourceType,
    // Associated public key (only for DzPrefixBlock)
    #[arg(long)]
    pub associated_pubkey: Option<String>,
    // Index (only for DzPrefixBlock)
    #[arg(long)]
    pub index: Option<usize>,
    // Requested allocation (optional)
    #[arg(long)]
    pub requested_allocation: Option<String>,
}

impl From<AllocateResourceCliCommand> for AllocateResourceCommand {
    fn from(cmd: AllocateResourceCliCommand) -> Self {
        let resource_type = super::resource_type_from(
            cmd.resource_type,
            cmd.associated_pubkey.as_ref().and_then(|s| s.parse().ok()),
            cmd.index,
        );

        let requested = cmd.requested_allocation.map(|x| match cmd.resource_type {
            ResourceType::DeviceTunnelBlock
            | ResourceType::UserTunnelBlock
            | ResourceType::MulticastGroupBlock
            | ResourceType::MulticastPublisherBlock
            | ResourceType::DzPrefixBlock => {
                IdOrIp::Ip(x.parse::<NetworkV4>().expect("Failed to parse IP address"))
            }
            ResourceType::TunnelIds
            | ResourceType::LinkIds
            | ResourceType::SegmentRoutingIds
            | ResourceType::VrfIds => IdOrIp::Id(x.parse::<u16>().expect("Failed to parse ID")),
        });

        AllocateResourceCommand {
            resource_type,
            requested,
        }
    }
}

impl AllocateResourceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let args: AllocateResourceCommand = self.into();

        super::check_device_if_needed(&args.resource_type, client)?;

        let signature = client.allocate_resource(args)?;
        writeln!(out, "Signature: {signature}",)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::doublezerocommand::MockCliCommand;
    use doublezero_sdk::{Device, ResourceType as SdkResourceType};
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
        let args = AllocateResourceCommand {
            resource_type: SdkResourceType::DzPrefixBlock(device_pk, 0),
            requested: None,
        };

        let sig = Signature::new_unique();
        mock.expect_allocate_resource()
            .with(eq(args))
            .returning(move |_| Ok(sig));

        let cmd = AllocateResourceCliCommand {
            resource_type: ResourceType::DzPrefixBlock,
            associated_pubkey: Some(device_pk.to_string()),
            index: Some(0),
            requested_allocation: None,
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

        let cmd = AllocateResourceCliCommand {
            resource_type: ResourceType::DzPrefixBlock,
            associated_pubkey: Some(device_pk.to_string()),
            index: Some(1),
            requested_allocation: None,
        };
        let mut out = Cursor::new(Vec::new());
        let result = cmd.execute(&mock, &mut out);
        assert!(result.is_err());
        assert_eq!(
            result.unwrap_err().to_string(),
            "Device does not have a DzPrefixBlock at index 1".to_string()
        );
    }

    #[test]
    fn test_from_device_tunnel_block_with_ip() {
        let cmd = AllocateResourceCliCommand {
            resource_type: ResourceType::DeviceTunnelBlock,
            associated_pubkey: None,
            index: None,
            requested_allocation: Some("10.0.0.1".to_string()),
        };
        let alloc_cmd: AllocateResourceCommand = cmd.into();
        match alloc_cmd.requested {
            Some(IdOrIp::Ip(ip)) => assert_eq!(ip.to_string(), "10.0.0.1/32"),
            _ => panic!("Expected Ip variant"),
        }
    }

    #[test]
    fn test_from_tunnel_ids_with_id() {
        let cmd = AllocateResourceCliCommand {
            resource_type: ResourceType::TunnelIds,
            associated_pubkey: None,
            index: None,
            requested_allocation: Some("42".to_string()),
        };
        let alloc_cmd: AllocateResourceCommand = cmd.into();
        match alloc_cmd.requested {
            Some(IdOrIp::Id(id)) => assert_eq!(id, 42),
            _ => panic!("Expected Id variant"),
        }
    }

    #[test]
    #[should_panic(expected = "Failed to parse IP address")]
    fn test_invalid_ip_panics() {
        let cmd = AllocateResourceCliCommand {
            resource_type: ResourceType::DeviceTunnelBlock,
            associated_pubkey: None,
            index: None,
            requested_allocation: Some("not_an_ip".to_string()),
        };
        let _: AllocateResourceCommand = cmd.into();
    }

    #[test]
    #[should_panic(expected = "Failed to parse ID")]
    fn test_invalid_id_panics() {
        let cmd = AllocateResourceCliCommand {
            resource_type: ResourceType::TunnelIds,
            associated_pubkey: None,
            index: None,
            requested_allocation: Some("not_an_id".to_string()),
        };
        let _: AllocateResourceCommand = cmd.into();
    }
}
