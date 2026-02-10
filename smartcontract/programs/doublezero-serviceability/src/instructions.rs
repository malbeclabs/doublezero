use crate::processors::{
    accesspass::{
        check_status::CheckStatusAccessPassArgs, close::CloseAccessPassArgs, set::SetAccessPassArgs,
    },
    allowlist::{
        foundation::{add::AddFoundationAllowlistArgs, remove::RemoveFoundationAllowlistArgs},
        qa::{add::AddQaAllowlistArgs, remove::RemoveQaAllowlistArgs},
    },
    contributor::{
        create::ContributorCreateArgs, delete::ContributorDeleteArgs,
        resume::ContributorResumeArgs, suspend::ContributorSuspendArgs,
        update::ContributorUpdateArgs,
    },
    device::{
        activate::DeviceActivateArgs,
        closeaccount::DeviceCloseAccountArgs,
        create::DeviceCreateArgs,
        delete::DeviceDeleteArgs,
        interface::{
            activate::DeviceInterfaceActivateArgs, create::DeviceInterfaceCreateArgs,
            delete::DeviceInterfaceDeleteArgs, reject::DeviceInterfaceRejectArgs,
            remove::DeviceInterfaceRemoveArgs, unlink::DeviceInterfaceUnlinkArgs,
            update::DeviceInterfaceUpdateArgs,
        },
        reject::DeviceRejectArgs,
        sethealth::DeviceSetHealthArgs,
        update::DeviceUpdateArgs,
    },
    exchange::{
        create::ExchangeCreateArgs, delete::ExchangeDeleteArgs, resume::ExchangeResumeArgs,
        setdevice::ExchangeSetDeviceArgs, suspend::ExchangeSuspendArgs, update::ExchangeUpdateArgs,
    },
    globalconfig::set::SetGlobalConfigArgs,
    globalstate::{
        setairdrop::SetAirdropArgs, setauthority::SetAuthorityArgs, setversion::SetVersionArgs,
    },
    link::{
        accept::LinkAcceptArgs, activate::LinkActivateArgs, closeaccount::LinkCloseAccountArgs,
        create::LinkCreateArgs, delete::LinkDeleteArgs, reject::LinkRejectArgs,
        sethealth::LinkSetHealthArgs, update::LinkUpdateArgs,
    },
    location::{
        create::LocationCreateArgs, delete::LocationDeleteArgs, resume::LocationResumeArgs,
        suspend::LocationSuspendArgs, update::LocationUpdateArgs,
    },
    migrate::MigrateArgs,
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
    resource::{
        allocate::ResourceAllocateArgs, closeaccount::ResourceExtensionCloseAccountArgs,
        create::ResourceCreateArgs, deallocate::ResourceDeallocateArgs,
    },
    tenant::{
        add_administrator::TenantAddAdministratorArgs, create::TenantCreateArgs,
        delete::TenantDeleteArgs, remove_administrator::TenantRemoveAdministratorArgs,
        update::TenantUpdateArgs, update_payment_status::UpdatePaymentStatusArgs,
    },
    user::{
        activate::UserActivateArgs, ban::UserBanArgs, check_access_pass::CheckUserAccessPassArgs,
        closeaccount::UserCloseAccountArgs, create::UserCreateArgs,
        create_subscribe::UserCreateSubscribeArgs, delete::UserDeleteArgs, reject::UserRejectArgs,
        requestban::UserRequestBanArgs, update::UserUpdateArgs,
    },
};
use borsh::BorshSerialize;
use solana_program::program_error::ProgramError;
use std::cmp::PartialEq;

// Instructions that our program can execute
#[derive(BorshSerialize, Debug, PartialEq, Clone)]
pub enum DoubleZeroInstruction {
    Migrate(MigrateArgs),                 // variant 0
    InitGlobalState(),                    // variant 1
    SetAuthority(SetAuthorityArgs),       // variant 2
    SetGlobalConfig(SetGlobalConfigArgs), // variant 3

    AddFoundationAllowlist(AddFoundationAllowlistArgs), // variant 4
    RemoveFoundationAllowlist(RemoveFoundationAllowlistArgs), // variant 5
    AddDeviceAllowlist(),                               // variant 6
    RemoveDeviceAllowlist(),                            // variant 7
    AddUserAllowlist(),                                 // variant 8
    RemoveUserAllowlist(),                              // variant 9

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
    SuspendDevice(),                            // variant 24
    ResumeDevice(),                             // variant 25
    DeleteDevice(DeviceDeleteArgs),             // variant 26
    CloseAccountDevice(DeviceCloseAccountArgs), // variant 27

    CreateLink(LinkCreateArgs),             // variant 28
    ActivateLink(LinkActivateArgs),         // variant 29
    RejectLink(LinkRejectArgs),             // variant 30
    UpdateLink(LinkUpdateArgs),             // variant 31
    SuspendLink(),                          // variant 32
    ResumeLink(),                           // variant 33
    DeleteLink(LinkDeleteArgs),             // variant 34
    CloseAccountLink(LinkCloseAccountArgs), // variant 35

    CreateUser(UserCreateArgs),             // variant 36
    ActivateUser(UserActivateArgs),         // variant 37
    RejectUser(UserRejectArgs),             // variant 38
    UpdateUser(UserUpdateArgs),             // variant 39
    SuspendUser(),                          // variant 40
    ResumeUser(),                           // variant 41
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
    CloseAccessPass(CloseAccessPassArgs),     // variant 69
    CheckStatusAccessPass(CheckStatusAccessPassArgs), // variant 70
    CheckUserAccessPass(CheckUserAccessPassArgs), // variant 71

    ActivateDeviceInterface(DeviceInterfaceActivateArgs), // variant 72
    CreateDeviceInterface(DeviceInterfaceCreateArgs),     // variant 73
    DeleteDeviceInterface(DeviceInterfaceDeleteArgs),     // variant 74
    RemoveDeviceInterface(DeviceInterfaceRemoveArgs),     // variant 75
    UpdateDeviceInterface(DeviceInterfaceUpdateArgs),     // variant 76
    UnlinkDeviceInterface(DeviceInterfaceUnlinkArgs),     // variant 77
    RejectDeviceInterface(DeviceInterfaceRejectArgs),     // variant 78

    SetMinVersion(SetVersionArgs), // variant 79

    AllocateResource(ResourceAllocateArgs),     // variant 80
    CreateResource(ResourceCreateArgs),         // variant 81
    DeallocateResource(ResourceDeallocateArgs), // variant 82

    SetDeviceHealth(DeviceSetHealthArgs), // variant 83
    SetLinkHealth(LinkSetHealthArgs),     // variant 84

    CloseResource(ResourceExtensionCloseAccountArgs), // variant 85

    AddQaAllowlist(AddQaAllowlistArgs),       // variant 86
    RemoveQaAllowlist(RemoveQaAllowlistArgs), // variant 87

    CreateTenant(TenantCreateArgs),                     // variant 88
    UpdateTenant(TenantUpdateArgs),                     // variant 89
    DeleteTenant(TenantDeleteArgs),                     // variant 90
    TenantAddAdministrator(TenantAddAdministratorArgs), // variant 91
    TenantRemoveAdministrator(TenantRemoveAdministratorArgs), // variant 92
    UpdatePaymentStatus(UpdatePaymentStatusArgs),       // variant 93
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
            0 => Ok(Self::Migrate(MigrateArgs::try_from(rest).unwrap())),
            1 => Ok(Self::InitGlobalState()),
            2 => Ok(Self::SetAuthority(SetAuthorityArgs::try_from(rest).unwrap())),
            3 => Ok(Self::SetGlobalConfig(SetGlobalConfigArgs::try_from(rest).unwrap())),

            4 => Ok(Self::AddFoundationAllowlist(AddFoundationAllowlistArgs::try_from(rest).unwrap())),
            5 => Ok(Self::RemoveFoundationAllowlist(RemoveFoundationAllowlistArgs::try_from(rest).unwrap())),
            6 => Ok(Self::AddDeviceAllowlist()),
            7 => Ok(Self::RemoveDeviceAllowlist()),
            8 => Ok(Self::AddUserAllowlist()),
            9 => Ok(Self::RemoveUserAllowlist()),

            10 => Ok(Self::CreateLocation(LocationCreateArgs::try_from(rest).unwrap())),
            11 => Ok(Self::UpdateLocation(LocationUpdateArgs::try_from(rest).unwrap())),
            12 => Ok(Self::SuspendLocation(LocationSuspendArgs::try_from(rest).unwrap())),
            13 => Ok(Self::ResumeLocation(LocationResumeArgs::try_from(rest).unwrap())),
            14 => Ok(Self::DeleteLocation(LocationDeleteArgs::try_from(rest).unwrap())),

            15 => Ok(Self::CreateExchange(ExchangeCreateArgs::try_from(rest).unwrap())),
            16 => Ok(Self::UpdateExchange(ExchangeUpdateArgs::try_from(rest).unwrap())),
            17 => Ok(Self::SuspendExchange(ExchangeSuspendArgs::try_from(rest).unwrap())),
            18 => Ok(Self::ResumeExchange(ExchangeResumeArgs::try_from(rest).unwrap())),
            19 => Ok(Self::DeleteExchange(ExchangeDeleteArgs::try_from(rest).unwrap())),

            20 => Ok(Self::CreateDevice(DeviceCreateArgs::try_from(rest).unwrap())),
            21 => Ok(Self::ActivateDevice(DeviceActivateArgs::try_from(rest).unwrap())),
            22 => Ok(Self::RejectDevice(DeviceRejectArgs::try_from(rest).unwrap())),
            23 => Ok(Self::UpdateDevice(DeviceUpdateArgs::try_from(rest).unwrap())),
            24 => Ok(Self::SuspendDevice()),
            25 => Ok(Self::ResumeDevice()),
            26 => Ok(Self::DeleteDevice(DeviceDeleteArgs::try_from(rest).unwrap())),
            27 => Ok(Self::CloseAccountDevice(DeviceCloseAccountArgs::try_from(rest).unwrap())),

            28 => Ok(Self::CreateLink(LinkCreateArgs::try_from(rest).unwrap())),
            29 => Ok(Self::ActivateLink(LinkActivateArgs::try_from(rest).unwrap())),
            30 => Ok(Self::RejectLink(LinkRejectArgs::try_from(rest).unwrap())),
            31 => Ok(Self::UpdateLink(LinkUpdateArgs::try_from(rest).unwrap())),
            32 => Ok(Self::SuspendLink()),
            33 => Ok(Self::ResumeLink()),
            34 => Ok(Self::DeleteLink(LinkDeleteArgs::try_from(rest).unwrap())),
            35 => Ok(Self::CloseAccountLink(LinkCloseAccountArgs::try_from(rest).unwrap())),

            36 => Ok(Self::CreateUser(UserCreateArgs::try_from(rest).unwrap())),
            37 => Ok(Self::ActivateUser(UserActivateArgs::try_from(rest).unwrap())),
            38 => Ok(Self::RejectUser(UserRejectArgs::try_from(rest).unwrap())),
            39 => Ok(Self::UpdateUser(UserUpdateArgs::try_from(rest).unwrap())),
            40 => Ok(Self::SuspendUser()),
            41 => Ok(Self::ResumeUser()),
            42 => Ok(Self::DeleteUser(UserDeleteArgs::try_from(rest).unwrap())),
            43 => Ok(Self::CloseAccountUser(UserCloseAccountArgs::try_from(rest).unwrap())),
            44 => Ok(Self::RequestBanUser(UserRequestBanArgs::try_from(rest).unwrap())),
            45 => Ok(Self::BanUser(UserBanArgs::try_from(rest).unwrap())),


            46 => Ok(Self::CreateMulticastGroup(MulticastGroupCreateArgs::try_from(rest).unwrap())),
            47 => Ok(Self::ActivateMulticastGroup(MulticastGroupActivateArgs::try_from(rest).unwrap())),
            48 => Ok(Self::RejectMulticastGroup(MulticastGroupRejectArgs::try_from(rest).unwrap())),
            49 => Ok(Self::UpdateMulticastGroup(MulticastGroupUpdateArgs::try_from(rest).unwrap())),
            50 => Ok(Self::SuspendMulticastGroup(MulticastGroupSuspendArgs::try_from(rest).unwrap())),
            51 => Ok(Self::ReactivateMulticastGroup(MulticastGroupReactivateArgs::try_from(rest).unwrap())),
            52 => Ok(Self::DeleteMulticastGroup(MulticastGroupDeleteArgs::try_from(rest).unwrap())),
            53 => Ok(Self::DeactivateMulticastGroup(MulticastGroupDeactivateArgs::try_from(rest).unwrap())),

            54 => Ok(Self::AddMulticastGroupPubAllowlist(AddMulticastGroupPubAllowlistArgs::try_from(rest).unwrap())),
            55 => Ok(Self::RemoveMulticastGroupPubAllowlist(RemoveMulticastGroupPubAllowlistArgs::try_from(rest).unwrap())),
            56 => Ok(Self::AddMulticastGroupSubAllowlist(AddMulticastGroupSubAllowlistArgs::try_from(rest).unwrap())),
            57 => Ok(Self::RemoveMulticastGroupSubAllowlist(RemoveMulticastGroupSubAllowlistArgs::try_from(rest).unwrap())),
            58 => Ok(Self::SubscribeMulticastGroup(MulticastGroupSubscribeArgs::try_from(rest).unwrap())),
            59 => Ok(Self::CreateSubscribeUser(UserCreateSubscribeArgs::try_from(rest).unwrap())),

            60 => Ok(Self::CreateContributor(ContributorCreateArgs::try_from(rest).unwrap())),
            61 => Ok(Self::UpdateContributor(ContributorUpdateArgs::try_from(rest).unwrap())),
            62 => Ok(Self::SuspendContributor(ContributorSuspendArgs::try_from(rest).unwrap())),
            63 => Ok(Self::ResumeContributor(ContributorResumeArgs::try_from(rest).unwrap())),
            64 => Ok(Self::DeleteContributor(ContributorDeleteArgs::try_from(rest).unwrap())),

            65 => Ok(Self::SetDeviceExchange(ExchangeSetDeviceArgs::try_from(rest).unwrap())),
            66 => Ok(Self::AcceptLink(LinkAcceptArgs::try_from(rest).unwrap())),
            67 => Ok(Self::SetAccessPass(SetAccessPassArgs::try_from(rest).unwrap())),

            68 => Ok(Self::SetAirdrop(SetAirdropArgs::try_from(rest).unwrap())),
            69 => Ok(Self::CloseAccessPass(CloseAccessPassArgs::try_from(rest).unwrap())),
            70 => Ok(Self::CheckStatusAccessPass(CheckStatusAccessPassArgs::try_from(rest).unwrap())),
            71 => Ok(Self::CheckUserAccessPass(CheckUserAccessPassArgs::try_from(rest).unwrap())),

            72 => Ok(Self::ActivateDeviceInterface(DeviceInterfaceActivateArgs::try_from(rest).unwrap())),
            73 => Ok(Self::CreateDeviceInterface(DeviceInterfaceCreateArgs::try_from(rest).unwrap())),
            74 => Ok(Self::DeleteDeviceInterface(DeviceInterfaceDeleteArgs::try_from(rest).unwrap())),
            75 => Ok(Self::RemoveDeviceInterface(DeviceInterfaceRemoveArgs::try_from(rest).unwrap())),
            76 => Ok(Self::UpdateDeviceInterface(DeviceInterfaceUpdateArgs::try_from(rest).unwrap())),
            77 => Ok(Self::UnlinkDeviceInterface(DeviceInterfaceUnlinkArgs::try_from(rest).unwrap())),
            78 => Ok(Self::RejectDeviceInterface(DeviceInterfaceRejectArgs::try_from(rest).unwrap())),

            79 => Ok(Self::SetMinVersion(SetVersionArgs::try_from(rest).unwrap())),
            80 => Ok(Self::AllocateResource(ResourceAllocateArgs::try_from(rest).unwrap())),
            81 => Ok(Self::CreateResource(ResourceCreateArgs::try_from(rest).unwrap())),
            82 => Ok(Self::DeallocateResource(ResourceDeallocateArgs::try_from(rest).unwrap())),
            83 => Ok(Self::SetDeviceHealth(DeviceSetHealthArgs::try_from(rest).unwrap())),
            84 => Ok(Self::SetLinkHealth(LinkSetHealthArgs::try_from(rest).unwrap())),
            85 => Ok(Self::CloseResource(ResourceExtensionCloseAccountArgs::try_from(rest).unwrap())),

            86 => Ok(Self::AddQaAllowlist(AddQaAllowlistArgs::try_from(rest).unwrap())),
            87 => Ok(Self::RemoveQaAllowlist(RemoveQaAllowlistArgs::try_from(rest).unwrap())),

            88 => Ok(Self::CreateTenant(TenantCreateArgs::try_from(rest).unwrap())),
            89 => Ok(Self::UpdateTenant(TenantUpdateArgs::try_from(rest).unwrap())),
            90 => Ok(Self::DeleteTenant(TenantDeleteArgs::try_from(rest).unwrap())),
            91 => Ok(Self::TenantAddAdministrator(TenantAddAdministratorArgs::try_from(rest).unwrap())),
            92 => Ok(Self::TenantRemoveAdministrator(TenantRemoveAdministratorArgs::try_from(rest).unwrap())),
            93 => Ok(Self::UpdatePaymentStatus(UpdatePaymentStatusArgs::try_from(rest).unwrap())),

            _ => Err(ProgramError::InvalidInstructionData),
        }
    }

    pub fn get_name(&self) -> String {
        match self {
            Self::Migrate(_) => "Migrate".to_string(), // variant 0
            Self::InitGlobalState() => "InitGlobalState".to_string(), // variant 1
            Self::SetAuthority(_) => "SetAuthority".to_string(), // variant 2
            Self::SetGlobalConfig(_) => "SetGlobalConfig".to_string(), // variant 3

            Self::AddFoundationAllowlist(_) => "AddFoundationAllowlist".to_string(), // variant 4
            Self::RemoveFoundationAllowlist(_) => "RemoveFoundationAllowlist".to_string(), // variant 5
            Self::AddDeviceAllowlist() => "AddDeviceAllowlist".to_string(), // variant 6
            Self::RemoveDeviceAllowlist() => "RemoveDeviceAllowlist".to_string(), // variant 7
            Self::AddUserAllowlist() => "AddUserAllowlist".to_string(),     // variant 8
            Self::RemoveUserAllowlist() => "RemoveUserAllowlist".to_string(), // variant 9

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
            Self::SuspendDevice() => "SuspendDevice".to_string(), // variant 24
            Self::ResumeDevice() => "ResumeDevice".to_string(),  // variant 25
            Self::DeleteDevice(_) => "DeleteDevice".to_string(), // variant 26
            Self::CloseAccountDevice(_) => "CloseAccountDevice".to_string(), // variant 27

            Self::CreateLink(_) => "CreateLink".to_string(), // variant 28
            Self::ActivateLink(_) => "ActivateLink".to_string(), // variant 29
            Self::RejectLink(_) => "RejectLink".to_string(), // variant 30
            Self::UpdateLink(_) => "UpdateLink".to_string(), // variant 31
            Self::SuspendLink() => "SuspendLink".to_string(), // variant 32
            Self::ResumeLink() => "ResumeLink".to_string(),  // variant 33
            Self::DeleteLink(_) => "DeleteLink".to_string(), // variant 34
            Self::CloseAccountLink(_) => "CloseAccountLink".to_string(), // variant 35

            Self::CreateUser(_) => "CreateUser".to_string(), // variant 36
            Self::ActivateUser(_) => "ActivateUser".to_string(), // variant 37
            Self::RejectUser(_) => "RejectUser".to_string(), // variant 38
            Self::UpdateUser(_) => "UpdateUser".to_string(), // variant 39
            Self::SuspendUser() => "SuspendUser".to_string(), // variant 40
            Self::ResumeUser() => "ResumeUser".to_string(),  // variant 41
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
            Self::CloseAccessPass(_) => "CloseAccessPass".to_string(),     // variant 69
            Self::CheckStatusAccessPass(_) => "CheckStatusAccessPass".to_string(), // variant 70
            Self::CheckUserAccessPass(_) => "CheckUserAccessPass".to_string(), // variant 71

            Self::ActivateDeviceInterface(_) => "ActivateDeviceInterface".to_string(), // variant 72
            Self::CreateDeviceInterface(_) => "CreateDeviceInterface".to_string(),     // variant 73
            Self::DeleteDeviceInterface(_) => "DeleteDeviceInterface".to_string(),     // variant 74
            Self::RemoveDeviceInterface(_) => "RemoveDeviceInterface".to_string(),     // variant 75
            Self::UpdateDeviceInterface(_) => "UpdateDeviceInterface".to_string(),     // variant 76
            Self::UnlinkDeviceInterface(_) => "UnlinkDeviceInterface".to_string(),     // variant 77
            Self::RejectDeviceInterface(_) => "RejectDeviceInterface".to_string(),     // variant 78

            Self::SetMinVersion(_) => "SetMinVersion".to_string(), // variant 79
            Self::AllocateResource(_) => "AllocateResource".to_string(), // variant 80
            Self::CreateResource(_) => "CreateResource".to_string(), // variant 81
            Self::DeallocateResource(_) => "DeallocateResource".to_string(), // variant 82
            Self::SetDeviceHealth(_) => "SetDeviceHealth".to_string(), // variant 83
            Self::SetLinkHealth(_) => "SetLinkHealth".to_string(), // variant 84
            Self::CloseResource(_) => "CloseResource".to_string(), // variant 85

            Self::AddQaAllowlist(_) => "AddQaAllowlist".to_string(), // variant 86
            Self::RemoveQaAllowlist(_) => "RemoveQaAllowlist".to_string(), // variant 87

            Self::CreateTenant(_) => "CreateTenant".to_string(), // variant 88
            Self::UpdateTenant(_) => "UpdateTenant".to_string(), // variant 89
            Self::DeleteTenant(_) => "DeleteTenant".to_string(), // variant 90
            Self::TenantAddAdministrator(_) => "TenantAddAdministrator".to_string(), // variant 91
            Self::TenantRemoveAdministrator(_) => "TenantRemoveAdministrator".to_string(), // variant 92
            Self::UpdatePaymentStatus(_) => "UpdatePaymentStatus".to_string(), // variant 93
        }
    }

    pub fn get_args(&self) -> String {
        match self {
            Self::Migrate(args) => format!("{args:?}"), // variant 0
            Self::InitGlobalState() => "".to_string(),  // variant 1
            Self::SetAuthority(args) => format!("{args:?}"), // variant 2
            Self::SetGlobalConfig(args) => format!("{args:?}"), // variant 3

            Self::AddFoundationAllowlist(args) => format!("{args:?}"), // variant 4
            Self::RemoveFoundationAllowlist(args) => format!("{args:?}"), // variant 5
            Self::AddDeviceAllowlist() => "".to_string(),              // variant 6
            Self::RemoveDeviceAllowlist() => "".to_string(),           // variant 7
            Self::AddUserAllowlist() => "".to_string(),                // variant 8
            Self::RemoveUserAllowlist() => "".to_string(),             // variant 9

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
            Self::SuspendDevice() => "".to_string(),         // variant 24
            Self::ResumeDevice() => "".to_string(),          // variant 25
            Self::DeleteDevice(args) => format!("{args:?}"), // variant 26
            Self::CloseAccountDevice(args) => format!("{args:?}"), // variant 27

            Self::CreateLink(args) => format!("{args:?}"), // variant 28
            Self::ActivateLink(args) => format!("{args:?}"), // variant 29
            Self::RejectLink(args) => format!("{args:?}"), // variant 30
            Self::UpdateLink(args) => format!("{args:?}"), // variant 31
            Self::SuspendLink() => "".to_string(),         // variant 32
            Self::ResumeLink() => "".to_string(),          // variant 33
            Self::DeleteLink(args) => format!("{args:?}"), // variant 34
            Self::CloseAccountLink(args) => format!("{args:?}"), // variant 35

            Self::CreateUser(args) => format!("{args:?}"), // variant 36
            Self::ActivateUser(args) => format!("{args:?}"), // variant 37
            Self::RejectUser(args) => format!("{args:?}"), // variant 38
            Self::UpdateUser(args) => format!("{args:?}"), // variant 39
            Self::SuspendUser() => "".to_string(),         // variant 40
            Self::ResumeUser() => "".to_string(),          // variant 41
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
            Self::CloseAccessPass(args) => format!("{args:?}"),   // variant 69
            Self::CheckStatusAccessPass(args) => format!("{args:?}"), // variant 70
            Self::CheckUserAccessPass(args) => format!("{args:?}"), // variant 71

            Self::ActivateDeviceInterface(args) => format!("{args:?}"), // variant 72
            Self::CreateDeviceInterface(args) => format!("{args:?}"),   // variant 73
            Self::DeleteDeviceInterface(args) => format!("{args:?}"),   // variant 74
            Self::RemoveDeviceInterface(args) => format!("{args:?}"),   // variant 75
            Self::UpdateDeviceInterface(args) => format!("{args:?}"),   // variant 76
            Self::UnlinkDeviceInterface(args) => format!("{args:?}"),   // variant 77
            Self::RejectDeviceInterface(args) => format!("{args:?}"),   // variant 78

            Self::SetMinVersion(args) => format!("{args:?}"), // variant 79
            Self::AllocateResource(args) => format!("{args:?}"), // variant 80
            Self::CreateResource(args) => format!("{args:?}"), // variant 81
            Self::DeallocateResource(args) => format!("{args:?}"), // variant 82
            Self::SetDeviceHealth(args) => format!("{args:?}"), // variant 83
            Self::SetLinkHealth(args) => format!("{args:?}"), // variant 84
            Self::CloseResource(args) => format!("{args:?}"), // variant 85

            Self::AddQaAllowlist(args) => format!("{args:?}"), // variant 86
            Self::RemoveQaAllowlist(args) => format!("{args:?}"), // variant 87

            Self::CreateTenant(args) => format!("{args:?}"), // variant 88
            Self::UpdateTenant(args) => format!("{args:?}"), // variant 89
            Self::DeleteTenant(args) => format!("{args:?}"), // variant 90
            Self::TenantAddAdministrator(args) => format!("{args:?}"), // variant 91
            Self::TenantRemoveAdministrator(args) => format!("{args:?}"), // variant 92
            Self::UpdatePaymentStatus(args) => format!("{args:?}"), // variant 93
        }
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        processors::exchange::setdevice::SetDeviceOption,
        resource::{IdOrIp, ResourceType},
        state::{
            device::{DeviceHealth, DeviceType},
            interface::{LoopbackType, RoutingMode},
            link::{LinkHealth, LinkLinkType},
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
        test_instruction(DoubleZeroInstruction::Migrate(MigrateArgs {}), "Migrate");
        test_instruction(DoubleZeroInstruction::InitGlobalState(), "InitGlobalState");
        test_instruction(
            DoubleZeroInstruction::SetGlobalConfig(SetGlobalConfigArgs {
                local_asn: 100,
                remote_asn: 200,
                device_tunnel_block: "1.2.3.4/1".parse().unwrap(),
                user_tunnel_block: "1.2.3.4/1".parse().unwrap(),
                multicastgroup_block: "1.2.3.4/1".parse().unwrap(),
                next_bgp_community: None,
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
            DoubleZeroInstruction::SuspendLocation(LocationSuspendArgs {}),
            "SuspendLocation",
        );
        test_instruction(
            DoubleZeroInstruction::ResumeLocation(LocationResumeArgs {}),
            "ResumeLocation",
        );
        test_instruction(
            DoubleZeroInstruction::DeleteLocation(LocationDeleteArgs {}),
            "DeleteLocation",
        );
        test_instruction(
            DoubleZeroInstruction::CreateExchange(ExchangeCreateArgs {
                code: "test".to_string(),
                name: "test".to_string(),
                lat: 1.0,
                lng: 2.0,
                reserved: 0,
            }),
            "CreateExchange",
        );
        test_instruction(
            DoubleZeroInstruction::UpdateExchange(ExchangeUpdateArgs {
                lat: Some(1.0),
                lng: Some(2.0),
                bgp_community: Some(123),
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
                device_type: DeviceType::Hybrid,
                dz_prefixes: "1.2.3.4/1".parse().unwrap(),
                metrics_publisher_pk: Pubkey::new_unique(),
                mgmt_vrf: "mgmt".to_string(),
                desired_status: None,
            }),
            "CreateDevice",
        );
        test_instruction(
            DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs { resource_count: 0 }),
            "ActivateDevice",
        );
        test_instruction(
            DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
                code: Some("test".to_string()),
                public_ip: Some([1, 2, 3, 4].into()),
                contributor_pk: Some(Pubkey::new_unique()),
                device_type: Some(DeviceType::Hybrid),
                dz_prefixes: Some("1.2.3.4/1".parse().unwrap()),
                metrics_publisher_pk: Some(Pubkey::new_unique()),
                mgmt_vrf: Some("mgmt".to_string()),
                max_users: None,
                users_count: None,
                status: None,
                desired_status: None,
                reference_count: None,
                resource_count: 0,
            }),
            "UpdateDevice",
        );
        test_instruction(DoubleZeroInstruction::SuspendDevice(), "SuspendDevice");
        test_instruction(DoubleZeroInstruction::ResumeDevice(), "ResumeDevice");
        test_instruction(
            DoubleZeroInstruction::DeleteDevice(DeviceDeleteArgs {}),
            "DeleteDevice",
        );
        test_instruction(
            DoubleZeroInstruction::CreateLink(LinkCreateArgs {
                code: "test".to_string(),
                link_type: LinkLinkType::WAN,
                bandwidth: 100,
                mtu: 1500,
                delay_ns: 10_000_000,
                jitter_ns: 1_000_000,
                side_a_iface_name: "eth0".to_string(),
                side_z_iface_name: Some("eth1".to_string()),
                desired_status: None,
            }),
            "CreateLink",
        );
        test_instruction(
            DoubleZeroInstruction::ActivateLink(LinkActivateArgs {
                tunnel_id: 1,
                tunnel_net: "1.2.3.4/1".parse().unwrap(),
                use_onchain_allocation: false,
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
                delay_ns: Some(10000),
                jitter_ns: Some(100),
                delay_override_ns: Some(0),
                status: None,
                desired_status: None,
            }),
            "UpdateLink",
        );
        test_instruction(DoubleZeroInstruction::SuspendLink(), "SuspendLink");
        test_instruction(DoubleZeroInstruction::ResumeLink(), "ResumeLink");
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
                dz_prefix_count: 0,
            }),
            "ActivateUser",
        );
        test_instruction(
            DoubleZeroInstruction::UpdateUser(UserUpdateArgs {
                user_type: Some(UserType::IBRL),
                cyoa_type: Some(UserCYOA::GREOverDIA),
                dz_ip: Some([1, 2, 3, 4].into()),
                tunnel_id: Some(1),
                tunnel_net: Some("1.2.3.4/1".parse().unwrap()),
                validator_pubkey: Some(Pubkey::new_unique()),
                tenant_pk: Some(Pubkey::new_unique()),
            }),
            "UpdateUser",
        );
        test_instruction(DoubleZeroInstruction::SuspendUser(), "SuspendUser");
        test_instruction(DoubleZeroInstruction::ResumeUser(), "ResumeUser");
        test_instruction(
            DoubleZeroInstruction::DeleteUser(UserDeleteArgs {}),
            "DeleteUser",
        );
        test_instruction(
            DoubleZeroInstruction::CloseAccountDevice(DeviceCloseAccountArgs { resource_count: 0 }),
            "CloseAccountDevice",
        );
        test_instruction(
            DoubleZeroInstruction::CloseAccountLink(LinkCloseAccountArgs {
                use_onchain_deallocation: false,
            }),
            "CloseAccountLink",
        );
        test_instruction(
            DoubleZeroInstruction::CloseAccountUser(UserCloseAccountArgs { dz_prefix_count: 0 }),
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
            DoubleZeroInstruction::AddQaAllowlist(AddQaAllowlistArgs {
                pubkey: Pubkey::new_unique(),
            }),
            "AddQaAllowlist",
        );
        test_instruction(
            DoubleZeroInstruction::RemoveQaAllowlist(RemoveQaAllowlistArgs {
                pubkey: Pubkey::new_unique(),
            }),
            "RemoveQaAllowlist",
        );
        test_instruction(
            DoubleZeroInstruction::AddDeviceAllowlist(),
            "AddDeviceAllowlist",
        );
        test_instruction(
            DoubleZeroInstruction::RemoveDeviceAllowlist(),
            "RemoveDeviceAllowlist",
        );
        test_instruction(
            DoubleZeroInstruction::AddUserAllowlist(),
            "AddUserAllowlist",
        );
        test_instruction(
            DoubleZeroInstruction::RemoveUserAllowlist(),
            "RemoveUserAllowlist",
        );
        test_instruction(
            DoubleZeroInstruction::RequestBanUser(UserRequestBanArgs {}),
            "RequestBanUser",
        );
        test_instruction(DoubleZeroInstruction::BanUser(UserBanArgs {}), "BanUser");

        test_instruction(
            DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
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
                publisher_count: None,
                subscriber_count: None,
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
            DoubleZeroInstruction::DeactivateMulticastGroup(MulticastGroupDeactivateArgs {
                use_onchain_deallocation: false,
            }),
            "DeactivateMulticastGroup",
        );

        test_instruction(
            DoubleZeroInstruction::AddMulticastGroupPubAllowlist(
                AddMulticastGroupPubAllowlistArgs {
                    client_ip: [1, 2, 3, 4].into(),
                    user_payer: Pubkey::new_unique(),
                },
            ),
            "AddMulticastGroupPubAllowlist",
        );
        test_instruction(
            DoubleZeroInstruction::RemoveMulticastGroupPubAllowlist(
                RemoveMulticastGroupPubAllowlistArgs {
                    client_ip: [1, 2, 3, 4].into(),
                    user_payer: Pubkey::new_unique(),
                },
            ),
            "RemoveMulticastGroupPubAllowlist",
        );
        test_instruction(
            DoubleZeroInstruction::AddMulticastGroupSubAllowlist(
                AddMulticastGroupSubAllowlistArgs {
                    client_ip: [1, 2, 3, 4].into(),
                    user_payer: Pubkey::new_unique(),
                },
            ),
            "AddMulticastGroupSubAllowlist",
        );
        test_instruction(
            DoubleZeroInstruction::RemoveMulticastGroupSubAllowlist(
                RemoveMulticastGroupSubAllowlistArgs {
                    client_ip: [1, 2, 3, 4].into(),
                    user_payer: Pubkey::new_unique(),
                },
            ),
            "RemoveMulticastGroupSubAllowlist",
        );
        test_instruction(
            DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
                client_ip: [1, 2, 3, 4].into(),
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
                ops_manager_pk: Some(Pubkey::new_unique()),
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
                allow_multiple_ip: false,
                tenant: Pubkey::default(),
            }),
            "SetAccessPass",
        );
        test_instruction(
            DoubleZeroInstruction::SetAirdrop(SetAirdropArgs {
                contributor_airdrop_lamports: Some(1),
                user_airdrop_lamports: Some(2),
            }),
            "SetAirdrop",
        );
        test_instruction(
            DoubleZeroInstruction::CloseAccessPass(CloseAccessPassArgs {}),
            "CloseAccessPass",
        );
        test_instruction(
            DoubleZeroInstruction::CheckStatusAccessPass(CheckStatusAccessPassArgs {}),
            "CheckStatusAccessPass",
        );
        test_instruction(
            DoubleZeroInstruction::CheckUserAccessPass(CheckUserAccessPassArgs {}),
            "CheckUserAccessPass",
        );
        test_instruction(
            DoubleZeroInstruction::ActivateDeviceInterface(DeviceInterfaceActivateArgs {
                name: "name".to_string(),
                ip_net: "10.0.0.0/3".parse().unwrap(),
                node_segment_idx: 1,
            }),
            "ActivateDeviceInterface",
        );
        test_instruction(
            DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
                name: "name".to_string(),
                interface_dia: crate::state::interface::InterfaceDIA::None,
                interface_cyoa: crate::state::interface::InterfaceCYOA::None,
                bandwidth: 0,
                cir: 0,
                mtu: 1500,
                routing_mode: RoutingMode::Static,
                loopback_type: LoopbackType::None,
                ip_net: None,
                vlan_id: 0,
                user_tunnel_endpoint: false,
            }),
            "CreateDeviceInterface",
        );
        test_instruction(
            DoubleZeroInstruction::DeleteDeviceInterface(DeviceInterfaceDeleteArgs {
                name: "name".to_string(),
            }),
            "DeleteDeviceInterface",
        );
        test_instruction(
            DoubleZeroInstruction::RemoveDeviceInterface(DeviceInterfaceRemoveArgs {
                name: "name".to_string(),
            }),
            "RemoveDeviceInterface",
        );
        test_instruction(
            DoubleZeroInstruction::UpdateDeviceInterface(DeviceInterfaceUpdateArgs {
                name: "name".to_string(),
                loopback_type: Some(LoopbackType::None),
                interface_dia: Some(crate::state::interface::InterfaceDIA::None),
                interface_cyoa: Some(crate::state::interface::InterfaceCYOA::None),
                bandwidth: Some(0),
                cir: Some(0),
                mtu: Some(1500),
                routing_mode: Some(RoutingMode::Static),
                vlan_id: Some(0),
                user_tunnel_endpoint: Some(false),
                ip_net: Some("10.0.0.0/3".parse().unwrap()),
                node_segment_idx: Some(1),
                status: None,
            }),
            "UpdateDeviceInterface",
        );
        test_instruction(
            DoubleZeroInstruction::UnlinkDeviceInterface(DeviceInterfaceUnlinkArgs {
                name: "name".to_string(),
            }),
            "UnlinkDeviceInterface",
        );
        test_instruction(
            DoubleZeroInstruction::RejectDeviceInterface(DeviceInterfaceRejectArgs {
                name: "name".to_string(),
            }),
            "RejectDeviceInterface",
        );
        test_instruction(
            DoubleZeroInstruction::SetMinVersion(SetVersionArgs {
                min_compatible_version: "1.0.0".parse().unwrap(),
            }),
            "SetMinVersion",
        );
        test_instruction(
            DoubleZeroInstruction::AllocateResource(ResourceAllocateArgs {
                resource_type: ResourceType::DeviceTunnelBlock,
                requested: None,
            }),
            "AllocateResource",
        );
        test_instruction(
            DoubleZeroInstruction::CreateResource(ResourceCreateArgs {
                resource_type: ResourceType::DeviceTunnelBlock,
            }),
            "CreateResource",
        );
        test_instruction(
            DoubleZeroInstruction::DeallocateResource(ResourceDeallocateArgs {
                resource_type: ResourceType::DeviceTunnelBlock,
                value: IdOrIp::Id(1),
            }),
            "DeallocateResource",
        );
        test_instruction(
            DoubleZeroInstruction::CloseResource(ResourceExtensionCloseAccountArgs {}),
            "CloseResource",
        );
        test_instruction(
            DoubleZeroInstruction::SetDeviceHealth(DeviceSetHealthArgs {
                health: DeviceHealth::Pending,
            }),
            "SetDeviceHealth",
        );
        test_instruction(
            DoubleZeroInstruction::SetLinkHealth(LinkSetHealthArgs {
                health: LinkHealth::Pending,
            }),
            "SetLinkHealth",
        );
        test_instruction(
            DoubleZeroInstruction::CreateTenant(TenantCreateArgs {
                code: "test".to_string(),
                administrator: Pubkey::new_unique(),
                token_account: None,
            }),
            "CreateTenant",
        );
        test_instruction(
            DoubleZeroInstruction::UpdateTenant(TenantUpdateArgs {
                vrf_id: Some(200),
                token_account: Some(Pubkey::new_unique()),
            }),
            "UpdateTenant",
        );
        test_instruction(
            DoubleZeroInstruction::UpdatePaymentStatus(UpdatePaymentStatusArgs {
                payment_status: 1,
            }),
            "UpdatePaymentStatus",
        );
    }
}
