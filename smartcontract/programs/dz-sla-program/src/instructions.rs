use borsh::{from_slice, BorshDeserialize, BorshSerialize};
use solana_program::program_error::ProgramError;
use std::cmp::PartialEq;

use crate::processors::{
    allowlist::{
        device::{add::AddDeviceAllowlistArgs, remove::RemoveDeviceAllowlistArgs},
        foundation::{add::AddFoundationAllowlistArgs, remove::RemoveFoundationAllowlistArgs},
        user::{add::AddUserAllowlistArgs, remove::RemoveUserAllowlistArgs},
    },
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
        activate::UserActivateArgs, ban::UserBanArgs, create::UserCreateArgs,
        deactivate::UserDeactivateArgs, delete::UserDeleteArgs, reactivate::UserReactivateArgs,
        reject::UserRejectArgs, requestban::UserRequestBanArgs, suspend::UserSuspendArgs,
        update::UserUpdateArgs,
    },
};

// Instructions that our program can execute
#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone)]
pub enum DoubleZeroInstruction {
    InitGlobalState(),                    // variant 0
    SetGlobalConfig(SetGlobalConfigArgs), // variant 1

    CreateLocation(LocationCreateArgs),         // variant 2
    UpdateLocation(LocationUpdateArgs),         // variant 3
    SuspendLocation(LocationSuspendArgs),       // variant 4
    ReactivateLocation(LocationReactivateArgs), // variant 5
    DeleteLocation(LocationDeleteArgs),         // variant 6

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

    AddFoundationAllowlist(AddFoundationAllowlistArgs), // variant 36
    RemoveFoundationAllowlist(RemoveFoundationAllowlistArgs), // variant 37
    AddDeviceAllowlist(AddDeviceAllowlistArgs),         // variant 38
    RemoveDeviceAllowlist(RemoveDeviceAllowlistArgs),   // variant 39

    AddUserAllowlist(AddUserAllowlistArgs),       // variant 40
    RemoveUserAllowlist(RemoveUserAllowlistArgs), // variant 41

    RequestBanUser(UserRequestBanArgs), // variant 42
    BanUser(UserBanArgs),               // variant 43
}

impl DoubleZeroInstruction {
    pub fn pack(&self) -> Vec<u8> {
        borsh::to_vec(&self).unwrap()
    }
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

            36 => Ok(Self::AddFoundationAllowlist(from_slice::<AddFoundationAllowlistArgs>(rest).unwrap())),
            37 => Ok(Self::RemoveFoundationAllowlist(from_slice::<RemoveFoundationAllowlistArgs>(rest).unwrap())),
            38 => Ok(Self::AddDeviceAllowlist(from_slice::<AddDeviceAllowlistArgs>(rest).unwrap())),
            39 => Ok(Self::RemoveDeviceAllowlist(from_slice::<RemoveDeviceAllowlistArgs>(rest).unwrap())),
            40 => Ok(Self::AddUserAllowlist(from_slice::<AddUserAllowlistArgs>(rest).unwrap())),
            41 => Ok(Self::RemoveUserAllowlist(from_slice::<RemoveUserAllowlistArgs>(rest).unwrap())),

            42 => Ok(Self::RequestBanUser(from_slice::<UserRequestBanArgs>(rest).unwrap())),
            43 => Ok(Self::BanUser(from_slice::<UserBanArgs>(rest).unwrap())),

            _ => Err(ProgramError::InvalidInstructionData),
        }
    }

    pub fn get_name(&self) -> String {
        match self {
            Self::InitGlobalState() => "InitGlobalState".to_string(), // variant 0
            Self::SetGlobalConfig(_) => "SetGlobalConfig".to_string(), // variant 1

            Self::CreateLocation(_) => "CreateLocation".to_string(), // variant 2
            Self::UpdateLocation(_) => "UpdateLocation".to_string(), // variant 3
            Self::SuspendLocation(_) => "SuspendLocation".to_string(), // variant 4
            Self::ReactivateLocation(_) => "ReactivateLocation".to_string(), // variant 5
            Self::DeleteLocation(_) => "DeleteLocation".to_string(), // variant 6

            Self::CreateExchange(_) => "CreateExchange".to_string(), // variant 7
            Self::UpdateExchange(_) => "UpdateExchange".to_string(), // variant 8
            Self::SuspendExchange(_) => "SuspendExchange".to_string(), // variant 9
            Self::ReactivateExchange(_) => "ReactivateExchange".to_string(), // variant 10
            Self::DeleteExchange(_) => "DeleteExchange".to_string(), // variant 11

            Self::CreateDevice(_) => "CreateDevice".to_string(), // variant 12
            Self::ActivateDevice(_) => "ActivateDevice".to_string(), // variant 13
            Self::UpdateDevice(_) => "UpdateDevice".to_string(), // variant 14
            Self::SuspendDevice(_) => "SuspendDevice".to_string(), // variant 15
            Self::ReactivateDevice(_) => "ReactivateDevice".to_string(), // variant 16
            Self::DeleteDevice(_) => "DeleteDevice".to_string(), // variant 17

            Self::CreateTunnel(_) => "CreateTunnel".to_string(), // variant 18
            Self::ActivateTunnel(_) => "ActivateTunnel".to_string(), // variant 19
            Self::UpdateTunnel(_) => "UpdateTunnel".to_string(), // variant 20
            Self::SuspendTunnel(_) => "SuspendTunnel".to_string(), // variant 21
            Self::ReactivateTunnel(_) => "ReactivateTunnel".to_string(), // variant 22
            Self::DeleteTunnel(_) => "DeleteTunnel".to_string(), // variant 23

            Self::CreateUser(_) => "CreateUser".to_string(), // variant 24
            Self::ActivateUser(_) => "ActivateUser".to_string(), // variant 25
            Self::UpdateUser(_) => "UpdateUser".to_string(), // variant 26
            Self::SuspendUser(_) => "SuspendUser".to_string(), // variant 27
            Self::ReactivateUser(_) => "ReactivateUser".to_string(), // variant 28
            Self::DeleteUser(_) => "DeleteUser".to_string(), // variant 29

            Self::DeactivateDevice(_) => "DeactivateDevice".to_string(), // variant 30
            Self::DeactivateTunnel(_) => "DeactivateTunnel".to_string(), // variant 31
            Self::DeactivateUser(_) => "DeactivateUser".to_string(),     // variant 32

            Self::RejectDevice(_) => "RejectDevice".to_string(), // variant 33
            Self::RejectTunnel(_) => "RejectTunnel".to_string(), // variant 34
            Self::RejectUser(_) => "RejectUser".to_string(),     // variant 35

            Self::AddFoundationAllowlist(_) => "AddFoundationAllowlist".to_string(), // variant 36
            Self::RemoveFoundationAllowlist(_) => "RemoveFoundationAllowlist".to_string(), // variant 37
            Self::AddDeviceAllowlist(_) => "AddDeviceAllowlist".to_string(), // variant 38
            Self::RemoveDeviceAllowlist(_) => "RemoveDeviceAllowlist".to_string(), // variant 39
            Self::AddUserAllowlist(_) => "AddUserAllowlist".to_string(),     // variant 40
            Self::RemoveUserAllowlist(_) => "RemoveUserAllowlist".to_string(), // variant 41

            Self::RequestBanUser(_) => "RequestBanUser".to_string(), // variant 42
            Self::BanUser(_) => "BanUser".to_string(),               // variant 43
        }
    }

    pub fn get_args(&self) -> String {
        match self {
            Self::InitGlobalState() => "".to_string(), // variant 0
            Self::SetGlobalConfig(args) => format!("{:?}", args), // variant 1

            Self::CreateLocation(args) => format!("{:?}", args), // variant 2
            Self::UpdateLocation(args) => format!("{:?}", args), // variant 3
            Self::SuspendLocation(args) => format!("{:?}", args), // variant 4
            Self::ReactivateLocation(args) => format!("{:?}", args), // variant 5
            Self::DeleteLocation(args) => format!("{:?}", args), // variant 6

            Self::CreateExchange(args) => format!("{:?}", args), // variant 7
            Self::UpdateExchange(args) => format!("{:?}", args), // variant 8
            Self::SuspendExchange(args) => format!("{:?}", args), // variant 9
            Self::ReactivateExchange(args) => format!("{:?}", args), // variant 10
            Self::DeleteExchange(args) => format!("{:?}", args), // variant 11

            Self::CreateDevice(args) => format!("{:?}", args), // variant 12
            Self::ActivateDevice(args) => format!("{:?}", args), // variant 13
            Self::UpdateDevice(args) => format!("{:?}", args), // variant 14
            Self::SuspendDevice(args) => format!("{:?}", args), // variant 15
            Self::ReactivateDevice(args) => format!("{:?}", args), // variant 16
            Self::DeleteDevice(args) => format!("{:?}", args), // variant 17

            Self::CreateTunnel(args) => format!("{:?}", args), // variant 18
            Self::ActivateTunnel(args) => format!("{:?}", args), // variant 19
            Self::UpdateTunnel(args) => format!("{:?}", args), // variant 20
            Self::SuspendTunnel(args) => format!("{:?}", args), // variant 21
            Self::ReactivateTunnel(args) => format!("{:?}", args), // variant 22
            Self::DeleteTunnel(args) => format!("{:?}", args), // variant 23

            Self::CreateUser(args) => format!("{:?}", args), // variant 24
            Self::ActivateUser(args) => format!("{:?}", args), // variant 25
            Self::UpdateUser(args) => format!("{:?}", args), // variant 26
            Self::SuspendUser(args) => format!("{:?}", args), // variant 27
            Self::ReactivateUser(args) => format!("{:?}", args), // variant 28
            Self::DeleteUser(args) => format!("{:?}", args), // variant 29

            Self::DeactivateDevice(args) => format!("{:?}", args), // variant 30
            Self::DeactivateTunnel(args) => format!("{:?}", args), // variant 31
            Self::DeactivateUser(args) => format!("{:?}", args),   // variant 32

            Self::RejectDevice(args) => format!("{:?}", args), // variant 33
            Self::RejectTunnel(args) => format!("{:?}", args), // variant 34
            Self::RejectUser(args) => format!("{:?}", args),   // variant 35

            Self::AddFoundationAllowlist(args) => format!("{:?}", args), // variant 36
            Self::RemoveFoundationAllowlist(args) => format!("{:?}", args), // variant 37
            Self::AddDeviceAllowlist(args) => format!("{:?}", args),     // variant 38
            Self::RemoveDeviceAllowlist(args) => format!("{:?}", args),  // variant 39
            Self::AddUserAllowlist(args) => format!("{:?}", args),       // variant 40
            Self::RemoveUserAllowlist(args) => format!("{:?}", args),    // variant 41

            Self::RequestBanUser(args) => format!("{:?}", args), // variant 42
            Self::BanUser(args) => format!("{:?}", args),        // variant 43
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
