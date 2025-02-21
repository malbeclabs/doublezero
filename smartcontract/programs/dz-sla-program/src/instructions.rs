use borsh::{from_slice, BorshDeserialize, BorshSerialize};
use solana_program::program_error::ProgramError;
use std::cmp::PartialEq;

use crate::processors::{
    device::{
        activate::DeviceActivateArgs, create::DeviceCreateArgs, deactivate::DeviceDeactivateArgs,
        delete::DeviceDeleteArgs, reactivate::DeviceReactivateArgs, reject::DeviceRejectArgs,
        suspend::DeviceSuspendArgs, update::DeviceUpdateArgs,
    },
    exchange::{
        create::ExchangeCreateArgs, delete::ExchangeDeleteArgs, reactivate::ExchangeReactivateArgs,
        suspend::ExchangeSuspendArgs, update::ExchangeUpdateArgs,
    },
    globalconfig::set::SetGlobalConfigArgs,
    globalstate::{
        device_allowlist::{
            add::AddDeviceAllowlistGlobalConfigArgs, remove::RemoveDeviceAllowlistGlobalConfigArgs,
        }, foundation_allowlist::{add::AddFoundationAllowlistGlobalConfigArgs, remove::RemoveFoundationAllowlistGlobalConfigArgs}, user_allowlist::{add::AddUserAllowlistGlobalConfigArgs, remove::RemoveUserAllowlistGlobalConfigArgs}
    },
    location::{
        create::LocationCreateArgs, delete::LocationDeleteArgs, reactivate::LocationReactivateArgs,
        suspend::LocationSuspendArgs, update::LocationUpdateArgs,
    },
    tunnel::{
        activate::TunnelActivateArgs, create::TunnelCreateArgs, deactivate::TunnelDeactivateArgs,
        delete::TunnelDeleteArgs, reactivate::TunnelReactivateArgs, reject::TunnelRejectArgs,
        suspend::TunnelSuspendArgs, update::TunnelUpdateArgs,
    },
    user::{
        activate::UserActivateArgs, requestban::UserRequestBanArgs, ban::UserBanArgs, create::UserCreateArgs, deactivate::UserDeactivateArgs, delete::UserDeleteArgs, reactivate::UserReactivateArgs, reject::UserRejectArgs, suspend::UserSuspendArgs, update::UserUpdateArgs
    },
};

// Instructions that our program can execute
#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq)]
pub enum DoubleZeroInstruction {
    InitGlobalState(),                    // variant 0
    SetGlobalConfig(SetGlobalConfigArgs), // variant 1

    CreateLocation(LocationCreateArgs),        // variant 2
    UpdateLocation(LocationUpdateArgs),        // variant 3
    SuspendLocation(LocationSuspendArgs),      // variant 4
    ReactivateLocation(LocationReactivateArgs), // variant 5
    DeleteLocation(LocationDeleteArgs),        // variant 6

    CreateExchange(ExchangeCreateArgs),         // variant 7
    UpdateExchange(ExchangeUpdateArgs),         // variant 8
    SuspendExchange(ExchangeSuspendArgs),       // variant 9
    ReactivateExchange(ExchangeReactivateArgs), // variant 10
    DeleteExchange(ExchangeDeleteArgs),         // variant 11

    CreateDevice(DeviceCreateArgs),         // variant 12
    ActivateDevice(DeviceActivateArgs),     // variant 13
    UpdateDevice(DeviceUpdateArgs),         // variant 14
    SuspendDevice(DeviceSuspendArgs),       // variant 15
    ReactivateDevice(DeviceReactivateArgs), // variant 16
    DeleteDevice(DeviceDeleteArgs),         // variant 17

    CreateTunnel(TunnelCreateArgs),         // variant 18
    ActivateTunnel(TunnelActivateArgs),     // variant 19
    UpdateTunnel(TunnelUpdateArgs),         // variant 20
    SuspendTunnel(TunnelSuspendArgs),       // variant 21
    ReactivateTunnel(TunnelReactivateArgs), // variant 22
    DeleteTunnel(TunnelDeleteArgs),         // variant 23

    CreateUser(UserCreateArgs),         // variant 24
    ActivateUser(UserActivateArgs),     // variant 25
    UpdateUser(UserUpdateArgs),         // variant 26
    SuspendUser(UserSuspendArgs),       // variant 27
    ReactivateUser(UserReactivateArgs), // variant 28
    DeleteUser(UserDeleteArgs),         // variant 29

    DeactivateDevice(DeviceDeactivateArgs), // variant 30
    DeactivateTunnel(TunnelDeactivateArgs), // variant 31
    DeactivateUser(UserDeactivateArgs),     // variant 32

    RejectDevice(DeviceRejectArgs), // variant 33
    RejectTunnel(TunnelRejectArgs), // variant 34
    RejectUser(UserRejectArgs),     // variant 35

    AddFoundationAllowlistGlobalConfig(AddFoundationAllowlistGlobalConfigArgs), // variant 36
    RemoveFoundationAllowlistGlobalConfig(RemoveFoundationAllowlistGlobalConfigArgs), // variant 37
    AddDeviceAllowlistGlobalConfig(AddDeviceAllowlistGlobalConfigArgs), // variant 38
    RemoveDeviceAllowlistGlobalConfig(RemoveDeviceAllowlistGlobalConfigArgs), // variant 39
    AddUserAllowlistGlobalConfig(AddUserAllowlistGlobalConfigArgs),     // variant 40
    RemoveUserAllowlistGlobalConfig(RemoveUserAllowlistGlobalConfigArgs), // variant 41

    RequestBanUser(UserRequestBanArgs), // variant 42
    BanUser(UserBanArgs),  // variant 43
}

impl DoubleZeroInstruction {
    #[rustfmt::skip]
    pub fn unpack(input: &[u8]) -> Result<Self, ProgramError> {
        let (&instruction, rest) = input
            .split_first()
            .ok_or(ProgramError::InvalidInstructionData)?;

        match instruction {
            0 => Ok(Self::InitGlobalState()),
            1 => Ok(Self::SetGlobalConfig(from_slice::<SetGlobalConfigArgs>(rest).unwrap())),

            2 => Ok(Self::CreateLocation(from_slice::<LocationCreateArgs>(rest).unwrap())),
            3 => Ok(Self::UpdateLocation(from_slice::<LocationUpdateArgs>(rest).unwrap())),
            4 => Ok(Self::SuspendLocation(from_slice::<LocationSuspendArgs>(rest).unwrap())),
            5 => Ok(Self::ReactivateLocation(from_slice::<LocationReactivateArgs>(rest).unwrap())),
            6 => Ok(Self::DeleteLocation(from_slice::<LocationDeleteArgs>(rest).unwrap())),

            7 => Ok(Self::CreateExchange(from_slice::<ExchangeCreateArgs>(rest).unwrap())),
            8 => Ok(Self::UpdateExchange(from_slice::<ExchangeUpdateArgs>(rest).unwrap())),
            9 => Ok(Self::SuspendExchange(from_slice::<ExchangeSuspendArgs>(rest).unwrap())),
            10 => Ok(Self::ReactivateExchange(from_slice::<ExchangeReactivateArgs>(rest).unwrap())),
            11 => Ok(Self::DeleteExchange(from_slice::<ExchangeDeleteArgs>(rest).unwrap())),

            12 => Ok(Self::CreateDevice(from_slice::<DeviceCreateArgs>(rest).unwrap())),
            13 => Ok(Self::ActivateDevice(from_slice::<DeviceActivateArgs>(rest).unwrap())),
            14 => Ok(Self::UpdateDevice(from_slice::<DeviceUpdateArgs>(rest).unwrap())),
            15 => Ok(Self::SuspendDevice(from_slice::<DeviceSuspendArgs>(rest).unwrap())),
            16 => Ok(Self::ReactivateDevice(from_slice::<DeviceReactivateArgs>(rest).unwrap())),
            17 => Ok(Self::DeleteDevice(from_slice::<DeviceDeleteArgs>(rest).unwrap())),

            18 => Ok(Self::CreateTunnel(from_slice::<TunnelCreateArgs>(rest).unwrap())),
            19 => Ok(Self::ActivateTunnel(from_slice::<TunnelActivateArgs>(rest).unwrap())),
            20 => Ok(Self::UpdateTunnel(from_slice::<TunnelUpdateArgs>(rest).unwrap())),
            21 => Ok(Self::SuspendTunnel(from_slice::<TunnelSuspendArgs>(rest).unwrap())),
            22 => Ok(Self::ReactivateTunnel(from_slice::<TunnelReactivateArgs>(rest).unwrap())),
            23 => Ok(Self::DeleteTunnel(from_slice::<TunnelDeleteArgs>(rest).unwrap())),

            24 => Ok(Self::CreateUser(from_slice::<UserCreateArgs>(rest).unwrap())),
            25 => Ok(Self::ActivateUser(from_slice::<UserActivateArgs>(rest).unwrap())),
            26 => Ok(Self::UpdateUser(from_slice::<UserUpdateArgs>(rest).unwrap())),
            27 => Ok(Self::SuspendUser(from_slice::<UserSuspendArgs>(rest).unwrap())),
            28 => Ok(Self::ReactivateUser(from_slice::<UserReactivateArgs>(rest).unwrap())),
            29 => Ok(Self::DeleteUser(from_slice::<UserDeleteArgs>(rest).unwrap())),

            30 => Ok(Self::DeactivateDevice(from_slice::<DeviceDeactivateArgs>(rest).unwrap())),
            31 => Ok(Self::DeactivateTunnel(from_slice::<TunnelDeactivateArgs>(rest).unwrap())),
            32 => Ok(Self::DeactivateUser(from_slice::<UserDeactivateArgs>(rest).unwrap())),

            33 => Ok(Self::RejectDevice(from_slice::<DeviceRejectArgs>(rest).unwrap())),
            34 => Ok(Self::RejectTunnel(from_slice::<TunnelRejectArgs>(rest).unwrap())),
            35 => Ok(Self::RejectUser(from_slice::<UserRejectArgs>(rest).unwrap())),

            36 => Ok(Self::AddFoundationAllowlistGlobalConfig(from_slice::<AddFoundationAllowlistGlobalConfigArgs>(rest).unwrap())),
            37 => Ok(Self::RemoveFoundationAllowlistGlobalConfig(from_slice::<RemoveFoundationAllowlistGlobalConfigArgs>(rest).unwrap())),
            38 => Ok(Self::AddDeviceAllowlistGlobalConfig(from_slice::<AddDeviceAllowlistGlobalConfigArgs>(rest).unwrap())),
            39 => Ok(Self::RemoveDeviceAllowlistGlobalConfig(from_slice::<RemoveDeviceAllowlistGlobalConfigArgs>(rest).unwrap())),
            40 => Ok(Self::AddUserAllowlistGlobalConfig(from_slice::<AddUserAllowlistGlobalConfigArgs>(rest).unwrap())),
            41 => Ok(Self::RemoveUserAllowlistGlobalConfig(from_slice::<RemoveUserAllowlistGlobalConfigArgs>(rest).unwrap())),

            42 => Ok(Self::RequestBanUser(from_slice::<UserRequestBanArgs>(rest).unwrap())),
            43 => Ok(Self::BanUser(from_slice::<UserBanArgs>(rest).unwrap())),

            _ => Err(ProgramError::InvalidInstructionData),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn serialize_deserialize(input: &DoubleZeroInstruction) -> DoubleZeroInstruction {
        let mut data: Vec<u8> = vec![];
        input.serialize(&mut data).unwrap();
        let output = DoubleZeroInstruction::unpack(&data).unwrap();

        output
    }

    #[test]
    fn test_doublesero_instruction() {
        let a = DoubleZeroInstruction::InitGlobalState();
        let b = serialize_deserialize(&a);
        assert_eq!(a, b);
    }
}
