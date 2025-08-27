use crate::processors::{
    accesspass::set::SetAccessPassArgs,
    allowlist::{
        device::{add::AddDeviceAllowlistArgs, remove::RemoveDeviceAllowlistArgs},
        foundation::{add::AddFoundationAllowlistArgs, remove::RemoveFoundationAllowlistArgs},
        user::{add::AddUserAllowlistArgs, remove::RemoveUserAllowlistArgs},
    },
    contributor::{
        create::ContributorCreateArgs, delete::ContributorDeleteArgs,
        resume::ContributorResumeArgs, suspend::ContributorSuspendArgs,
        update::ContributorUpdateArgs,
    },
    device::{
        activate::DeviceActivateArgs, closeaccount::DeviceCloseAccountArgs,
        create::DeviceCreateArgs, delete::DeviceDeleteArgs, reject::DeviceRejectArgs,
        resume::DeviceResumeArgs, suspend::DeviceSuspendArgs, update::DeviceUpdateArgs,
    },
    exchange::{
        create::ExchangeCreateArgs, delete::ExchangeDeleteArgs, resume::ExchangeResumeArgs,
        setdevice::ExchangeSetDeviceArgs, suspend::ExchangeSuspendArgs, update::ExchangeUpdateArgs,
    },
    globalconfig::set::SetGlobalConfigArgs,
    globalstate::{setairdrop::SetAirdropArgs, setauthority::SetAuthorityArgs},
    link::{
        accept::LinkAcceptArgs, activate::LinkActivateArgs, closeaccount::LinkCloseAccountArgs,
        create::LinkCreateArgs, delete::LinkDeleteArgs, reject::LinkRejectArgs,
        resume::LinkResumeArgs, suspend::LinkSuspendArgs, update::LinkUpdateArgs,
    },
    location::{
        create::LocationCreateArgs, delete::LocationDeleteArgs, resume::LocationResumeArgs,
        suspend::LocationSuspendArgs, update::LocationUpdateArgs,
    },
    multicastgroup::{
        activate::MulticastGroupActivateArgs,
        allowlist::{
            publisher::{
                add::AddMulticastGroupPubAllowlistArgs,
                remove::RemoveMulticastGroupPubAllowlistArgs,
            },
            subscriber::{
                add::AddMulticastGroupSubAllowlistArgs,
                remove::RemoveMulticastGroupSubAllowlistArgs,
            },
        },
        closeaccount::MulticastGroupDeactivateArgs,
        create::MulticastGroupCreateArgs,
        delete::MulticastGroupDeleteArgs,
        reactivate::MulticastGroupReactivateArgs,
        reject::MulticastGroupRejectArgs,
        subscribe::MulticastGroupSubscribeArgs,
        suspend::MulticastGroupSuspendArgs,
        update::MulticastGroupUpdateArgs,
    },
    user::{
        activate::UserActivateArgs, ban::UserBanArgs, closeaccount::UserCloseAccountArgs,
        create::UserCreateArgs, create_subscribe::UserCreateSubscribeArgs, delete::UserDeleteArgs,
        reject::UserRejectArgs, requestban::UserRequestBanArgs, resume::UserResumeArgs,
        suspend::UserSuspendArgs, update::UserUpdateArgs,
    },
};
use borsh::{from_slice, BorshDeserialize, BorshSerialize};
use solana_program::program_error::ProgramError;
use std::cmp::PartialEq;

// Instructions that our program can execute
#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone)]
pub enum DoubleZeroInstruction {
    None(),                               // variant 0
    InitGlobalState(),                    // variant 1
    SetAuthority(SetAuthorityArgs),       // variant 2
    SetGlobalConfig(SetGlobalConfigArgs), // variant 3

    AddFoundationAllowlist(AddFoundationAllowlistArgs), // variant 4
    RemoveFoundationAllowlist(RemoveFoundationAllowlistArgs), // variant 5
    AddDeviceAllowlist(AddDeviceAllowlistArgs),         // variant 6
    RemoveDeviceAllowlist(RemoveDeviceAllowlistArgs),   // variant 7
    AddUserAllowlist(AddUserAllowlistArgs),             // variant 8
    RemoveUserAllowlist(RemoveUserAllowlistArgs),       // variant 9

    CreateLocation(LocationCreateArgs),   // variant 10
    UpdateLocation(LocationUpdateArgs),   // variant 11
    SuspendLocation(LocationSuspendArgs), // variant 12
    ResumeLocation(LocationResumeArgs),   // variant 13
    DeleteLocation(LocationDeleteArgs),   // variant 14

    CreateExchange(ExchangeCreateArgs),   // variant 15
    UpdateExchange(ExchangeUpdateArgs),   // variant 16
    SuspendExchange(ExchangeSuspendArgs), // variant 17
    ResumeExchange(ExchangeResumeArgs),   // variant 18
    DeleteExchange(ExchangeDeleteArgs),   // variant 19

    CreateDevice(DeviceCreateArgs),             // variant 20
    ActivateDevice(DeviceActivateArgs),         // variant 21
    RejectDevice(DeviceRejectArgs),             // variant 22
    UpdateDevice(DeviceUpdateArgs),             // variant 23
    SuspendDevice(DeviceSuspendArgs),           // variant 24
    ResumeDevice(DeviceResumeArgs),             // variant 25
    DeleteDevice(DeviceDeleteArgs),             // variant 26
    CloseAccountDevice(DeviceCloseAccountArgs), // variant 27

    CreateLink(LinkCreateArgs),             // variant 28
    ActivateLink(LinkActivateArgs),         // variant 29
    RejectLink(LinkRejectArgs),             // variant 30
    UpdateLink(LinkUpdateArgs),             // variant 31
    SuspendLink(LinkSuspendArgs),           // variant 32
    ResumeLink(LinkResumeArgs),             // variant 33
    DeleteLink(LinkDeleteArgs),             // variant 34
    CloseAccountLink(LinkCloseAccountArgs), // variant 35

    CreateUser(UserCreateArgs),             // variant 36
    ActivateUser(UserActivateArgs),         // variant 37
    RejectUser(UserRejectArgs),             // variant 38
    UpdateUser(UserUpdateArgs),             // variant 39
    SuspendUser(UserSuspendArgs),           // variant 40
    ResumeUser(UserResumeArgs),             // variant 41
    DeleteUser(UserDeleteArgs),             // variant 42
    CloseAccountUser(UserCloseAccountArgs), // variant 43
    RequestBanUser(UserRequestBanArgs),     // variant 44
    BanUser(UserBanArgs),                   // variant 45

    CreateMulticastGroup(MulticastGroupCreateArgs), // variant 46
    ActivateMulticastGroup(MulticastGroupActivateArgs), // variant 47
    RejectMulticastGroup(MulticastGroupRejectArgs), // variant 48
    UpdateMulticastGroup(MulticastGroupUpdateArgs), // variant 49
    SuspendMulticastGroup(MulticastGroupSuspendArgs), // variant 50
    ReactivateMulticastGroup(MulticastGroupReactivateArgs), // variant 51
    DeleteMulticastGroup(MulticastGroupDeleteArgs), // variant 52
    DeactivateMulticastGroup(MulticastGroupDeactivateArgs), // variant 53

    AddMulticastGroupPubAllowlist(AddMulticastGroupPubAllowlistArgs), // variant 54
    RemoveMulticastGroupPubAllowlist(RemoveMulticastGroupPubAllowlistArgs), // variant 55
    AddMulticastGroupSubAllowlist(AddMulticastGroupSubAllowlistArgs), // variant 56
    RemoveMulticastGroupSubAllowlist(RemoveMulticastGroupSubAllowlistArgs), // variant 57

    SubscribeMulticastGroup(MulticastGroupSubscribeArgs), // variant 58
    CreateSubscribeUser(UserCreateSubscribeArgs),         // variant 59

    CreateContributor(ContributorCreateArgs),   // variant 60
    UpdateContributor(ContributorUpdateArgs),   // variant 61
    SuspendContributor(ContributorSuspendArgs), // variant 62
    ResumeContributor(ContributorResumeArgs),   // variant 63
    DeleteContributor(ContributorDeleteArgs),   // variant 64

    SetDeviceExchange(ExchangeSetDeviceArgs), // variant 65
    AcceptLink(LinkAcceptArgs),               // variant 66
    SetAccessPass(SetAccessPassArgs),         // variant 67
    SetAirdrop(SetAirdropArgs),               // variant 68
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
            2 => Ok(Self::SetAuthority(from_slice::<SetAuthorityArgs>(rest).unwrap())),
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


            46 => Ok(Self::CreateMulticastGroup(from_slice::<MulticastGroupCreateArgs>(rest).unwrap())),
            47 => Ok(Self::ActivateMulticastGroup(from_slice::<MulticastGroupActivateArgs>(rest).unwrap())),
            48 => Ok(Self::RejectMulticastGroup(from_slice::<MulticastGroupRejectArgs>(rest).unwrap())),
            49 => Ok(Self::UpdateMulticastGroup(from_slice::<MulticastGroupUpdateArgs>(rest).unwrap())),
            50 => Ok(Self::SuspendMulticastGroup(from_slice::<MulticastGroupSuspendArgs>(rest).unwrap())),
            51 => Ok(Self::ReactivateMulticastGroup(from_slice::<MulticastGroupReactivateArgs>(rest).unwrap())),
            52 => Ok(Self::DeleteMulticastGroup(from_slice::<MulticastGroupDeleteArgs>(rest).unwrap())),
            53 => Ok(Self::DeactivateMulticastGroup(from_slice::<MulticastGroupDeactivateArgs>(rest).unwrap())),

            54 => Ok(Self::AddMulticastGroupPubAllowlist(from_slice::<AddMulticastGroupPubAllowlistArgs>(rest).unwrap())),
            55 => Ok(Self::RemoveMulticastGroupPubAllowlist(from_slice::<RemoveMulticastGroupPubAllowlistArgs>(rest).unwrap())),
            56 => Ok(Self::AddMulticastGroupSubAllowlist(from_slice::<AddMulticastGroupSubAllowlistArgs>(rest).unwrap())),
            57 => Ok(Self::RemoveMulticastGroupSubAllowlist(from_slice::<RemoveMulticastGroupSubAllowlistArgs>(rest).unwrap())),
            58 => Ok(Self::SubscribeMulticastGroup(from_slice::<MulticastGroupSubscribeArgs>(rest).unwrap())),
            59 => Ok(Self::CreateSubscribeUser(from_slice::<UserCreateSubscribeArgs>(rest).unwrap())),

            60 => Ok(Self::CreateContributor(from_slice::<ContributorCreateArgs>(rest).unwrap())),
            61 => Ok(Self::UpdateContributor(from_slice::<ContributorUpdateArgs>(rest).unwrap())),
            62 => Ok(Self::SuspendContributor(from_slice::<ContributorSuspendArgs>(rest).unwrap())),
            63 => Ok(Self::ResumeContributor(from_slice::<ContributorResumeArgs>(rest).unwrap())),
            64 => Ok(Self::DeleteContributor(from_slice::<ContributorDeleteArgs>(rest).unwrap())),

            65 => Ok(Self::SetDeviceExchange(from_slice::<ExchangeSetDeviceArgs>(rest).unwrap())),
            66 => Ok(Self::AcceptLink(from_slice::<LinkAcceptArgs>(rest).unwrap())),
            67 => Ok(Self::SetAccessPass(from_slice::<SetAccessPassArgs>(rest).unwrap())),
            68 => Ok(Self::SetAirdrop(from_slice::<SetAirdropArgs>(rest).unwrap())),

            _ => Err(ProgramError::InvalidInstructionData),
        }
    }

    pub fn get_name(&self) -> String {
        match self {
            Self::None() => "None".to_string(), // variant 0
            Self::InitGlobalState() => "InitGlobalState".to_string(), // variant 1
            Self::SetAuthority(_) => "SetAuthority".to_string(), // variant 2
            Self::SetGlobalConfig(_) => "SetGlobalConfig".to_string(), // variant 3

            Self::AddFoundationAllowlist(_) => "AddFoundationAllowlist".to_string(), // variant 4
            Self::RemoveFoundationAllowlist(_) => "RemoveFoundationAllowlist".to_string(), // variant 5
            Self::AddDeviceAllowlist(_) => "AddDeviceAllowlist".to_string(), // variant 6
            Self::RemoveDeviceAllowlist(_) => "RemoveDeviceAllowlist".to_string(), // variant 7
            Self::AddUserAllowlist(_) => "AddUserAllowlist".to_string(),     // variant 8
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
            Self::BanUser(_) => "BanUser".to_string(),               // variant 45

            Self::CreateMulticastGroup(_) => "CreateMulticastGroup".to_string(), // variant 46
            Self::ActivateMulticastGroup(_) => "ActivateMulticastGroup".to_string(), // variant 47
            Self::RejectMulticastGroup(_) => "RejectMulticastGroup".to_string(), // variant 48
            Self::SuspendMulticastGroup(_) => "SuspendMulticastGroup".to_string(), // variant 49
            Self::ReactivateMulticastGroup(_) => "ReactivateMulticastGroup".to_string(), // variant 50
            Self::DeleteMulticastGroup(_) => "DeleteMulticastGroup".to_string(), // variant 51
            Self::UpdateMulticastGroup(_) => "UpdateMulticastGroup".to_string(), // variant 52
            Self::DeactivateMulticastGroup(_) => "DeactivateMulticastGroup".to_string(), // variant 53

            Self::AddMulticastGroupPubAllowlist(_) => "AddMulticastGroupPubAllowlist".to_string(), // variant 54
            Self::RemoveMulticastGroupPubAllowlist(_) => {
                "RemoveMulticastGroupPubAllowlist".to_string()
            } // variant 55
            Self::AddMulticastGroupSubAllowlist(_) => "AddMulticastGroupSubAllowlist".to_string(), // variant 56
            Self::RemoveMulticastGroupSubAllowlist(_) => {
                "RemoveMulticastGroupSubAllowlist".to_string()
            } // variant 57

            Self::SubscribeMulticastGroup(_) => "SubscribeMulticastGroup".to_string(), // variant 58
            Self::CreateSubscribeUser(_) => "CreateSubscribeUser".to_string(),         // variant 59

            Self::CreateContributor(_) => "CreateContributor".to_string(), // variant 60
            Self::UpdateContributor(_) => "UpdateContributor".to_string(), // variant 61
            Self::SuspendContributor(_) => "SuspendContributor".to_string(), // variant 62
            Self::ResumeContributor(_) => "ResumeContributor".to_string(), // variant 63
            Self::DeleteContributor(_) => "DeleteContributor".to_string(), // variant 64

            Self::SetDeviceExchange(_) => "SetDeviceExchange".to_string(), // variant 65
            Self::AcceptLink(_) => "AcceptLink".to_string(),               // variant 66
            Self::SetAccessPass(_) => "SetAccessPass".to_string(),         // variant 67
            Self::SetAirdrop(_) => "SetAirdrop".to_string(),               // variant 68
        }
    }

    pub fn get_args(&self) -> String {
        match self {
            Self::None() => "".to_string(),                     // variant 0
            Self::InitGlobalState() => "".to_string(),          // variant 1
            Self::SetAuthority(args) => format!("{args:?}"),    // variant 2
            Self::SetGlobalConfig(args) => format!("{args:?}"), // variant 3

            Self::AddFoundationAllowlist(args) => format!("{args:?}"), // variant 4
            Self::RemoveFoundationAllowlist(args) => format!("{args:?}"), // variant 5
            Self::AddDeviceAllowlist(args) => format!("{args:?}"),     // variant 6
            Self::RemoveDeviceAllowlist(args) => format!("{args:?}"),  // variant 7
            Self::AddUserAllowlist(args) => format!("{args:?}"),       // variant 8
            Self::RemoveUserAllowlist(args) => format!("{args:?}"),    // variant 9

            Self::CreateLocation(args) => format!("{args:?}"), // variant 10
            Self::UpdateLocation(args) => format!("{args:?}"), // variant 11
            Self::SuspendLocation(args) => format!("{args:?}"), // variant 12
            Self::ResumeLocation(args) => format!("{args:?}"), // variant 13
            Self::DeleteLocation(args) => format!("{args:?}"), // variant 14

            Self::CreateExchange(args) => format!("{args:?}"), // variant 15
            Self::UpdateExchange(args) => format!("{args:?}"), // variant 16
            Self::SuspendExchange(args) => format!("{args:?}"), // variant 17
            Self::ResumeExchange(args) => format!("{args:?}"), // variant 18
            Self::DeleteExchange(args) => format!("{args:?}"), // variant 19

            Self::CreateDevice(args) => format!("{args:?}"), // variant 20
            Self::ActivateDevice(args) => format!("{args:?}"), // variant 21
            Self::RejectDevice(args) => format!("{args:?}"), // variant 22
            Self::UpdateDevice(args) => format!("{args:?}"), // variant 23
            Self::SuspendDevice(args) => format!("{args:?}"), // variant 24
            Self::ResumeDevice(args) => format!("{args:?}"), // variant 25
            Self::DeleteDevice(args) => format!("{args:?}"), // variant 26
            Self::CloseAccountDevice(args) => format!("{args:?}"), // variant 27

            Self::CreateLink(args) => format!("{args:?}"), // variant 28
            Self::ActivateLink(args) => format!("{args:?}"), // variant 29
            Self::RejectLink(args) => format!("{args:?}"), // variant 30
            Self::UpdateLink(args) => format!("{args:?}"), // variant 31
            Self::SuspendLink(args) => format!("{args:?}"), // variant 32
            Self::ResumeLink(args) => format!("{args:?}"), // variant 33
            Self::DeleteLink(args) => format!("{args:?}"), // variant 34
            Self::CloseAccountLink(args) => format!("{args:?}"), // variant 35

            Self::CreateUser(args) => format!("{args:?}"), // variant 36
            Self::ActivateUser(args) => format!("{args:?}"), // variant 37
            Self::RejectUser(args) => format!("{args:?}"), // variant 38
            Self::UpdateUser(args) => format!("{args:?}"), // variant 39
            Self::SuspendUser(args) => format!("{args:?}"), // variant 40
            Self::ResumeUser(args) => format!("{args:?}"), // variant 41
            Self::DeleteUser(args) => format!("{args:?}"), // variant 42
            Self::CloseAccountUser(args) => format!("{args:?}"), // variant 43

            Self::RequestBanUser(args) => format!("{args:?}"), // variant 44
            Self::BanUser(args) => format!("{args:?}"),        // variant 45

            Self::CreateMulticastGroup(args) => format!("{args:?}"), // variant 46
            Self::ActivateMulticastGroup(args) => format!("{args:?}"), // variant 47
            Self::RejectMulticastGroup(args) => format!("{args:?}"), // variant 48
            Self::SuspendMulticastGroup(args) => format!("{args:?}"), // variant 49
            Self::ReactivateMulticastGroup(args) => format!("{args:?}"), // variant 50
            Self::DeleteMulticastGroup(args) => format!("{args:?}"), // variant 51
            Self::UpdateMulticastGroup(args) => format!("{args:?}"), // variant 52
            Self::DeactivateMulticastGroup(args) => format!("{args:?}"), // variant 53
            Self::SubscribeMulticastGroup(args) => format!("{args:?}"), // variant 54
            Self::AddMulticastGroupPubAllowlist(args) => format!("{args:?}"), // variant 55
            Self::RemoveMulticastGroupPubAllowlist(args) => format!("{args:?}"), // variant 56
            Self::AddMulticastGroupSubAllowlist(args) => format!("{args:?}"), // variant 57
            Self::RemoveMulticastGroupSubAllowlist(args) => format!("{args:?}"), // variant 58
            Self::CreateSubscribeUser(args) => format!("{args:?}"),  // variant 59

            Self::CreateContributor(args) => format!("{args:?}"), // variant 60
            Self::UpdateContributor(args) => format!("{args:?}"), // variant 61
            Self::SuspendContributor(args) => format!("{args:?}"), // variant 62
            Self::ResumeContributor(args) => format!("{args:?}"), // variant 63
            Self::DeleteContributor(args) => format!("{args:?}"), // variant 64

            Self::SetDeviceExchange(args) => format!("{args:?}"), // variant 65
            Self::AcceptLink(args) => format!("{args:?}"),        // variant 66
            Self::SetAccessPass(args) => format!("{args:?}"),     // variant 67
            Self::SetAirdrop(args) => format!("{args:?}"),        // variant 68
        }
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        processors::exchange::setdevice::SetDeviceOption,
        state::{
            device::DeviceType,
            link::LinkLinkType,
            user::{UserCYOA, UserType},
        },
    };
    use solana_program::pubkey::Pubkey;

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
                device_tunnel_block: "1.2.3.4/1".parse().unwrap(),
                user_tunnel_block: "1.2.3.4/1".parse().unwrap(),
                multicastgroup_block: "1.2.3.4/1".parse().unwrap(),
            }),
            "SetGlobalConfig",
        );
        test_instruction(
            DoubleZeroInstruction::CreateLocation(LocationCreateArgs {
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
            DoubleZeroInstruction::SuspendLocation(LocationSuspendArgs),
            "SuspendLocation",
        );
        test_instruction(
            DoubleZeroInstruction::ResumeLocation(LocationResumeArgs),
            "ResumeLocation",
        );
        test_instruction(
            DoubleZeroInstruction::DeleteLocation(LocationDeleteArgs),
            "DeleteLocation",
        );
        test_instruction(
            DoubleZeroInstruction::CreateExchange(ExchangeCreateArgs {
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
                lat: Some(1.0),
                lng: Some(2.0),
                loc_id: Some(123),
                code: Some("test".to_string()),
                name: Some("test".to_string()),
            }),
            "UpdateExchange",
        );
        test_instruction(
            DoubleZeroInstruction::SuspendExchange(ExchangeSuspendArgs {}),
            "SuspendExchange",
        );
        test_instruction(
            DoubleZeroInstruction::ResumeExchange(ExchangeResumeArgs {}),
            "ResumeExchange",
        );
        test_instruction(
            DoubleZeroInstruction::DeleteExchange(ExchangeDeleteArgs {}),
            "DeleteExchange",
        );
        test_instruction(
            DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
                code: "test".to_string(),
                public_ip: [1, 2, 3, 4].into(),
                device_type: DeviceType::Switch,
                dz_prefixes: "1.2.3.4/1".parse().unwrap(),
                metrics_publisher_pk: Pubkey::new_unique(),
                mgmt_vrf: "mgmt".to_string(),
                interfaces: vec![],
            }),
            "CreateDevice",
        );
        test_instruction(
            DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs),
            "ActivateDevice",
        );
        test_instruction(
            DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
                code: Some("test".to_string()),
                public_ip: Some([1, 2, 3, 4].into()),
                contributor_pk: Some(Pubkey::new_unique()),
                device_type: Some(DeviceType::Switch),
                dz_prefixes: Some("1.2.3.4/1".parse().unwrap()),
                metrics_publisher_pk: Some(Pubkey::new_unique()),
                mgmt_vrf: Some("mgmt".to_string()),
                interfaces: None,
                max_users: None,
            }),
            "UpdateDevice",
        );
        test_instruction(
            DoubleZeroInstruction::SuspendDevice(DeviceSuspendArgs),
            "SuspendDevice",
        );
        test_instruction(
            DoubleZeroInstruction::ResumeDevice(DeviceResumeArgs),
            "ResumeDevice",
        );
        test_instruction(
            DoubleZeroInstruction::DeleteDevice(DeviceDeleteArgs),
            "DeleteDevice",
        );
        test_instruction(
            DoubleZeroInstruction::CreateLink(LinkCreateArgs {
                code: "test".to_string(),
                link_type: LinkLinkType::WAN,
                bandwidth: 100,
                mtu: 1500,
                delay_ns: 1000,
                jitter_ns: 100,
                side_a_iface_name: "eth0".to_string(),
                side_z_iface_name: Some("eth1".to_string()),
            }),
            "CreateLink",
        );
        test_instruction(
            DoubleZeroInstruction::ActivateLink(LinkActivateArgs {
                tunnel_id: 1,
                tunnel_net: "1.2.3.4/1".parse().unwrap(),
            }),
            "ActivateLink",
        );
        test_instruction(
            DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
                code: Some("test".to_string()),
                contributor_pk: Some(Pubkey::new_unique()),
                tunnel_type: Some(LinkLinkType::WAN),
                bandwidth: Some(100),
                mtu: Some(1500),
                delay_ns: Some(1000),
                jitter_ns: Some(100),
            }),
            "UpdateLink",
        );
        test_instruction(
            DoubleZeroInstruction::SuspendLink(LinkSuspendArgs {}),
            "SuspendLink",
        );
        test_instruction(
            DoubleZeroInstruction::ResumeLink(LinkResumeArgs {}),
            "ResumeLink",
        );
        test_instruction(
            DoubleZeroInstruction::DeleteLink(LinkDeleteArgs {}),
            "DeleteLink",
        );
        test_instruction(
            DoubleZeroInstruction::CreateUser(UserCreateArgs {
                user_type: UserType::IBRL,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: [1, 2, 3, 4].into(),
            }),
            "CreateUser",
        );
        test_instruction(
            DoubleZeroInstruction::ActivateUser(UserActivateArgs {
                tunnel_id: 1,
                tunnel_net: "1.2.3.4/1".parse().unwrap(),
                dz_ip: [1, 2, 3, 4].into(),
            }),
            "ActivateUser",
        );
        test_instruction(
            DoubleZeroInstruction::UpdateUser(UserUpdateArgs {
                user_type: Some(UserType::IBRL),
                cyoa_type: Some(UserCYOA::GREOverDIA),
                client_ip: Some([1, 2, 3, 4].into()),
                dz_ip: Some([1, 2, 3, 4].into()),
                tunnel_id: Some(1),
                tunnel_net: Some("1.2.3.4/1".parse().unwrap()),
            }),
            "UpdateUser",
        );
        test_instruction(
            DoubleZeroInstruction::SuspendUser(UserSuspendArgs {}),
            "SuspendUser",
        );
        test_instruction(
            DoubleZeroInstruction::ResumeUser(UserResumeArgs {}),
            "ResumeUser",
        );
        test_instruction(
            DoubleZeroInstruction::DeleteUser(UserDeleteArgs {}),
            "DeleteUser",
        );
        test_instruction(
            DoubleZeroInstruction::CloseAccountDevice(DeviceCloseAccountArgs {}),
            "CloseAccountDevice",
        );
        test_instruction(
            DoubleZeroInstruction::CloseAccountLink(LinkCloseAccountArgs {}),
            "CloseAccountLink",
        );
        test_instruction(
            DoubleZeroInstruction::CloseAccountUser(UserCloseAccountArgs {}),
            "CloseAccountUser",
        );
        test_instruction(
            DoubleZeroInstruction::RejectDevice(DeviceRejectArgs {
                reason: "test".to_string(),
            }),
            "RejectDevice",
        );
        test_instruction(
            DoubleZeroInstruction::RejectLink(LinkRejectArgs {
                reason: "test".to_string(),
            }),
            "RejectLink",
        );
        test_instruction(
            DoubleZeroInstruction::RejectUser(UserRejectArgs {
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
            DoubleZeroInstruction::RequestBanUser(UserRequestBanArgs {}),
            "RequestBanUser",
        );
        test_instruction(DoubleZeroInstruction::BanUser(UserBanArgs {}), "BanUser");

        test_instruction(
            DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
                index: 123,
                bump_seed: 255,
                max_bandwidth: 1000,
                code: "test".to_string(),
                owner: Pubkey::new_unique(),
            }),
            "CreateMulticastGroup",
        );

        test_instruction(
            DoubleZeroInstruction::ActivateMulticastGroup(MulticastGroupActivateArgs {
                multicast_ip: [1, 2, 3, 4].into(),
            }),
            "ActivateMulticastGroup",
        );

        test_instruction(
            DoubleZeroInstruction::RejectMulticastGroup(MulticastGroupRejectArgs {
                reason: "test".to_string(),
            }),
            "RejectMulticastGroup",
        );

        test_instruction(
            DoubleZeroInstruction::UpdateMulticastGroup(MulticastGroupUpdateArgs {
                multicast_ip: Some([1, 2, 3, 4].into()),
                max_bandwidth: Some(1000),
                code: Some("test".to_string()),
            }),
            "UpdateMulticastGroup",
        );

        test_instruction(
            DoubleZeroInstruction::SuspendMulticastGroup(MulticastGroupSuspendArgs {}),
            "SuspendMulticastGroup",
        );

        test_instruction(
            DoubleZeroInstruction::ReactivateMulticastGroup(MulticastGroupReactivateArgs {}),
            "ReactivateMulticastGroup",
        );

        test_instruction(
            DoubleZeroInstruction::DeleteMulticastGroup(MulticastGroupDeleteArgs {}),
            "DeleteMulticastGroup",
        );

        test_instruction(
            DoubleZeroInstruction::DeactivateMulticastGroup(MulticastGroupDeactivateArgs {}),
            "DeactivateMulticastGroup",
        );

        test_instruction(
            DoubleZeroInstruction::AddMulticastGroupPubAllowlist(
                AddMulticastGroupPubAllowlistArgs {
                    pubkey: Pubkey::new_unique(),
                },
            ),
            "AddMulticastGroupPubAllowlist",
        );
        test_instruction(
            DoubleZeroInstruction::RemoveMulticastGroupPubAllowlist(
                RemoveMulticastGroupPubAllowlistArgs {
                    pubkey: Pubkey::new_unique(),
                },
            ),
            "RemoveMulticastGroupPubAllowlist",
        );
        test_instruction(
            DoubleZeroInstruction::AddMulticastGroupSubAllowlist(
                AddMulticastGroupSubAllowlistArgs {
                    pubkey: Pubkey::new_unique(),
                },
            ),
            "AddMulticastGroupSubAllowlist",
        );
        test_instruction(
            DoubleZeroInstruction::RemoveMulticastGroupSubAllowlist(
                RemoveMulticastGroupSubAllowlistArgs {
                    pubkey: Pubkey::new_unique(),
                },
            ),
            "RemoveMulticastGroupSubAllowlist",
        );
        test_instruction(
            DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
                publisher: false,
                subscriber: true,
            }),
            "SubscribeMulticastGroup",
        );
        test_instruction(
            DoubleZeroInstruction::CreateSubscribeUser(UserCreateSubscribeArgs {
                user_type: UserType::IBRL,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: [1, 2, 3, 4].into(),
                publisher: false,
                subscriber: true,
            }),
            "CreateSubscribeUser",
        );
        test_instruction(
            DoubleZeroInstruction::CreateContributor(ContributorCreateArgs {
                code: "test".to_string(),
            }),
            "CreateContributor",
        );
        test_instruction(
            DoubleZeroInstruction::UpdateContributor(ContributorUpdateArgs {
                code: Some("test".to_string()),
                owner: Some(Pubkey::new_unique()),
            }),
            "UpdateContributor",
        );
        test_instruction(
            DoubleZeroInstruction::SuspendContributor(ContributorSuspendArgs {}),
            "SuspendContributor",
        );
        test_instruction(
            DoubleZeroInstruction::ResumeContributor(ContributorResumeArgs {}),
            "ResumeContributor",
        );
        test_instruction(
            DoubleZeroInstruction::DeleteContributor(ContributorDeleteArgs {}),
            "DeleteContributor",
        );
        test_instruction(
            DoubleZeroInstruction::AcceptLink(LinkAcceptArgs {
                side_z_iface_name: "AcceptLink".to_string(),
            }),
            "AcceptLink",
        );
        test_instruction(
            DoubleZeroInstruction::SetDeviceExchange(ExchangeSetDeviceArgs {
                index: 1,
                set: SetDeviceOption::Set,
            }),
            "SetDeviceExchange",
        );
        test_instruction(
            DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
                accesspass_type: crate::state::accesspass::AccessPassType::SolanaValidator(
                    Pubkey::new_unique(),
                ),
                client_ip: [1, 2, 3, 4].into(),
                last_access_epoch: 123,
            }),
            "SetAccessPass",
        );
    }
}
