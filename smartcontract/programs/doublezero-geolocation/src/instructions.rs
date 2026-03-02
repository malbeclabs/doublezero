use borsh::{BorshDeserialize, BorshSerialize};

pub use crate::processors::{
    geo_probe::{create::CreateGeoProbeArgs, update::UpdateGeoProbeArgs},
    program_config::{init::InitProgramConfigArgs, update::UpdateProgramConfigArgs},
};

#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone)]
pub enum GeolocationInstruction {
    InitProgramConfig(InitProgramConfigArgs),
    UpdateProgramConfig(UpdateProgramConfigArgs),
    CreateGeoProbe(CreateGeoProbeArgs),
    UpdateGeoProbe(UpdateGeoProbeArgs),
    DeleteGeoProbe,
}

#[cfg(test)]
mod tests {
    use super::*;
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
    }

    #[test]
    fn test_deserialize_invalid() {
        assert!(borsh::from_slice::<GeolocationInstruction>(&[]).is_err());
        assert!(borsh::from_slice::<GeolocationInstruction>(&[255]).is_err());
    }
}
