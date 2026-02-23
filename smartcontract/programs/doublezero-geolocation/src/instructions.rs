use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::program_error::ProgramError;

pub use crate::processors::program_config::{
    init::InitProgramConfigArgs, update::UpdateProgramConfigArgs,
};

#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone)]
pub enum GeolocationInstruction {
    InitProgramConfig(InitProgramConfigArgs),
    UpdateProgramConfig(UpdateProgramConfigArgs),
}

impl GeolocationInstruction {
    pub fn pack(&self) -> Vec<u8> {
        borsh::to_vec(&self).unwrap()
    }

    pub fn unpack(data: &[u8]) -> Result<Self, ProgramError> {
        borsh::from_slice(data).map_err(|_| ProgramError::InvalidInstructionData)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use solana_program::pubkey::Pubkey;

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
