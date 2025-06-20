use crate::processors::telemetry::{
    initialize_dz_samples::InitializeDzLatencySamplesArgs,
    initialize_thirdparty_samples::InitializeThirdPartyLatencySamplesArgs,
    write_dz_samples::WriteDzLatencySamplesArgs,
    write_thirdparty_samples::WriteThirdPartyLatencySamplesArgs,
};
use borsh::{from_slice, BorshDeserialize, BorshSerialize};
use solana_program::program_error::ProgramError;
use std::cmp::PartialEq;

#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq)]
pub enum TelemetryInstruction {
    /// Initialize DZ latency samples account
    InitializeDzLatencySamples(InitializeDzLatencySamplesArgs),
    /// Initialize third-party latency samples account
    InitializeThirdPartyLatencySamples(InitializeThirdPartyLatencySamplesArgs),
    /// Write DZ latency samples to chain
    WriteDzLatencySamples(WriteDzLatencySamplesArgs),
    /// Write third-party latency samples to chain
    WriteThirdPartyLatencySamples(WriteThirdPartyLatencySamplesArgs),
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
            1 => {
                let args: InitializeThirdPartyLatencySamplesArgs = from_slice(&data[1..])?;
                TelemetryInstruction::InitializeThirdPartyLatencySamples(args)
            }
            2 => {
                let args: WriteDzLatencySamplesArgs = from_slice(&data[1..])?;
                TelemetryInstruction::WriteDzLatencySamples(args)
            }
            3 => {
                let args: WriteThirdPartyLatencySamplesArgs = from_slice(&data[1..])?;
                TelemetryInstruction::WriteThirdPartyLatencySamples(args)
            }
            _ => return Err(ProgramError::InvalidInstructionData),
        };

        Ok(instruction)
    }
}
