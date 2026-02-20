use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use solana_program::{program_error::ProgramError, pubkey::Pubkey};
use std::net::Ipv4Addr;

// Instruction indices
pub const INIT_PROGRAM_CONFIG: u8 = 0;
pub const CREATE_GEO_PROBE: u8 = 1;
pub const UPDATE_GEO_PROBE: u8 = 2;
pub const DELETE_GEO_PROBE: u8 = 3;
pub const ADD_PARENT_DEVICE: u8 = 4;
pub const REMOVE_PARENT_DEVICE: u8 = 5;
pub const CREATE_GEOLOCATION_USER: u8 = 6;
pub const UPDATE_GEOLOCATION_USER: u8 = 7;
pub const DELETE_GEOLOCATION_USER: u8 = 8;
pub const ADD_TARGET: u8 = 9;
pub const REMOVE_TARGET: u8 = 10;
pub const UPDATE_PAYMENT_STATUS: u8 = 11;

// Args structs
#[derive(BorshSerialize, BorshDeserializeIncremental, Debug, PartialEq, Clone)]
pub struct InitProgramConfigArgs {
    pub serviceability_program_id: Pubkey,
}

#[derive(BorshSerialize, BorshDeserializeIncremental, Debug, PartialEq, Clone)]
pub struct CreateGeoProbeArgs {
    pub code: String,
    #[incremental(default = std::net::Ipv4Addr::UNSPECIFIED)]
    pub public_ip: Ipv4Addr,
    pub location_offset_port: u16,
    pub latency_threshold_ns: u64,
    pub metrics_publisher_pk: Pubkey,
}

#[derive(BorshSerialize, BorshDeserializeIncremental, Debug, PartialEq, Clone)]
pub struct UpdateGeoProbeArgs {
    pub public_ip: Option<Ipv4Addr>,
    pub location_offset_port: Option<u16>,
    pub latency_threshold_ns: Option<u64>,
    pub metrics_publisher_pk: Option<Pubkey>,
}

#[derive(BorshSerialize, BorshDeserializeIncremental, Debug, PartialEq, Clone)]
pub struct AddParentDeviceArgs {
    pub device_pk: Pubkey,
}

#[derive(BorshSerialize, BorshDeserializeIncremental, Debug, PartialEq, Clone)]
pub struct RemoveParentDeviceArgs {
    pub device_pk: Pubkey,
}

#[derive(BorshSerialize, BorshDeserializeIncremental, Debug, PartialEq, Clone)]
pub struct CreateGeolocationUserArgs {
    pub code: String,
    pub token_account: Pubkey,
}

#[derive(BorshSerialize, BorshDeserializeIncremental, Debug, PartialEq, Clone)]
pub struct UpdateGeolocationUserArgs {
    pub token_account: Option<Pubkey>,
}

#[derive(BorshSerialize, BorshDeserializeIncremental, Debug, PartialEq, Clone)]
pub struct AddTargetArgs {
    #[incremental(default = std::net::Ipv4Addr::UNSPECIFIED)]
    pub ip_address: Ipv4Addr,
    pub location_offset_port: u16,
    pub exchange_pk: Pubkey,
}

#[derive(BorshSerialize, BorshDeserializeIncremental, Debug, PartialEq, Clone)]
pub struct RemoveTargetArgs {
    #[incremental(default = std::net::Ipv4Addr::UNSPECIFIED)]
    pub target_ip: Ipv4Addr,
    pub exchange_pk: Pubkey,
}

#[derive(BorshSerialize, BorshDeserializeIncremental, Debug, PartialEq, Clone)]
pub struct UpdatePaymentStatusArgs {
    pub payment_status: u8,
    pub last_deduction_dz_epoch: Option<u64>,
}

#[derive(BorshSerialize, Debug, PartialEq, Clone)]
pub enum GeolocationInstruction {
    InitProgramConfig(InitProgramConfigArgs),
    CreateGeoProbe(CreateGeoProbeArgs),
    UpdateGeoProbe(UpdateGeoProbeArgs),
    DeleteGeoProbe,
    AddParentDevice(AddParentDeviceArgs),
    RemoveParentDevice(RemoveParentDeviceArgs),
    CreateGeolocationUser(CreateGeolocationUserArgs),
    UpdateGeolocationUser(UpdateGeolocationUserArgs),
    DeleteGeolocationUser,
    AddTarget(AddTargetArgs),
    RemoveTarget(RemoveTargetArgs),
    UpdatePaymentStatus(UpdatePaymentStatusArgs),
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
            CREATE_GEOLOCATION_USER => Ok(Self::CreateGeolocationUser(
                CreateGeolocationUserArgs::try_from(rest)
                    .map_err(|_| ProgramError::InvalidInstructionData)?,
            )),
            UPDATE_GEOLOCATION_USER => Ok(Self::UpdateGeolocationUser(
                UpdateGeolocationUserArgs::try_from(rest)
                    .map_err(|_| ProgramError::InvalidInstructionData)?,
            )),
            DELETE_GEOLOCATION_USER => Ok(Self::DeleteGeolocationUser),
            ADD_TARGET => Ok(Self::AddTarget(
                AddTargetArgs::try_from(rest).map_err(|_| ProgramError::InvalidInstructionData)?,
            )),
            REMOVE_TARGET => Ok(Self::RemoveTarget(
                RemoveTargetArgs::try_from(rest)
                    .map_err(|_| ProgramError::InvalidInstructionData)?,
            )),
            UPDATE_PAYMENT_STATUS => Ok(Self::UpdatePaymentStatus(
                UpdatePaymentStatusArgs::try_from(rest)
                    .map_err(|_| ProgramError::InvalidInstructionData)?,
            )),
            _ => Err(ProgramError::InvalidInstructionData),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

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
            latency_threshold_ns: 500_000,
        }));
        test_instruction(GeolocationInstruction::UpdateGeoProbe(UpdateGeoProbeArgs {
            public_ip: Some(Ipv4Addr::new(1, 1, 1, 1)),
            location_offset_port: Some(9999),
            metrics_publisher_pk: None,
            latency_threshold_ns: Some(1_000_000),
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
        test_instruction(GeolocationInstruction::CreateGeolocationUser(
            CreateGeolocationUserArgs {
                code: "test-user".to_string(),
                token_account: Pubkey::new_unique(),
            },
        ));
        test_instruction(GeolocationInstruction::UpdateGeolocationUser(
            UpdateGeolocationUserArgs {
                token_account: Some(Pubkey::new_unique()),
            },
        ));
        test_instruction(GeolocationInstruction::DeleteGeolocationUser);
        test_instruction(GeolocationInstruction::AddTarget(AddTargetArgs {
            ip_address: Ipv4Addr::new(203, 0, 113, 42),
            location_offset_port: 443,
            exchange_pk: Pubkey::new_unique(),
        }));
        test_instruction(GeolocationInstruction::RemoveTarget(RemoveTargetArgs {
            target_ip: Ipv4Addr::new(203, 0, 113, 42),
            exchange_pk: Pubkey::new_unique(),
        }));
        test_instruction(GeolocationInstruction::UpdatePaymentStatus(
            UpdatePaymentStatusArgs {
                payment_status: 1,
                last_deduction_dz_epoch: Some(42),
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
