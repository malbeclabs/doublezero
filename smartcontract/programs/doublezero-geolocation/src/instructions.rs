use borsh::BorshSerialize;
use solana_program::program_error::ProgramError;

pub use crate::processors::{
    geo_probe::{
        add_parent_device::AddParentDeviceArgs, create::CreateGeoProbeArgs,
        remove_parent_device::RemoveParentDeviceArgs, update::UpdateGeoProbeArgs,
    },
    program_config::{init::InitProgramConfigArgs, update::UpdateProgramConfigArgs},
};

// Instruction indices
pub const INIT_PROGRAM_CONFIG: u8 = 0;
pub const CREATE_GEO_PROBE: u8 = 1;
pub const UPDATE_GEO_PROBE: u8 = 2;
pub const DELETE_GEO_PROBE: u8 = 3;
pub const ADD_PARENT_DEVICE: u8 = 4;
pub const REMOVE_PARENT_DEVICE: u8 = 5;
pub const UPDATE_PROGRAM_CONFIG: u8 = 6;

#[derive(BorshSerialize, Debug, PartialEq, Clone)]
pub enum GeolocationInstruction {
    InitProgramConfig(InitProgramConfigArgs),
    CreateGeoProbe(CreateGeoProbeArgs),
    UpdateGeoProbe(UpdateGeoProbeArgs),
    DeleteGeoProbe,
    AddParentDevice(AddParentDeviceArgs),
    RemoveParentDevice(RemoveParentDeviceArgs),
    UpdateProgramConfig(UpdateProgramConfigArgs),
}

impl GeolocationInstruction {
    pub fn pack(&self) -> Vec<u8> {
        borsh::to_vec(&self).unwrap()
    }

    pub fn unpack(data: &[u8]) -> Result<Self, ProgramError> {
        if data.is_empty() {
            return Err(ProgramError::InvalidInstructionData);
        }

        let (&instruction, rest) = data
            .split_first()
            .ok_or(ProgramError::InvalidInstructionData)?;

        match instruction {
            INIT_PROGRAM_CONFIG => Ok(Self::InitProgramConfig(
                InitProgramConfigArgs::try_from(rest)
                    .map_err(|_| ProgramError::InvalidInstructionData)?,
            )),
            CREATE_GEO_PROBE => Ok(Self::CreateGeoProbe(
                CreateGeoProbeArgs::try_from(rest)
                    .map_err(|_| ProgramError::InvalidInstructionData)?,
            )),
            UPDATE_GEO_PROBE => Ok(Self::UpdateGeoProbe(
                UpdateGeoProbeArgs::try_from(rest)
                    .map_err(|_| ProgramError::InvalidInstructionData)?,
            )),
            DELETE_GEO_PROBE => Ok(Self::DeleteGeoProbe),
            ADD_PARENT_DEVICE => Ok(Self::AddParentDevice(
                AddParentDeviceArgs::try_from(rest)
                    .map_err(|_| ProgramError::InvalidInstructionData)?,
            )),
            REMOVE_PARENT_DEVICE => Ok(Self::RemoveParentDevice(
                RemoveParentDeviceArgs::try_from(rest)
                    .map_err(|_| ProgramError::InvalidInstructionData)?,
            )),
            UPDATE_PROGRAM_CONFIG => Ok(Self::UpdateProgramConfig(
                UpdateProgramConfigArgs::try_from(rest)
                    .map_err(|_| ProgramError::InvalidInstructionData)?,
            )),
            _ => Err(ProgramError::InvalidInstructionData),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use solana_program::pubkey::Pubkey;
    use std::net::Ipv4Addr;

    fn test_instruction(instruction: GeolocationInstruction) {
        let packed = instruction.pack();
        let unpacked = GeolocationInstruction::unpack(&packed).unwrap();
        assert_eq!(instruction, unpacked, "Instruction mismatch");
    }

    #[test]
    fn test_pack_unpack_all_instructions() {
        test_instruction(GeolocationInstruction::InitProgramConfig(
            InitProgramConfigArgs {
                serviceability_program_id: Pubkey::new_unique(),
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
        test_instruction(GeolocationInstruction::AddParentDevice(
            AddParentDeviceArgs {
                device_pk: Pubkey::new_unique(),
            },
        ));
        test_instruction(GeolocationInstruction::RemoveParentDevice(
            RemoveParentDeviceArgs {
                device_pk: Pubkey::new_unique(),
            },
        ));
        test_instruction(GeolocationInstruction::UpdateProgramConfig(
            UpdateProgramConfigArgs {
                serviceability_program_id: Some(Pubkey::new_unique()),
                version: Some(2),
                min_compatible_version: Some(1),
            },
        ));
        test_instruction(GeolocationInstruction::UpdateProgramConfig(
            UpdateProgramConfigArgs {
                serviceability_program_id: None,
                version: None,
                min_compatible_version: None,
            },
        ));
    }

    #[test]
    fn test_unpack_invalid() {
        assert_eq!(
            GeolocationInstruction::unpack(&[]).unwrap_err(),
            ProgramError::InvalidInstructionData,
        );
        assert_eq!(
            GeolocationInstruction::unpack(&[255]).unwrap_err(),
            ProgramError::InvalidInstructionData,
        );
    }
}
