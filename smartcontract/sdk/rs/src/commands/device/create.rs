use doublezero_program_common::{types::NetworkV4List, validate_account_code};
use doublezero_serviceability::{
    pda::get_device_pda,
    processors::device::create::DeviceCreateArgs,
    state::device::{DeviceDesiredStatus, DeviceType},
};
use doublezero_serviceability_instruction::device::create_device;
use solana_sdk::{pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct CreateDeviceCommand {
    pub code: String,
    pub contributor_pk: Pubkey,
    pub location_pk: Pubkey,
    pub exchange_pk: Pubkey,
    pub device_type: DeviceType,
    pub public_ip: Ipv4Addr,
    pub dz_prefixes: NetworkV4List,
    pub metrics_publisher: Pubkey,
    pub mgmt_vrf: String,
    pub desired_status: Option<DeviceDesiredStatus>,
}

impl CreateDeviceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let mut code =
            validate_account_code(&self.code).map_err(|err| eyre::eyre!("invalid code: {err}"))?;
        code.make_ascii_lowercase();

        let (_, globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let program_id = client.get_program_id();
        let account_index = globalstate.account_index + 1;
        let (pda_pubkey, _) = get_device_pda(&program_id, account_index);

        // The builder derives every PDA (globalconfig, tunnel_ids, dz_prefix blocks)
        // and writes `resource_count` from its own loop, so the command only supplies
        // the resolved account_index and the resource-count-agnostic args.
        let ix = create_device(
            &program_id,
            &client.get_payer(),
            &self.contributor_pk,
            &self.location_pk,
            &self.exchange_pk,
            account_index,
            DeviceCreateArgs {
                code,
                device_type: self.device_type,
                public_ip: self.public_ip,
                dz_prefixes: self.dz_prefixes.clone(),
                metrics_publisher_pk: self.metrics_publisher,
                mgmt_vrf: self.mgmt_vrf.clone(),
                desired_status: self.desired_status,
                resource_count: 0,
            },
        );

        client.send_transaction(ix).map(|sig| (sig, pda_pubkey))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::device::create::CreateDeviceCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        processors::device::create::DeviceCreateArgs, state::device::DeviceType,
    };
    use doublezero_serviceability_instruction::device::create_device;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_device_create_command() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let payer = client.get_payer();

        let contributor_pubkey = Pubkey::default();
        let location_pubkey = Pubkey::new_unique();
        let exchange_pubkey = Pubkey::new_unique();
        let metrics_publisher = Pubkey::default();

        // create_test_client seeds globalstate.account_index = 0, so the new device
        // index is 1. The command must hand send_transaction exactly the builder's
        // instruction (code lowercased, resource_count written by the builder = 2).
        let expected = create_device(
            &program_id,
            &payer,
            &contributor_pubkey,
            &location_pubkey,
            &exchange_pubkey,
            1,
            DeviceCreateArgs {
                code: "test_device".to_string(),
                device_type: DeviceType::Hybrid,
                public_ip: [10, 0, 0, 1].into(),
                dz_prefixes: "10.0.0.0/8".parse().unwrap(),
                metrics_publisher_pk: metrics_publisher,
                mgmt_vrf: "mgmt".to_string(),
                desired_status: None,
                resource_count: 0,
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        // Mixed-case input verifies the SDK lowercases device codes.
        let command = CreateDeviceCommand {
            code: "Test_Device".to_string(),
            contributor_pk: contributor_pubkey,
            location_pk: location_pubkey,
            exchange_pk: exchange_pubkey,
            device_type: DeviceType::Hybrid,
            public_ip: [10, 0, 0, 1].into(),
            dz_prefixes: "10.0.0.0/8".parse().unwrap(),
            metrics_publisher,
            mgmt_vrf: "mgmt".to_string(),
            desired_status: None,
        };

        let invalid_command = CreateDeviceCommand {
            code: "test/device".to_string(),
            ..command.clone()
        };
        assert!(invalid_command.execute(&client).is_err());
        assert!(command.execute(&client).is_ok());
    }
}
