use doublezero_sdk::commands::allowlist::device::add::AddDeviceAllowlistCommand;
use doublezero_sdk::commands::allowlist::device::list::ListDeviceAllowlistCommand;
use doublezero_sdk::commands::allowlist::device::remove::RemoveDeviceAllowlistCommand;
use doublezero_sdk::commands::allowlist::foundation::add::AddFoundationAllowlistCommand;
use doublezero_sdk::commands::allowlist::foundation::list::ListFoundationAllowlistCommand;
use doublezero_sdk::commands::allowlist::foundation::remove::RemoveFoundationAllowlistCommand;
use doublezero_sdk::commands::allowlist::user::add::AddUserAllowlistCommand;
use doublezero_sdk::commands::allowlist::user::list::ListUserAllowlistCommand;
use doublezero_sdk::commands::allowlist::user::remove::RemoveUserAllowlistCommand;
use doublezero_sdk::commands::device::closeaccount::CloseAccountDeviceCommand;
use doublezero_sdk::commands::device::resume::ResumeDeviceCommand;
use doublezero_sdk::commands::device::suspend::SuspendDeviceCommand;
use doublezero_sdk::commands::device::{
    activate::ActivateDeviceCommand, create::CreateDeviceCommand, delete::DeleteDeviceCommand,
    get::GetDeviceCommand, list::ListDeviceCommand, reject::RejectDeviceCommand,
    update::UpdateDeviceCommand,
};
use doublezero_sdk::commands::exchange::{
    create::CreateExchangeCommand, delete::DeleteExchangeCommand, get::GetExchangeCommand,
    list::ListExchangeCommand, update::UpdateExchangeCommand,
};
use doublezero_sdk::commands::globalconfig::set::SetGlobalConfigCommand;
use doublezero_sdk::commands::globalstate::init::InitGlobalStateCommand;
use doublezero_sdk::commands::location::{
    create::CreateLocationCommand, delete::DeleteLocationCommand, get::GetLocationCommand,
    list::ListLocationCommand, update::UpdateLocationCommand,
};
use doublezero_sdk::commands::multicastgroup::{
    activate::ActivateMulticastGroupCommand, create::CreateMulticastGroupCommand,
    deactivate::DeactivateMulticastGroupCommand, delete::DeleteMulticastGroupCommand,
    get::GetMulticastGroupCommand, list::ListMulticastGroupCommand,
    reject::RejectMulticastGroupCommand, subscribe::SubscribeMulticastGroupCommand,
    unsubscribe::UnsubscribeMulticastGroupCommand, update::UpdateMulticastGroupCommand,
};
use doublezero_sdk::commands::tunnel::activate::ActivateTunnelCommand;
use doublezero_sdk::commands::tunnel::{
    closeaccount::CloseAccountTunnelCommand, create::CreateTunnelCommand,
    delete::DeleteTunnelCommand, get::GetTunnelCommand, list::ListTunnelCommand,
    reject::RejectTunnelCommand, update::UpdateTunnelCommand,
};
use doublezero_sdk::commands::user::requestban::RequestBanUserCommand;
use doublezero_sdk::commands::user::{
    create::CreateUserCommand, delete::DeleteUserCommand, get::GetUserCommand,
    list::ListUserCommand, update::UpdateUserCommand,
};
use doublezero_sdk::MulticastGroup;
use doublezero_sdk::{
    DZClient, Device, DoubleZeroClient, Exchange, GetGlobalConfigCommand, GlobalConfig, Location,
    Tunnel, User,
};
use mockall::automock;
use solana_sdk::{pubkey::Pubkey, signature::Signature};
use std::collections::HashMap;

#[automock]
pub trait CliCommand {
    fn check_requirements(&self, checks: u8) -> eyre::Result<()>;

    fn get_program_id(&self) -> Pubkey;
    fn get_payer(&self) -> Pubkey;
    fn get_balance(&self) -> eyre::Result<u64>;
    fn get_logs(&self, pubkey: &Pubkey) -> eyre::Result<Vec<String>>;

    fn init_global_state(&self, cmd: InitGlobalStateCommand) -> eyre::Result<Signature>;
    fn get_globalconfig(&self, cmd: GetGlobalConfigCommand)
        -> eyre::Result<(Pubkey, GlobalConfig)>;
    fn set_globalconfig(&self, cmd: SetGlobalConfigCommand) -> eyre::Result<Signature>;

    fn create_location(&self, cmd: CreateLocationCommand) -> eyre::Result<(Signature, Pubkey)>;
    fn get_location(&self, cmd: GetLocationCommand) -> eyre::Result<(Pubkey, Location)>;
    fn list_location(&self, cmd: ListLocationCommand) -> eyre::Result<HashMap<Pubkey, Location>>;
    fn update_location(&self, cmd: UpdateLocationCommand) -> eyre::Result<Signature>;
    fn delete_location(&self, cmd: DeleteLocationCommand) -> eyre::Result<Signature>;

    fn create_exchange(&self, cmd: CreateExchangeCommand) -> eyre::Result<(Signature, Pubkey)>;
    fn get_exchange(&self, cmd: GetExchangeCommand) -> eyre::Result<(Pubkey, Exchange)>;
    fn list_exchange(&self, cmd: ListExchangeCommand) -> eyre::Result<HashMap<Pubkey, Exchange>>;
    fn update_exchange(&self, cmd: UpdateExchangeCommand) -> eyre::Result<Signature>;
    fn delete_exchange(&self, cmd: DeleteExchangeCommand) -> eyre::Result<Signature>;

    fn create_device(&self, cmd: CreateDeviceCommand) -> eyre::Result<(Signature, Pubkey)>;
    fn get_device(&self, cmd: GetDeviceCommand) -> eyre::Result<(Pubkey, Device)>;
    fn list_device(&self, cmd: ListDeviceCommand) -> eyre::Result<HashMap<Pubkey, Device>>;
    fn suspend_device(&self, cmd: SuspendDeviceCommand) -> eyre::Result<Signature>;
    fn resume_device(&self, cmd: ResumeDeviceCommand) -> eyre::Result<Signature>;
    fn update_device(&self, cmd: UpdateDeviceCommand) -> eyre::Result<Signature>;
    fn delete_device(&self, cmd: DeleteDeviceCommand) -> eyre::Result<Signature>;

    fn activate_device(&self, cmd: ActivateDeviceCommand) -> eyre::Result<Signature>;
    fn reject_device(&self, cmd: RejectDeviceCommand) -> eyre::Result<Signature>;
    fn closeaccount_device(&self, cmd: CloseAccountDeviceCommand) -> eyre::Result<Signature>;

    fn create_tunnel(&self, cmd: CreateTunnelCommand) -> eyre::Result<(Signature, Pubkey)>;
    fn get_tunnel(&self, cmd: GetTunnelCommand) -> eyre::Result<(Pubkey, Tunnel)>;
    fn list_tunnel(&self, cmd: ListTunnelCommand) -> eyre::Result<HashMap<Pubkey, Tunnel>>;
    fn update_tunnel(&self, cmd: UpdateTunnelCommand) -> eyre::Result<Signature>;
    fn delete_tunnel(&self, cmd: DeleteTunnelCommand) -> eyre::Result<Signature>;
    fn activate_tunnel(&self, cmd: ActivateTunnelCommand) -> eyre::Result<Signature>;
    fn reject_tunnel(&self, cmd: RejectTunnelCommand) -> eyre::Result<Signature>;
    fn closeaccount_tunnel(&self, cmd: CloseAccountTunnelCommand) -> eyre::Result<Signature>;

    fn create_user(&self, cmd: CreateUserCommand) -> eyre::Result<(Signature, Pubkey)>;
    fn get_user(&self, cmd: GetUserCommand) -> eyre::Result<(Pubkey, User)>;
    fn list_user(&self, cmd: ListUserCommand) -> eyre::Result<HashMap<Pubkey, User>>;
    fn update_user(&self, cmd: UpdateUserCommand) -> eyre::Result<Signature>;
    fn delete_user(&self, cmd: DeleteUserCommand) -> eyre::Result<Signature>;
    fn request_ban_user(&self, cmd: RequestBanUserCommand) -> eyre::Result<Signature>;

    fn list_foundation_allowlist(
        &self,
        cmd: ListFoundationAllowlistCommand,
    ) -> eyre::Result<Vec<Pubkey>>;
    fn list_device_allowlist(&self, cmd: ListDeviceAllowlistCommand) -> eyre::Result<Vec<Pubkey>>;
    fn list_user_allowlist(&self, cmd: ListUserAllowlistCommand) -> eyre::Result<Vec<Pubkey>>;
    fn add_foundation_allowlist(
        &self,
        cmd: AddFoundationAllowlistCommand,
    ) -> eyre::Result<Signature>;
    fn remove_foundation_allowlist(
        &self,
        cmd: RemoveFoundationAllowlistCommand,
    ) -> eyre::Result<Signature>;
    fn add_device_allowlist(&self, cmd: AddDeviceAllowlistCommand) -> eyre::Result<Signature>;
    fn remove_device_allowlist(&self, cmd: RemoveDeviceAllowlistCommand)
        -> eyre::Result<Signature>;
    fn add_user_allowlist(&self, cmd: AddUserAllowlistCommand) -> eyre::Result<Signature>;
    fn remove_user_allowlist(&self, cmd: RemoveUserAllowlistCommand) -> eyre::Result<Signature>;

    fn create_multicastgroup(
        &self,
        cmd: CreateMulticastGroupCommand,
    ) -> eyre::Result<(Signature, Pubkey)>;
    fn get_multicastgroup(
        &self,
        cmd: GetMulticastGroupCommand,
    ) -> eyre::Result<(Pubkey, MulticastGroup)>;
    fn list_multicastgroup(
        &self,
        cmd: ListMulticastGroupCommand,
    ) -> eyre::Result<HashMap<Pubkey, MulticastGroup>>;
    fn update_multicastgroup(&self, cmd: UpdateMulticastGroupCommand) -> eyre::Result<Signature>;
    fn delete_multicastgroup(&self, cmd: DeleteMulticastGroupCommand) -> eyre::Result<Signature>;
    fn activate_multicastgroup(
        &self,
        cmd: ActivateMulticastGroupCommand,
    ) -> eyre::Result<Signature>;
    fn reject_multicastgroup(&self, cmd: RejectMulticastGroupCommand) -> eyre::Result<Signature>;
    fn deactivate_multicastgroup(
        &self,
        cmd: DeactivateMulticastGroupCommand,
    ) -> eyre::Result<Signature>;
    fn subscribe_multicastgroup(
        &self,
        cmd: SubscribeMulticastGroupCommand,
    ) -> eyre::Result<Signature>;
    fn unsubscribe_multicastgroup(
        &self,
        cmd: UnsubscribeMulticastGroupCommand,
    ) -> eyre::Result<Signature>;
}

pub struct CliCommandImpl<'a> {
    client: &'a DZClient,
}

impl CliCommandImpl<'_> {
    pub fn new(client: &DZClient) -> CliCommandImpl {
        CliCommandImpl { client: client }
    }
}

impl CliCommand for CliCommandImpl<'_> {
    fn check_requirements(&self, checks: u8) -> eyre::Result<()> {
        crate::requirements::check_requirements(self, None, checks)
    }

    fn get_program_id(&self) -> Pubkey {
        *self.client.get_program_id()
    }
    fn get_payer(&self) -> Pubkey {
        self.client.get_payer()
    }
    fn get_balance(&self) -> eyre::Result<u64> {
        self.client.get_balance()
    }
    fn get_logs(&self, pubkey: &Pubkey) -> eyre::Result<Vec<String>> {
        self.client.get_logs(pubkey)
    }

    fn init_global_state(&self, cmd: InitGlobalStateCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }

    fn get_globalconfig(
        &self,
        cmd: GetGlobalConfigCommand,
    ) -> eyre::Result<(Pubkey, GlobalConfig)> {
        cmd.execute(self.client)
    }
    fn set_globalconfig(&self, cmd: SetGlobalConfigCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }

    fn create_location(&self, cmd: CreateLocationCommand) -> eyre::Result<(Signature, Pubkey)> {
        cmd.execute(self.client)
    }
    fn get_location(&self, cmd: GetLocationCommand) -> eyre::Result<(Pubkey, Location)> {
        cmd.execute(self.client)
    }
    fn list_location(&self, cmd: ListLocationCommand) -> eyre::Result<HashMap<Pubkey, Location>> {
        cmd.execute(self.client)
    }
    fn update_location(&self, cmd: UpdateLocationCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn delete_location(&self, cmd: DeleteLocationCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn create_exchange(&self, cmd: CreateExchangeCommand) -> eyre::Result<(Signature, Pubkey)> {
        cmd.execute(self.client)
    }
    fn get_exchange(&self, cmd: GetExchangeCommand) -> eyre::Result<(Pubkey, Exchange)> {
        cmd.execute(self.client)
    }
    fn list_exchange(&self, cmd: ListExchangeCommand) -> eyre::Result<HashMap<Pubkey, Exchange>> {
        cmd.execute(self.client)
    }
    fn update_exchange(&self, cmd: UpdateExchangeCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn delete_exchange(&self, cmd: DeleteExchangeCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn create_device(&self, cmd: CreateDeviceCommand) -> eyre::Result<(Signature, Pubkey)> {
        cmd.execute(self.client)
    }
    fn get_device(&self, cmd: GetDeviceCommand) -> eyre::Result<(Pubkey, Device)> {
        cmd.execute(self.client)
    }
    fn list_device(&self, cmd: ListDeviceCommand) -> eyre::Result<HashMap<Pubkey, Device>> {
        cmd.execute(self.client)
    }
    fn update_device(&self, cmd: UpdateDeviceCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn delete_device(&self, cmd: DeleteDeviceCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn activate_device(&self, cmd: ActivateDeviceCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn reject_device(&self, cmd: RejectDeviceCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn suspend_device(&self, cmd: SuspendDeviceCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn resume_device(&self, cmd: ResumeDeviceCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn closeaccount_device(&self, cmd: CloseAccountDeviceCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn create_tunnel(&self, cmd: CreateTunnelCommand) -> eyre::Result<(Signature, Pubkey)> {
        cmd.execute(self.client)
    }
    fn get_tunnel(&self, cmd: GetTunnelCommand) -> eyre::Result<(Pubkey, Tunnel)> {
        cmd.execute(self.client)
    }
    fn list_tunnel(&self, cmd: ListTunnelCommand) -> eyre::Result<HashMap<Pubkey, Tunnel>> {
        cmd.execute(self.client)
    }
    fn update_tunnel(&self, cmd: UpdateTunnelCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn delete_tunnel(&self, cmd: DeleteTunnelCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn activate_tunnel(&self, cmd: ActivateTunnelCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn reject_tunnel(&self, cmd: RejectTunnelCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn closeaccount_tunnel(&self, cmd: CloseAccountTunnelCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn create_user(&self, cmd: CreateUserCommand) -> eyre::Result<(Signature, Pubkey)> {
        cmd.execute(self.client)
    }
    fn get_user(&self, cmd: GetUserCommand) -> eyre::Result<(Pubkey, User)> {
        cmd.execute(self.client)
    }
    fn list_user(&self, cmd: ListUserCommand) -> eyre::Result<HashMap<Pubkey, User>> {
        cmd.execute(self.client)
    }
    fn update_user(&self, cmd: UpdateUserCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn delete_user(&self, cmd: DeleteUserCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn request_ban_user(&self, cmd: RequestBanUserCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn list_foundation_allowlist(
        &self,
        cmd: ListFoundationAllowlistCommand,
    ) -> eyre::Result<Vec<Pubkey>> {
        cmd.execute(self.client)
    }
    fn list_device_allowlist(&self, cmd: ListDeviceAllowlistCommand) -> eyre::Result<Vec<Pubkey>> {
        cmd.execute(self.client)
    }
    fn list_user_allowlist(&self, cmd: ListUserAllowlistCommand) -> eyre::Result<Vec<Pubkey>> {
        cmd.execute(self.client)
    }
    fn add_foundation_allowlist(
        &self,
        cmd: AddFoundationAllowlistCommand,
    ) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn remove_foundation_allowlist(
        &self,
        cmd: RemoveFoundationAllowlistCommand,
    ) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn add_device_allowlist(&self, cmd: AddDeviceAllowlistCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn remove_device_allowlist(
        &self,
        cmd: RemoveDeviceAllowlistCommand,
    ) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn add_user_allowlist(&self, cmd: AddUserAllowlistCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn remove_user_allowlist(&self, cmd: RemoveUserAllowlistCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn create_multicastgroup(
        &self,
        cmd: CreateMulticastGroupCommand,
    ) -> eyre::Result<(Signature, Pubkey)> {
        cmd.execute(self.client)
    }
    fn get_multicastgroup(
        &self,
        cmd: GetMulticastGroupCommand,
    ) -> eyre::Result<(Pubkey, MulticastGroup)> {
        cmd.execute(self.client)
    }
    fn list_multicastgroup(
        &self,
        cmd: ListMulticastGroupCommand,
    ) -> eyre::Result<HashMap<Pubkey, MulticastGroup>> {
        cmd.execute(self.client)
    }
    fn update_multicastgroup(&self, cmd: UpdateMulticastGroupCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn delete_multicastgroup(&self, cmd: DeleteMulticastGroupCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn activate_multicastgroup(
        &self,
        cmd: ActivateMulticastGroupCommand,
    ) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn reject_multicastgroup(&self, cmd: RejectMulticastGroupCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn deactivate_multicastgroup(
        &self,
        cmd: DeactivateMulticastGroupCommand,
    ) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn subscribe_multicastgroup(
        &self,
        cmd: SubscribeMulticastGroupCommand,
    ) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn unsubscribe_multicastgroup(
        &self,
        cmd: UnsubscribeMulticastGroupCommand,
    ) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
}
