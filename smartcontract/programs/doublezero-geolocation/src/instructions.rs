use borsh::{BorshDeserialize, BorshSerialize};

pub use crate::processors::program_config::{
    init::InitProgramConfigArgs, update::UpdateProgramConfigArgs,
};

#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone)]
pub enum GeolocationInstruction {
    InitProgramConfig(InitProgramConfigArgs),
    UpdateProgramConfig(UpdateProgramConfigArgs),
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_roundtrip_all_instructions() {
        let cases = vec![
            GeolocationInstruction::InitProgramConfig(InitProgramConfigArgs {}),
            GeolocationInstruction::UpdateProgramConfig(UpdateProgramConfigArgs {
                version: Some(2),
                min_compatible_version: Some(1),
            }),
            GeolocationInstruction::UpdateProgramConfig(UpdateProgramConfigArgs {
                version: None,
                min_compatible_version: None,
            }),
        ];
        for instruction in cases {
            let data = borsh::to_vec(&instruction).unwrap();
            let decoded: GeolocationInstruction = borsh::from_slice(&data).unwrap();
            assert_eq!(instruction, decoded);
        }
    }

    #[test]
    fn test_deserialize_invalid() {
        assert!(borsh::from_slice::<GeolocationInstruction>(&[]).is_err());
        assert!(borsh::from_slice::<GeolocationInstruction>(&[255]).is_err());
    }
}
