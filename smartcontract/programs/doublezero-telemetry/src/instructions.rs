use crate::processors::telemetry::{
    initialize_dz_samples::InitializeDzLatencySamplesArgs,
    write_dz_samples::WriteDzLatencySamplesArgs,
};
use borsh::{from_slice, BorshDeserialize, BorshSerialize};
use solana_program::program_error::ProgramError;
use std::cmp::PartialEq;

#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq)]
pub enum TelemetryInstruction {
    /// Initialize DZ latency samples account
    InitializeDzLatencySamples(InitializeDzLatencySamplesArgs),
    /// Write DZ latency samples to chain
    WriteDzLatencySamples(WriteDzLatencySamplesArgs),
}

impl TelemetryInstruction {
    pub fn pack(&self) -> Result<Vec<u8>, ProgramError> {
        match borsh::to_vec(&self) {
            Err(e) => Err(ProgramError::BorshIoError(e.to_string())),
            Ok(packed) => Ok(packed),
        }
    }

    pub fn unpack(data: &[u8]) -> Result<Self, ProgramError> {
        if data.is_empty() {
            return Err(ProgramError::InvalidInstructionData);
        }

        let instruction = match data[0] {
            0 => {
                let args: InitializeDzLatencySamplesArgs = from_slice(&data[1..])?;
                TelemetryInstruction::InitializeDzLatencySamples(args)
            }
            2 => {
                let args: WriteDzLatencySamplesArgs = from_slice(&data[1..])?;
                TelemetryInstruction::WriteDzLatencySamples(args)
            }
            _ => return Err(ProgramError::InvalidInstructionData),
        };

        Ok(instruction)
    }
}
