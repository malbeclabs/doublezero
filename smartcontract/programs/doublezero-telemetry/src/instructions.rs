use crate::processors::telemetry::{
    initialize_device_latency_samples::InitializeDeviceLatencySamplesArgs,
    write_device_latency_samples::WriteDeviceLatencySamplesArgs,
};
use borsh::{from_slice, BorshDeserialize, BorshSerialize};
use solana_program::program_error::ProgramError;
use std::cmp::PartialEq;

#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq)]
pub enum TelemetryInstruction {
    /// Initialize device latency samples account
    InitializeDeviceLatencySamples(InitializeDeviceLatencySamplesArgs),
    /// Write device latency samples to chain
    WriteDeviceLatencySamples(WriteDeviceLatencySamplesArgs),
}

pub const INITIALIZE_DEVICE_LATENCY_SAMPLES_INSTRUCTION_INDEX: u8 = 0;
pub const WRITE_DEVICE_LATENCY_SAMPLES_INSTRUCTION_INDEX: u8 = 1;

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
            INITIALIZE_DEVICE_LATENCY_SAMPLES_INSTRUCTION_INDEX => {
                let args: InitializeDeviceLatencySamplesArgs = from_slice(&data[1..])?;
                TelemetryInstruction::InitializeDeviceLatencySamples(args)
            }
            WRITE_DEVICE_LATENCY_SAMPLES_INSTRUCTION_INDEX => {
                let args: WriteDeviceLatencySamplesArgs = from_slice(&data[1..])?;
                TelemetryInstruction::WriteDeviceLatencySamples(args)
            }
            _ => return Err(ProgramError::InvalidInstructionData),
        };

        Ok(instruction)
    }
}
