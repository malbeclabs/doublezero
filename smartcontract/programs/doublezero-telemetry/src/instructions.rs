use crate::processors::telemetry::{
    initialize_device_latency_samples::InitializeDeviceLatencySamplesArgs,
    initialize_internet_latency_samples::InitializeInternetLatencySamplesArgs,
    write_device_latency_samples::WriteDeviceLatencySamplesArgs,
    write_internet_latency_samples::WriteInternetLatencySamplesArgs,
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
    /// Initialize internet latency samples account,
    InitializeInternetLatencySamples(InitializeInternetLatencySamplesArgs),
    /// Write internet latency samples to chain
    WriteInternetLatencySamples(WriteInternetLatencySamplesArgs),
}

pub const INITIALIZE_DEVICE_LATENCY_SAMPLES_INSTRUCTION_INDEX: u8 = 0;
pub const WRITE_DEVICE_LATENCY_SAMPLES_INSTRUCTION_INDEX: u8 = 1;
pub const INITIALIZE_INTERNET_LATENCY_SAMPLES_INSTRUCTION_INDEX: u8 = 2;
pub const WRITE_INTERNET_LATENCY_SAMPLES_INSTRUCTION_INDEX: u8 = 3;

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
            INITIALIZE_INTERNET_LATENCY_SAMPLES_INSTRUCTION_INDEX => {
                let args: InitializeInternetLatencySamplesArgs = from_slice(&data[1..])?;
                TelemetryInstruction::InitializeInternetLatencySamples(args)
            }
            WRITE_INTERNET_LATENCY_SAMPLES_INSTRUCTION_INDEX => {
                let args: WriteInternetLatencySamplesArgs = from_slice(&data[1..])?;
                TelemetryInstruction::WriteInternetLatencySamples(args)
            }
            _ => return Err(ProgramError::InvalidInstructionData),
        };

        Ok(instruction)
    }
}
