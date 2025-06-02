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
        activate::DeviceActivateArgs, create::DeviceCreateArgs, closeaccount::DeviceCloseAccountArgs,
        delete::DeviceDeleteArgs, resume::DeviceResumeArgs, reject::DeviceRejectArgs,
        suspend::DeviceSuspendArgs, update::DeviceUpdateArgs,
    },
    exchange::{
        create::ExchangeCreateArgs, delete::ExchangeDeleteArgs, resume::ExchangeResumeArgs,
        suspend::ExchangeSuspendArgs, update::ExchangeUpdateArgs,
    },
    globalconfig::set::SetGlobalConfigArgs,
    globalstate::close::CloseAccountArgs,
    location::{
        create::LocationCreateArgs, delete::LocationDeleteArgs, resume::LocationResumeArgs,
        suspend::LocationSuspendArgs, update::LocationUpdateArgs,
    },
    link::{
        activate::LinkActivateArgs, create::LinkCreateArgs, closeaccount::LinkCloseAccountArgs,
        delete::LinkDeleteArgs, resume::LinkResumeArgs, reject::LinkRejectArgs,
        suspend::LinkSuspendArgs, update::LinkUpdateArgs,
    },
    user::{
        activate::UserActivateArgs, ban::UserBanArgs, create::UserCreateArgs,
        closeaccount::UserCloseAccountArgs, delete::UserDeleteArgs, resume::UserResumeArgs,
        reject::UserRejectArgs, requestban::UserRequestBanArgs, suspend::UserSuspendArgs,
        update::UserUpdateArgs,
    },
};

// Instructions that our program can execute
#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone)]
pub enum DoubleZeroInstruction {
    None(),
    InitGlobalState(),                    // variant 1
    CloseAccount(CloseAccountArgs),       // variant 2
    SetGlobalConfig(SetGlobalConfigArgs), // variant 3

    AddFoundationAllowlist(AddFoundationAllowlistArgs), // variant 4
    RemoveFoundationAllowlist(RemoveFoundationAllowlistArgs), // variant 5
    AddDeviceAllowlist(AddDeviceAllowlistArgs),         // variant 6
    RemoveDeviceAllowlist(RemoveDeviceAllowlistArgs),   // variant 7
    AddUserAllowlist(AddUserAllowlistArgs),             // variant 8
    RemoveUserAllowlist(RemoveUserAllowlistArgs),       // variant 9

    CreateLocation(LocationCreateArgs),         // variant 10
    UpdateLocation(LocationUpdateArgs),         // variant 11
    SuspendLocation(LocationSuspendArgs),       // variant 12
    ResumeLocation(LocationResumeArgs), // variant 13
    DeleteLocation(LocationDeleteArgs),         // variant 14

    CreateExchange(ExchangeCreateArgs),         // variant 15
    UpdateExchange(ExchangeUpdateArgs),         // variant 16
    SuspendExchange(ExchangeSuspendArgs),       // variant 17
    ResumeExchange(ExchangeResumeArgs), // variant 18
    DeleteExchange(ExchangeDeleteArgs),         // variant 19

    CreateDevice(DeviceCreateArgs),         // variant 20
    ActivateDevice(DeviceActivateArgs),     // variant 21
    RejectDevice(DeviceRejectArgs),         // variant 22
    UpdateDevice(DeviceUpdateArgs),         // variant 23
    SuspendDevice(DeviceSuspendArgs),       // variant 24
    ResumeDevice(DeviceResumeArgs), // variant 25
    DeleteDevice(DeviceDeleteArgs),         // variant 26
    CloseAccountDevice(DeviceCloseAccountArgs), // variant 27

    CreateLink(LinkCreateArgs),         // variant 28
    ActivateLink(LinkActivateArgs),     // variant 29
    RejectLink(LinkRejectArgs),         // variant 30
    UpdateLink(LinkUpdateArgs),         // variant 31
    SuspendLink(LinkSuspendArgs),       // variant 32
    ResumeLink(LinkResumeArgs), // variant 33
    DeleteLink(LinkDeleteArgs),         // variant 34
    CloseAccountLink(LinkCloseAccountArgs), // variant 35

    CreateUser(UserCreateArgs),     // variant 36
    ActivateUser(UserActivateArgs), // variant 37
    RejectUser(UserRejectArgs),     // variant 38

    UpdateUser(UserUpdateArgs),         // variant 39
    SuspendUser(UserSuspendArgs),       // variant 40
    ResumeUser(UserResumeArgs), // variant 41
    DeleteUser(UserDeleteArgs),         // variant 42
    CloseAccountUser(UserCloseAccountArgs), // variant 42
    RequestBanUser(UserRequestBanArgs), // variant 44
    BanUser(UserBanArgs),               // variant 45
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
            1 => Ok(Self::InitGlobalState()),
            2 => Ok(Self::CloseAccount(from_slice::<CloseAccountArgs>(rest).unwrap())),
            3 => Ok(Self::SetGlobalConfig(from_slice::<SetGlobalConfigArgs>(rest).unwrap())),

            4 => Ok(Self::AddFoundationAllowlist(from_slice::<AddFoundationAllowlistArgs>(rest).unwrap())),
            5 => Ok(Self::RemoveFoundationAllowlist(from_slice::<RemoveFoundationAllowlistArgs>(rest).unwrap())),
            6 => Ok(Self::AddDeviceAllowlist(from_slice::<AddDeviceAllowlistArgs>(rest).unwrap())),
            7 => Ok(Self::RemoveDeviceAllowlist(from_slice::<RemoveDeviceAllowlistArgs>(rest).unwrap())),
            8 => Ok(Self::AddUserAllowlist(from_slice::<AddUserAllowlistArgs>(rest).unwrap())),
            9 => Ok(Self::RemoveUserAllowlist(from_slice::<RemoveUserAllowlistArgs>(rest).unwrap())),

            10 => Ok(Self::CreateLocation(from_slice::<LocationCreateArgs>(rest).unwrap())),
            11 => Ok(Self::UpdateLocation(from_slice::<LocationUpdateArgs>(rest).unwrap())),
            12 => Ok(Self::SuspendLocation(from_slice::<LocationSuspendArgs>(rest).unwrap())),
            13 => Ok(Self::ResumeLocation(from_slice::<LocationResumeArgs>(rest).unwrap())),
            14 => Ok(Self::DeleteLocation(from_slice::<LocationDeleteArgs>(rest).unwrap())),

            15 => Ok(Self::CreateExchange(from_slice::<ExchangeCreateArgs>(rest).unwrap())),
            16 => Ok(Self::UpdateExchange(from_slice::<ExchangeUpdateArgs>(rest).unwrap())),
            17 => Ok(Self::SuspendExchange(from_slice::<ExchangeSuspendArgs>(rest).unwrap())),
            18 => Ok(Self::ResumeExchange(from_slice::<ExchangeResumeArgs>(rest).unwrap())),
            19 => Ok(Self::DeleteExchange(from_slice::<ExchangeDeleteArgs>(rest).unwrap())),
            
            20 => Ok(Self::CreateDevice(from_slice::<DeviceCreateArgs>(rest).unwrap())),
            21 => Ok(Self::ActivateDevice(from_slice::<DeviceActivateArgs>(rest).unwrap())),
            22 => Ok(Self::RejectDevice(from_slice::<DeviceRejectArgs>(rest).unwrap())),
            23 => Ok(Self::UpdateDevice(from_slice::<DeviceUpdateArgs>(rest).unwrap())),
            24 => Ok(Self::SuspendDevice(from_slice::<DeviceSuspendArgs>(rest).unwrap())),
            25 => Ok(Self::ResumeDevice(from_slice::<DeviceResumeArgs>(rest).unwrap())),
            26 => Ok(Self::DeleteDevice(from_slice::<DeviceDeleteArgs>(rest).unwrap())),
            27 => Ok(Self::CloseAccountDevice(from_slice::<DeviceCloseAccountArgs>(rest).unwrap())),

            28 => Ok(Self::CreateLink(from_slice::<LinkCreateArgs>(rest).unwrap())),
            29 => Ok(Self::ActivateLink(from_slice::<LinkActivateArgs>(rest).unwrap())),
            30 => Ok(Self::RejectLink(from_slice::<LinkRejectArgs>(rest).unwrap())),
            31 => Ok(Self::UpdateLink(from_slice::<LinkUpdateArgs>(rest).unwrap())),
            32 => Ok(Self::SuspendLink(from_slice::<LinkSuspendArgs>(rest).unwrap())),
            33 => Ok(Self::ResumeLink(from_slice::<LinkResumeArgs>(rest).unwrap())),
            34 => Ok(Self::DeleteLink(from_slice::<LinkDeleteArgs>(rest).unwrap())),
            35 => Ok(Self::CloseAccountLink(from_slice::<LinkCloseAccountArgs>(rest).unwrap())),

            36 => Ok(Self::CreateUser(from_slice::<UserCreateArgs>(rest).unwrap())),
            37 => Ok(Self::ActivateUser(from_slice::<UserActivateArgs>(rest).unwrap())),
            38 => Ok(Self::RejectUser(from_slice::<UserRejectArgs>(rest).unwrap())),
            39 => Ok(Self::UpdateUser(from_slice::<UserUpdateArgs>(rest).unwrap())),
            40 => Ok(Self::SuspendUser(from_slice::<UserSuspendArgs>(rest).unwrap())),
            41 => Ok(Self::ResumeUser(from_slice::<UserResumeArgs>(rest).unwrap())),
            42 => Ok(Self::DeleteUser(from_slice::<UserDeleteArgs>(rest).unwrap())),
            43 => Ok(Self::CloseAccountUser(from_slice::<UserCloseAccountArgs>(rest).unwrap())),
            44 => Ok(Self::RequestBanUser(from_slice::<UserRequestBanArgs>(rest).unwrap())),
            45 => Ok(Self::BanUser(from_slice::<UserBanArgs>(rest).unwrap())),        
            _ => Err(ProgramError::InvalidInstructionData),
        }
    }

    pub fn get_name(&self) -> String {
        match self {
            Self::None() => "None".to_string(), // variant 0
            Self::InitGlobalState() => "InitGlobalState".to_string(), // variant 1
            Self::CloseAccount(_) => "CloseAccount".to_string(), // variant 2
            Self::SetGlobalConfig(_) => "SetGlobalConfig".to_string(), // variant 3

            Self::AddFoundationAllowlist(_) => "AddFoundationAllowlist".to_string(), // variant 4
            Self::RemoveFoundationAllowlist(_) => "RemoveFoundationAllowlist".to_string(), // variant 5
            Self::AddDeviceAllowlist(_) => "AddDeviceAllowlist".to_string(), // variant 6
            Self::RemoveDeviceAllowlist(_) => "RemoveDeviceAllowlist".to_string(), // variant 7
            Self::AddUserAllowlist(_) => "AddUserAllowlist".to_string(), // variant 8
            Self::RemoveUserAllowlist(_) => "RemoveUserAllowlist".to_string(), // variant 9

            Self::CreateLocation(_) => "CreateLocation".to_string(), // variant 10
            Self::UpdateLocation(_) => "UpdateLocation".to_string(), // variant 11
            Self::SuspendLocation(_) => "SuspendLocation".to_string(), // variant 12
            Self::ResumeLocation(_) => "ResumeLocation".to_string(), // variant 13
            Self::DeleteLocation(_) => "DeleteLocation".to_string(), // variant 14

            Self::CreateExchange(_) => "CreateExchange".to_string(), // variant 15
            Self::UpdateExchange(_) => "UpdateExchange".to_string(), // variant 16
            Self::SuspendExchange(_) => "SuspendExchange".to_string(), // variant 17
            Self::ResumeExchange(_) => "ResumeExchange".to_string(), // variant 18
            Self::DeleteExchange(_) => "DeleteExchange".to_string(), // variant 19

            Self::CreateDevice(_) => "CreateDevice".to_string(), // variant 20
            Self::ActivateDevice(_) => "ActivateDevice".to_string(), // variant 21
            Self::RejectDevice(_) => "RejectDevice".to_string(), // variant 22
            Self::UpdateDevice(_) => "UpdateDevice".to_string(), // variant 23
            Self::SuspendDevice(_) => "SuspendDevice".to_string(), // variant 24
            Self::ResumeDevice(_) => "ResumeDevice".to_string(), // variant 25
            Self::DeleteDevice(_) => "DeleteDevice".to_string(), // variant 26
            Self::CloseAccountDevice(_) => "CloseAccountDevice".to_string(), // variant 27

            Self::CreateLink(_) => "CreateLink".to_string(), // variant 28
            Self::ActivateLink(_) => "ActivateLink".to_string(), // variant 29
            Self::RejectLink(_) => "RejectLink".to_string(), // variant 30
            Self::UpdateLink(_) => "UpdateLink".to_string(), // variant 31
            Self::SuspendLink(_) => "SuspendLink".to_string(), // variant 32
            Self::ResumeLink(_) => "ResumeLink".to_string(), // variant 33
            Self::DeleteLink(_) => "DeleteLink".to_string(), // variant 34
            Self::CloseAccountLink(_) => "CloseAccountLink".to_string(), // variant 35

            Self::CreateUser(_) => "CreateUser".to_string(), // variant 36
            Self::ActivateUser(_) => "ActivateUser".to_string(), // variant 37
            Self::RejectUser(_) => "RejectUser".to_string(), // variant 38
            Self::UpdateUser(_) => "UpdateUser".to_string(), // variant 39
            Self::SuspendUser(_) => "SuspendUser".to_string(), // variant 40
            Self::ResumeUser(_) => "ResumeUser".to_string(), // variant 41
            Self::DeleteUser(_) => "DeleteUser".to_string(), // variant 42
            Self::CloseAccountUser(_) => "CloseAccountUser".to_string(), // variant 43

            Self::RequestBanUser(_) => "RequestBanUser".to_string(), // variant 44
            Self::BanUser(_) => "BanUser".to_string(), // variant 45
        }
    }

    pub fn get_args(&self) -> String {
        match self {
            Self::None() => "".to_string(), // variant 0
            Self::InitGlobalState() => "".to_string(), // variant 1
            Self::CloseAccount(args) => format!("{:?}", args), // variant 2
            Self::SetGlobalConfig(args) => format!("{:?}", args), // variant 3

            Self::AddFoundationAllowlist(args) => format!("{:?}", args), // variant 4
            Self::RemoveFoundationAllowlist(args) => format!("{:?}", args), // variant 5
            Self::AddDeviceAllowlist(args) => format!("{:?}", args), // variant 6
            Self::RemoveDeviceAllowlist(args) => format!("{:?}", args), // variant 7
            Self::AddUserAllowlist(args) => format!("{:?}", args), // variant 8
            Self::RemoveUserAllowlist(args) => format!("{:?}", args), // variant 9

            Self::CreateLocation(args) => format!("{:?}", args), // variant 10
            Self::UpdateLocation(args) => format!("{:?}", args), // variant 11
            Self::SuspendLocation(args) => format!("{:?}", args), // variant 12
            Self::ResumeLocation(args) => format!("{:?}", args), // variant 13
            Self::DeleteLocation(args) => format!("{:?}", args), // variant 14

            Self::CreateExchange(args) => format!("{:?}", args), // variant 15
            Self::UpdateExchange(args) => format!("{:?}", args), // variant 16
            Self::SuspendExchange(args) => format!("{:?}", args), // variant 17
            Self::ResumeExchange(args) => format!("{:?}", args), // variant 18
            Self::DeleteExchange(args) => format!("{:?}", args), // variant 19

            Self::CreateDevice(args) => format!("{:?}", args), // variant 20
            Self::ActivateDevice(args) => format!("{:?}", args), // variant 21
            Self::RejectDevice(args) => format!("{:?}", args), // variant 22
            Self::UpdateDevice(args) => format!("{:?}", args), // variant 23
            Self::SuspendDevice(args) => format!("{:?}", args), // variant 24
            Self::ResumeDevice(args) => format!("{:?}", args), // variant 25
            Self::DeleteDevice(args) => format!("{:?}", args), // variant 26
            Self::CloseAccountDevice(args) => format!("{:?}", args), // variant 27

            Self::CreateLink(args) => format!("{:?}", args), // variant 28
            Self::ActivateLink(args) => format!("{:?}", args), // variant 29
            Self::RejectLink(args) => format!("{:?}", args), // variant 30
            Self::UpdateLink(args) => format!("{:?}", args), // variant 31
            Self::SuspendLink(args) => format!("{:?}", args), // variant 32
            Self::ResumeLink(args) => format!("{:?}", args), // variant 33
            Self::DeleteLink(args) => format!("{:?}", args), // variant 34
            Self::CloseAccountLink(args) => format!("{:?}", args), // variant 35

            Self::CreateUser(args) => format!("{:?}", args), // variant 36
            Self::ActivateUser(args) => format!("{:?}", args), // variant 37
            Self::RejectUser(args) => format!("{:?}", args), // variant 38
            Self::UpdateUser(args) => format!("{:?}", args), // variant 39
            Self::SuspendUser(args) => format!("{:?}", args), // variant 40
            Self::ResumeUser(args) => format!("{:?}", args), // variant 41
            Self::DeleteUser(args) => format!("{:?}", args), // variant 42
            Self::CloseAccountUser(args) => format!("{:?}", args), // variant 43

            Self::RequestBanUser(args) => format!("{:?}", args), // variant 44
            Self::BanUser(args) => format!("{:?}", args), // variant 45
        }
    }
}

#[cfg(test)]
mod tests {
    use solana_program::pubkey::Pubkey;
    use crate::state::{device::DeviceType, user::{UserCYOA, UserType}, link::LinkLinkType};

    use super::*;

    fn test_instruction(instruction: DoubleZeroInstruction, expected_name: &str) {
        let unpacked = DoubleZeroInstruction::unpack(&instruction.pack()).unwrap();
        assert_eq!(instruction, unpacked, "Instruction mismatch");
        assert_eq!(
            expected_name,
            unpacked.get_name(),
            "Instruction name mismatch"
        );
    }

    #[test]
    fn test_doublezero_instruction() {
        test_instruction(DoubleZeroInstruction::InitGlobalState(), "InitGlobalState");
        test_instruction(
            DoubleZeroInstruction::SetGlobalConfig(SetGlobalConfigArgs {
                local_asn: 100,
                remote_asn: 200,
                tunnel_tunnel_block: ([1, 2, 3, 4], 1),
                user_tunnel_block: ([1, 2, 3, 4], 1),
            }),
            "SetGlobalConfig",
        );
        test_instruction(
            DoubleZeroInstruction::CreateLocation(LocationCreateArgs {
                index: 123,
                bump_seed: 255,
                lat: 1.0,
                lng: 2.0,
                loc_id: 123,
                code: "test".to_string(),
                name: "test".to_string(),
                country: "US".to_string(),
            }),
            "CreateLocation",
        );
        test_instruction(
            DoubleZeroInstruction::UpdateLocation(LocationUpdateArgs {
                index: 123,
                bump_seed: 255,
                lat: Some(1.0),
                lng: Some(2.0),
                loc_id: Some(123),
                code: Some("test".to_string()),
                name: Some("test".to_string()),
                country: Some("US".to_string()),
            }),
            "UpdateLocation",
        );
        test_instruction(
            DoubleZeroInstruction::SuspendLocation(LocationSuspendArgs {
                index: 123,
                bump_seed: 255,
            }),
            "SuspendLocation",
        );
        test_instruction(
            DoubleZeroInstruction::ResumeLocation(LocationResumeArgs {
                index: 123,
                bump_seed: 255,
            }),
            "ResumeLocation",
        );
        test_instruction(
            DoubleZeroInstruction::DeleteLocation(LocationDeleteArgs {
                index: 123,
                bump_seed: 255,
            }),
            "DeleteLocation",
        );
        test_instruction(
            DoubleZeroInstruction::CreateExchange(ExchangeCreateArgs {
                index: 123,
                bump_seed: 255,
                code: "test".to_string(),
                name: "test".to_string(),
                lat: 1.0,
                lng: 2.0,
                loc_id: 123,
            }),
            "CreateExchange",
        );
        test_instruction(
            DoubleZeroInstruction::UpdateExchange(ExchangeUpdateArgs {
                index: 123,
                bump_seed: 255,
                lat: Some(1.0),
                lng: Some(2.0),
                loc_id: Some(123),
                code: Some("test".to_string()),
                name: Some("test".to_string()),
            }),
            "UpdateExchange",
        );
        test_instruction(
            DoubleZeroInstruction::SuspendExchange(ExchangeSuspendArgs {
                index: 123,
                bump_seed: 255,
            }),
            "SuspendExchange",
        );
        test_instruction(
            DoubleZeroInstruction::ResumeExchange(ExchangeResumeArgs {
                index: 123,
                bump_seed: 255,
            }),
            "ResumeExchange",
        );
        test_instruction(
            DoubleZeroInstruction::DeleteExchange(ExchangeDeleteArgs {
                index: 123,
                bump_seed: 255,
            }),
            "DeleteExchange",
        );
        test_instruction(
            DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
                index: 123,
                bump_seed: 255,
                code: "test".to_string(),
                public_ip: [1, 2, 3, 4],
                device_type: DeviceType::Switch,
                dz_prefixes: vec![([1, 2, 3, 4], 1)],
                location_pk: Pubkey::new_unique(),
                exchange_pk: Pubkey::new_unique(),
            }),
            "CreateDevice",
        );
        test_instruction(
            DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs {
                index: 123,
                bump_seed: 255,
            }),
            "ActivateDevice",
        );
        test_instruction(
            DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
                index: 123,
                bump_seed: 255,
                code: Some("test".to_string()),
                public_ip: Some([1, 2, 3, 4]),
                device_type: Some(DeviceType::Switch),
                dz_prefixes: Some(vec![([1, 2, 3, 4], 1)]),
            }),
            "UpdateDevice",
        );
        test_instruction(
            DoubleZeroInstruction::SuspendDevice(DeviceSuspendArgs {
                index: 123,
                bump_seed: 255,
            }),
            "SuspendDevice",
        );
        test_instruction(
            DoubleZeroInstruction::ResumeDevice(DeviceResumeArgs {
                index: 123,
                bump_seed: 255,
            }),
            "ResumeDevice",
        );
        test_instruction(
            DoubleZeroInstruction::DeleteDevice(DeviceDeleteArgs {
                index: 123,
                bump_seed: 255,
            }),
            "DeleteDevice",
        );
        test_instruction(
            DoubleZeroInstruction::CreateLink(LinkCreateArgs {
                index: 123,
                bump_seed: 255,
                code: "test".to_string(),
                side_a_pk: Pubkey::new_unique(),
                side_z_pk: Pubkey::new_unique(),
                tunnel_type: LinkLinkType::L3,
                bandwidth: 100,
                mtu: 1500,
                delay_ns: 1000,
                jitter_ns: 100,
            }),
            "CreateLink",
        );
        test_instruction(
            DoubleZeroInstruction::ActivateLink(LinkActivateArgs {
                index: 123,
                bump_seed: 255,
                tunnel_id: 1,
                tunnel_net: ([1, 2, 3, 4], 1),
            }),
            "ActivateLink",
        );
        test_instruction(
            DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
                index: 123,
                bump_seed: 255,
                code: Some("test".to_string()),
                tunnel_type: Some(LinkLinkType::L3),
                bandwidth: Some(100),
                mtu: Some(1500),
                delay_ns: Some(1000),
                jitter_ns: Some(100),
            }),
            "UpdateLink",
        );
        test_instruction(
            DoubleZeroInstruction::SuspendLink(LinkSuspendArgs {
                index: 123,
                bump_seed: 255,
            }),
            "SuspendLink",
        );
        test_instruction(
            DoubleZeroInstruction::ResumeLink(LinkResumeArgs {
                index: 123,
                bump_seed: 255,
            }),
            "ResumeLink",
        );
        test_instruction(
            DoubleZeroInstruction::DeleteLink(LinkDeleteArgs {
                index: 123,
                bump_seed: 255,
            }),
            "DeleteLink",
        );
        test_instruction(
            DoubleZeroInstruction::CreateUser(UserCreateArgs {
                index: 123,
                bump_seed: 255,
                user_type: UserType::IBRL,
                device_pk: Pubkey::new_unique(),
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: [1, 2, 3, 4],
            }),
            "CreateUser",
        );
        test_instruction(
            DoubleZeroInstruction::ActivateUser(UserActivateArgs {
                index: 123,
                bump_seed: 255,
                tunnel_id: 1,
                tunnel_net: ([1, 2, 3, 4], 1),
                dz_ip: [1, 2, 3, 4],
            }),
            "ActivateUser",
        );
        test_instruction(
            DoubleZeroInstruction::UpdateUser(UserUpdateArgs {
                index: 123,
                bump_seed: 255,
                user_type: Some(UserType::IBRL),
                cyoa_type: Some(UserCYOA::GREOverDIA),
                client_ip: Some([1, 2, 3, 4]),
                dz_ip: Some([1, 2, 3, 4]),
                tunnel_id: Some(1),
                tunnel_net: Some(([1, 2, 3, 4], 1)),
            }),
            "UpdateUser",
        );
        test_instruction(
            DoubleZeroInstruction::SuspendUser(UserSuspendArgs {
                index: 123,
                bump_seed: 255,
            }),
            "SuspendUser",
        );
        test_instruction(
            DoubleZeroInstruction::ResumeUser(UserResumeArgs {
                index: 123,
                bump_seed: 255,
            }),
            "ResumeUser",
        );
        test_instruction(
            DoubleZeroInstruction::DeleteUser(UserDeleteArgs {
                index: 123,
                bump_seed: 255,
            }),
            "DeleteUser",
        );
        test_instruction(
            DoubleZeroInstruction::CloseAccountDevice(DeviceCloseAccountArgs {
                index: 123,
                bump_seed: 255,
            }),
            "CloseAccountDevice",
        );
        test_instruction(
            DoubleZeroInstruction::CloseAccountLink(LinkCloseAccountArgs {
                index: 123,
                bump_seed: 255,
            }),
            "CloseAccountLink",
        );
        test_instruction(
            DoubleZeroInstruction::CloseAccountUser(UserCloseAccountArgs {
                index: 123,
                bump_seed: 255,
            }),
            "CloseAccountUser",
        );
        test_instruction(
            DoubleZeroInstruction::RejectDevice(DeviceRejectArgs {
                index: 123,
                bump_seed: 255,
                reason: "test".to_string(),
            }),
            "RejectDevice",
        );
        test_instruction(
            DoubleZeroInstruction::RejectLink(LinkRejectArgs {
                index: 123,
                bump_seed: 255,
                reason: "test".to_string(),
            }),
            "RejectLink",
        );
        test_instruction(
            DoubleZeroInstruction::RejectUser(UserRejectArgs {
                index: 123,
                bump_seed: 255,
                reason: "test".to_string(),
            }),
            "RejectUser",
        );
        test_instruction(
            DoubleZeroInstruction::AddFoundationAllowlist(AddFoundationAllowlistArgs {
                pubkey: Pubkey::new_unique(),
            }),
            "AddFoundationAllowlist",
        );
        test_instruction(
            DoubleZeroInstruction::RemoveFoundationAllowlist(RemoveFoundationAllowlistArgs {
                pubkey: Pubkey::new_unique(),
            }),
            "RemoveFoundationAllowlist",
        );
        test_instruction(
            DoubleZeroInstruction::AddDeviceAllowlist(AddDeviceAllowlistArgs {
                pubkey: Pubkey::new_unique(),
            }),
            "AddDeviceAllowlist",
        );
        test_instruction(
            DoubleZeroInstruction::RemoveDeviceAllowlist(RemoveDeviceAllowlistArgs {
                pubkey: Pubkey::new_unique(),
            }),
            "RemoveDeviceAllowlist",
        );
        test_instruction(
            DoubleZeroInstruction::AddUserAllowlist(AddUserAllowlistArgs {
                pubkey: Pubkey::new_unique(),
            }),
            "AddUserAllowlist",
        );
        test_instruction(
            DoubleZeroInstruction::RemoveUserAllowlist(RemoveUserAllowlistArgs {
                pubkey: Pubkey::new_unique(),
            }),
            "RemoveUserAllowlist",
        );
        test_instruction(
            DoubleZeroInstruction::RequestBanUser(UserRequestBanArgs {
                index: 123,
                bump_seed: 255,
            }),
            "RequestBanUser",
        );
        test_instruction(
            DoubleZeroInstruction::BanUser(UserBanArgs {
                index: 123,
                bump_seed: 255,
            }),
            "BanUser",
        );
    }
}
