use borsh::{BorshDeserialize, BorshSerialize};

pub use crate::processors::{
    geo_probe::{
        create::CreateGeoProbeArgs, remove_parent_device::RemoveParentDeviceArgs,
        update::UpdateGeoProbeArgs,
    },
    geolocation_user::{
        add_target::AddTargetArgs, create::CreateGeolocationUserArgs,
        remove_target::RemoveTargetArgs, set_result_destination::SetResultDestinationArgs,
        update::UpdateGeolocationUserArgs, update_payment_status::UpdatePaymentStatusArgs,
    },
    program_config::{init::InitProgramConfigArgs, update::UpdateProgramConfigArgs},
};

#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone)]
pub enum GeolocationInstruction {
    InitProgramConfig(InitProgramConfigArgs),
    UpdateProgramConfig(UpdateProgramConfigArgs),
    CreateGeoProbe(CreateGeoProbeArgs),
    UpdateGeoProbe(UpdateGeoProbeArgs),
    DeleteGeoProbe,
    AddParentDevice,
    RemoveParentDevice(RemoveParentDeviceArgs),
    CreateGeolocationUser(CreateGeolocationUserArgs),
    UpdateGeolocationUser(UpdateGeolocationUserArgs),
    DeleteGeolocationUser,
    AddTarget(AddTargetArgs),
    RemoveTarget(RemoveTargetArgs),
    UpdatePaymentStatus(UpdatePaymentStatusArgs),
    SetResultDestination(SetResultDestinationArgs),
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::state::geolocation_user::GeolocationPaymentStatus;
    use solana_program::pubkey::Pubkey;
    use std::net::Ipv4Addr;

    fn test_instruction(instruction: GeolocationInstruction) {
        let data = borsh::to_vec(&instruction).unwrap();
        let decoded: GeolocationInstruction = borsh::from_slice(&data).unwrap();
        assert_eq!(instruction, decoded, "Instruction mismatch");
    }

    #[test]
    fn test_roundtrip_all_instructions() {
        test_instruction(GeolocationInstruction::InitProgramConfig(
            InitProgramConfigArgs {},
        ));
        test_instruction(GeolocationInstruction::UpdateProgramConfig(
            UpdateProgramConfigArgs {
                version: Some(2),
                min_compatible_version: Some(1),
            },
        ));
        test_instruction(GeolocationInstruction::UpdateProgramConfig(
            UpdateProgramConfigArgs {
                version: None,
                min_compatible_version: None,
            },
        ));
        test_instruction(GeolocationInstruction::CreateGeoProbe(CreateGeoProbeArgs {
            code: "test-probe".to_string(),
            public_ip: Ipv4Addr::new(8, 8, 8, 8),
            location_offset_port: 8923,
            metrics_publisher_pk: Pubkey::new_unique(),
        }));
        test_instruction(GeolocationInstruction::UpdateGeoProbe(UpdateGeoProbeArgs {
            public_ip: Some(Ipv4Addr::new(1, 1, 1, 1)),
            location_offset_port: Some(9999),
            metrics_publisher_pk: None,
        }));
        test_instruction(GeolocationInstruction::DeleteGeoProbe);
        test_instruction(GeolocationInstruction::AddParentDevice);
        test_instruction(GeolocationInstruction::RemoveParentDevice(
            RemoveParentDeviceArgs {
                device_pk: Pubkey::new_unique(),
            },
        ));
        test_instruction(GeolocationInstruction::CreateGeolocationUser(
            CreateGeolocationUserArgs {
                code: "geo-user-01".to_string(),
                token_account: Pubkey::new_unique(),
            },
        ));
        test_instruction(GeolocationInstruction::UpdateGeolocationUser(
            UpdateGeolocationUserArgs {
                token_account: Some(Pubkey::new_unique()),
            },
        ));
        test_instruction(GeolocationInstruction::UpdateGeolocationUser(
            UpdateGeolocationUserArgs {
                token_account: None,
            },
        ));
        test_instruction(GeolocationInstruction::DeleteGeolocationUser);
        test_instruction(GeolocationInstruction::AddTarget(AddTargetArgs {
            target_type: crate::state::geolocation_user::GeoLocationTargetType::Outbound,
            ip_address: Ipv4Addr::new(8, 8, 8, 8),
            location_offset_port: 8923,
            target_pk: Pubkey::default(),
        }));
        test_instruction(GeolocationInstruction::AddTarget(AddTargetArgs {
            target_type: crate::state::geolocation_user::GeoLocationTargetType::Inbound,
            ip_address: Ipv4Addr::UNSPECIFIED,
            location_offset_port: 0,
            target_pk: Pubkey::new_unique(),
        }));
        test_instruction(GeolocationInstruction::RemoveTarget(RemoveTargetArgs {
            target_type: crate::state::geolocation_user::GeoLocationTargetType::Outbound,
            ip_address: Ipv4Addr::new(8, 8, 8, 8),
            target_pk: Pubkey::default(),
        }));
        test_instruction(GeolocationInstruction::UpdatePaymentStatus(
            UpdatePaymentStatusArgs {
                payment_status: GeolocationPaymentStatus::Paid,
                last_deduction_dz_epoch: Some(42),
            },
        ));
        test_instruction(GeolocationInstruction::UpdatePaymentStatus(
            UpdatePaymentStatusArgs {
                payment_status: GeolocationPaymentStatus::Delinquent,
                last_deduction_dz_epoch: None,
            },
        ));
        test_instruction(GeolocationInstruction::SetResultDestination(
            SetResultDestinationArgs {
                destination: "185.199.108.1:9000".to_string(),
            },
        ));
        test_instruction(GeolocationInstruction::SetResultDestination(
            SetResultDestinationArgs {
                destination: String::new(),
            },
        ));
    }

    #[test]
    fn test_deserialize_invalid() {
        assert!(borsh::from_slice::<GeolocationInstruction>(&[]).is_err());
        assert!(borsh::from_slice::<GeolocationInstruction>(&[255]).is_err());
    }
}
