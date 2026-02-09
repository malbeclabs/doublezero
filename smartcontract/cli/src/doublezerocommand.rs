use doublezero_config::Environment;
use doublezero_sdk::{
    commands::{
        accesspass::{
            close::CloseAccessPassCommand, get::GetAccessPassCommand, list::ListAccessPassCommand,
            set::SetAccessPassCommand,
        },
        allowlist::{
            foundation::{
                add::AddFoundationAllowlistCommand, list::ListFoundationAllowlistCommand,
                remove::RemoveFoundationAllowlistCommand,
            },
            qa::{
                add::AddQaAllowlistCommand, list::ListQaAllowlistCommand,
                remove::RemoveQaAllowlistCommand,
            },
        },
        contributor::{
            create::CreateContributorCommand, delete::DeleteContributorCommand,
            get::GetContributorCommand, list::ListContributorCommand,
            resume::ResumeContributorCommand, suspend::SuspendContributorCommand,
            update::UpdateContributorCommand,
        },
        device::{
            activate::ActivateDeviceCommand,
            closeaccount::CloseAccountDeviceCommand,
            create::CreateDeviceCommand,
            delete::DeleteDeviceCommand,
            get::GetDeviceCommand,
            interface::{
                activate::ActivateDeviceInterfaceCommand, create::CreateDeviceInterfaceCommand,
                delete::DeleteDeviceInterfaceCommand, remove::RemoveDeviceInterfaceCommand,
                update::UpdateDeviceInterfaceCommand,
            },
            list::ListDeviceCommand,
            reject::RejectDeviceCommand,
            sethealth::SetDeviceHealthCommand,
            update::UpdateDeviceCommand,
        },
        exchange::{
            create::CreateExchangeCommand, delete::DeleteExchangeCommand, get::GetExchangeCommand,
            list::ListExchangeCommand, setdevice::SetDeviceExchangeCommand,
            update::UpdateExchangeCommand,
        },
        globalconfig::set::SetGlobalConfigCommand,
        globalstate::{
            init::InitGlobalStateCommand, setairdrop::SetAirdropCommand,
            setauthority::SetAuthorityCommand, setversion::SetVersionCommand,
        },
        link::{
            accept::AcceptLinkCommand, activate::ActivateLinkCommand,
            closeaccount::CloseAccountLinkCommand, create::CreateLinkCommand,
            delete::DeleteLinkCommand, get::GetLinkCommand, latency::LatencyLinkCommand,
            list::ListLinkCommand, reject::RejectLinkCommand, sethealth::SetLinkHealthCommand,
            update::UpdateLinkCommand,
        },
        location::{
            create::CreateLocationCommand, delete::DeleteLocationCommand, get::GetLocationCommand,
            list::ListLocationCommand, update::UpdateLocationCommand,
        },
        migrate::MigrateCommand,
        multicastgroup::{
            activate::ActivateMulticastGroupCommand,
            allowlist::{
                publisher::{
                    add::AddMulticastGroupPubAllowlistCommand,
                    remove::RemoveMulticastGroupPubAllowlistCommand,
                },
                subscriber::{
                    add::AddMulticastGroupSubAllowlistCommand,
                    remove::RemoveMulticastGroupSubAllowlistCommand,
                },
            },
            create::CreateMulticastGroupCommand,
            deactivate::DeactivateMulticastGroupCommand,
            delete::DeleteMulticastGroupCommand,
            get::GetMulticastGroupCommand,
            list::ListMulticastGroupCommand,
            reject::RejectMulticastGroupCommand,
            subscribe::SubscribeMulticastGroupCommand,
            update::UpdateMulticastGroupCommand,
        },
        programconfig::get::GetProgramConfigCommand,
        resource::{
            allocate::AllocateResourceCommand, closeaccount::CloseResourceCommand,
            create::CreateResourceCommand, deallocate::DeallocateResourceCommand,
            get::GetResourceCommand,
        },
        tenant::{
            add_administrator::AddAdministratorTenantCommand, create::CreateTenantCommand,
            delete::DeleteTenantCommand, get::GetTenantCommand, list::ListTenantCommand,
            remove_administrator::RemoveAdministratorTenantCommand, update::UpdateTenantCommand,
            update_payment_status::UpdatePaymentStatusCommand,
        },
        user::{
            create::CreateUserCommand, create_subscribe::CreateSubscribeUserCommand,
            delete::DeleteUserCommand, get::GetUserCommand, list::ListUserCommand,
            requestban::RequestBanUserCommand, update::UpdateUserCommand,
        },
    },
    telemetry::LinkLatencyStats,
    DZClient, Device, DoubleZeroClient, Exchange, GetGlobalConfigCommand, GetGlobalStateCommand,
    GlobalConfig, GlobalState, Link, Location, MulticastGroup, ResourceExtensionOwned, User,
};
use doublezero_serviceability::state::{
    accesspass::AccessPass, accountdata::AccountData, contributor::Contributor,
    programconfig::ProgramConfig, tenant::Tenant,
};
use mockall::automock;
use solana_client::rpc_config::RpcProgramAccountsConfig;
use solana_sdk::{account::Account, pubkey::Pubkey, signature::Signature};
use std::collections::HashMap;

#[automock]
pub trait CliCommand {
    fn check_requirements(&self, checks: u8) -> eyre::Result<()>;

    fn get_program_config(
        &self,
        cmd: GetProgramConfigCommand,
    ) -> eyre::Result<(Pubkey, ProgramConfig)>;

    fn get_program_id(&self) -> Pubkey;
    fn get_environment(&self) -> Environment;
    fn get_payer(&self) -> Pubkey;
    fn get_balance(&self) -> eyre::Result<u64>;
    fn get_epoch(&self) -> eyre::Result<u64>;
    fn get_logs(&self, pubkey: &Pubkey) -> eyre::Result<Vec<String>>;
    fn get_account(&self, pubkey: Pubkey) -> eyre::Result<Account>;
    fn get_all(&self) -> eyre::Result<HashMap<Box<Pubkey>, Box<AccountData>>>;
    fn get_program_accounts(
        &self,
        program_id: &Pubkey,
        config: RpcProgramAccountsConfig,
    ) -> eyre::Result<Vec<(Pubkey, Account)>>;

    fn init_globalstate(&self, cmd: InitGlobalStateCommand) -> eyre::Result<Signature>;
    fn migrate(&self, cmd: MigrateCommand) -> eyre::Result<Vec<Signature>>;
    fn get_globalstate(&self, cmd: GetGlobalStateCommand) -> eyre::Result<(Pubkey, GlobalState)>;
    fn get_globalconfig(&self, cmd: GetGlobalConfigCommand)
        -> eyre::Result<(Pubkey, GlobalConfig)>;
    fn set_airdrop(&self, cmd: SetAirdropCommand) -> eyre::Result<Signature>;
    fn set_authority(&self, cmd: SetAuthorityCommand) -> eyre::Result<Signature>;
    fn set_globalconfig(&self, cmd: SetGlobalConfigCommand) -> eyre::Result<Signature>;
    fn set_minversion(&self, cmd: SetVersionCommand) -> eyre::Result<Signature>;

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
    fn setdevice_exchange(&self, cmd: SetDeviceExchangeCommand) -> eyre::Result<Signature>;

    fn create_contributor(
        &self,
        cmd: CreateContributorCommand,
    ) -> eyre::Result<(Signature, Pubkey)>;
    fn get_contributor(&self, cmd: GetContributorCommand) -> eyre::Result<(Pubkey, Contributor)>;
    fn suspend_contributor(&self, cmd: SuspendContributorCommand) -> eyre::Result<Signature>;
    fn resume_contributor(&self, cmd: ResumeContributorCommand) -> eyre::Result<Signature>;
    fn list_contributor(
        &self,
        cmd: ListContributorCommand,
    ) -> eyre::Result<HashMap<Pubkey, Contributor>>;
    fn update_contributor(&self, cmd: UpdateContributorCommand) -> eyre::Result<Signature>;
    fn delete_contributor(&self, cmd: DeleteContributorCommand) -> eyre::Result<Signature>;

    fn create_tenant(&self, cmd: CreateTenantCommand) -> eyre::Result<(Signature, Pubkey)>;
    fn get_tenant(&self, cmd: GetTenantCommand) -> eyre::Result<(Pubkey, Tenant)>;
    fn list_tenant(&self, cmd: ListTenantCommand) -> eyre::Result<HashMap<Pubkey, Tenant>>;
    fn update_tenant(&self, cmd: UpdateTenantCommand) -> eyre::Result<Signature>;
    fn delete_tenant(&self, cmd: DeleteTenantCommand) -> eyre::Result<Signature>;
    fn add_administrator_tenant(
        &self,
        cmd: AddAdministratorTenantCommand,
    ) -> eyre::Result<Signature>;
    fn remove_administrator_tenant(
        &self,
        cmd: RemoveAdministratorTenantCommand,
    ) -> eyre::Result<Signature>;
    fn update_payment_status_tenant(
        &self,
        cmd: UpdatePaymentStatusCommand,
    ) -> eyre::Result<Signature>;

    fn create_device(&self, cmd: CreateDeviceCommand) -> eyre::Result<(Signature, Pubkey)>;
    fn get_device(&self, cmd: GetDeviceCommand) -> eyre::Result<(Pubkey, Device)>;
    fn list_device(&self, cmd: ListDeviceCommand) -> eyre::Result<HashMap<Pubkey, Device>>;
    fn update_device(&self, cmd: UpdateDeviceCommand) -> eyre::Result<Signature>;
    fn delete_device(&self, cmd: DeleteDeviceCommand) -> eyre::Result<Signature>;
    fn set_device_health(&self, cmd: SetDeviceHealthCommand) -> eyre::Result<Signature>;

    fn activate_device(&self, cmd: ActivateDeviceCommand) -> eyre::Result<Signature>;
    fn reject_device(&self, cmd: RejectDeviceCommand) -> eyre::Result<Signature>;
    fn closeaccount_device(&self, cmd: CloseAccountDeviceCommand) -> eyre::Result<Signature>;

    fn activate_device_interface(
        &self,
        cmd: ActivateDeviceInterfaceCommand,
    ) -> eyre::Result<Signature>;
    fn create_device_interface(
        &self,
        cmd: CreateDeviceInterfaceCommand,
    ) -> eyre::Result<(Signature, Pubkey)>;
    fn delete_device_interface(&self, cmd: DeleteDeviceInterfaceCommand)
        -> eyre::Result<Signature>;
    fn remove_device_interface(&self, cmd: RemoveDeviceInterfaceCommand)
        -> eyre::Result<Signature>;
    fn update_device_interface(&self, cmd: UpdateDeviceInterfaceCommand)
        -> eyre::Result<Signature>;

    fn create_link(&self, cmd: CreateLinkCommand) -> eyre::Result<(Signature, Pubkey)>;
    fn accept_link(&self, cmd: AcceptLinkCommand) -> eyre::Result<Signature>;
    fn get_link(&self, cmd: GetLinkCommand) -> eyre::Result<(Pubkey, Link)>;
    fn list_link(&self, cmd: ListLinkCommand) -> eyre::Result<HashMap<Pubkey, Link>>;
    fn update_link(&self, cmd: UpdateLinkCommand) -> eyre::Result<Signature>;
    fn delete_link(&self, cmd: DeleteLinkCommand) -> eyre::Result<Signature>;
    fn activate_link(&self, cmd: ActivateLinkCommand) -> eyre::Result<Signature>;
    fn latency_link(&self, cmd: LatencyLinkCommand) -> eyre::Result<Vec<LinkLatencyStats>>;
    fn reject_link(&self, cmd: RejectLinkCommand) -> eyre::Result<Signature>;
    fn closeaccount_link(&self, cmd: CloseAccountLinkCommand) -> eyre::Result<Signature>;
    fn set_link_health(&self, cmd: SetLinkHealthCommand) -> eyre::Result<Signature>;

    fn create_user(&self, cmd: CreateUserCommand) -> eyre::Result<(Signature, Pubkey)>;
    fn create_subscribe_user(
        &self,
        cmd: CreateSubscribeUserCommand,
    ) -> eyre::Result<(Signature, Pubkey)>;
    fn get_user(&self, cmd: GetUserCommand) -> eyre::Result<(Pubkey, User)>;
    fn list_user(&self, cmd: ListUserCommand) -> eyre::Result<HashMap<Pubkey, User>>;
    fn update_user(&self, cmd: UpdateUserCommand) -> eyre::Result<Signature>;
    fn delete_user(&self, cmd: DeleteUserCommand) -> eyre::Result<Signature>;
    fn request_ban_user(&self, cmd: RequestBanUserCommand) -> eyre::Result<Signature>;

    fn list_foundation_allowlist(
        &self,
        cmd: ListFoundationAllowlistCommand,
    ) -> eyre::Result<Vec<Pubkey>>;
    fn add_foundation_allowlist(
        &self,
        cmd: AddFoundationAllowlistCommand,
    ) -> eyre::Result<Signature>;
    fn remove_foundation_allowlist(
        &self,
        cmd: RemoveFoundationAllowlistCommand,
    ) -> eyre::Result<Signature>;
    fn list_qa_allowlist(&self, cmd: ListQaAllowlistCommand) -> eyre::Result<Vec<Pubkey>>;
    fn add_qa_allowlist(&self, cmd: AddQaAllowlistCommand) -> eyre::Result<Signature>;
    fn remove_qa_allowlist(&self, cmd: RemoveQaAllowlistCommand) -> eyre::Result<Signature>;
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
    fn add_multicastgroup_pub_allowlist(
        &self,
        cmd: AddMulticastGroupPubAllowlistCommand,
    ) -> eyre::Result<Signature>;
    fn remove_multicastgroup_pub_allowlist(
        &self,
        cmd: RemoveMulticastGroupPubAllowlistCommand,
    ) -> eyre::Result<Signature>;
    fn add_multicastgroup_sub_allowlist(
        &self,
        cmd: AddMulticastGroupSubAllowlistCommand,
    ) -> eyre::Result<Signature>;
    fn remove_multicastgroup_sub_allowlist(
        &self,
        cmd: RemoveMulticastGroupSubAllowlistCommand,
    ) -> eyre::Result<Signature>;
    fn set_accesspass(&self, cmd: SetAccessPassCommand) -> eyre::Result<Signature>;
    fn get_accesspass(&self, cmd: GetAccessPassCommand) -> eyre::Result<(Pubkey, AccessPass)>;
    fn list_accesspass(
        &self,
        cmd: ListAccessPassCommand,
    ) -> eyre::Result<HashMap<Pubkey, AccessPass>>;
    fn close_accesspass(&self, cmd: CloseAccessPassCommand) -> eyre::Result<Signature>;

    fn allocate_resource(&self, cmd: AllocateResourceCommand) -> eyre::Result<Signature>;
    fn create_resource(&self, cmd: CreateResourceCommand) -> eyre::Result<Signature>;
    fn deallocate_resource(&self, cmd: DeallocateResourceCommand) -> eyre::Result<Signature>;
    fn get_resource(
        &self,
        cmd: GetResourceCommand,
    ) -> eyre::Result<(Pubkey, ResourceExtensionOwned)>;
    fn close_resource(&self, cmd: CloseResourceCommand) -> eyre::Result<Signature>;
}

pub struct CliCommandImpl<'a> {
    client: &'a DZClient,
}

impl CliCommandImpl<'_> {
    pub fn new(client: &DZClient) -> CliCommandImpl<'_> {
        CliCommandImpl { client }
    }
}

impl CliCommand for CliCommandImpl<'_> {
    fn check_requirements(&self, checks: u8) -> eyre::Result<()> {
        crate::requirements::check_requirements(self, None, checks)
    }

    fn get_program_config(
        &self,
        cmd: GetProgramConfigCommand,
    ) -> eyre::Result<(Pubkey, ProgramConfig)> {
        cmd.execute(self.client)
    }

    fn get_program_id(&self) -> Pubkey {
        *self.client.get_program_id()
    }
    fn get_environment(&self) -> Environment {
        self.client.get_environment()
    }
    fn get_payer(&self) -> Pubkey {
        self.client.get_payer()
    }
    fn get_balance(&self) -> eyre::Result<u64> {
        self.client.get_balance()
    }
    fn get_epoch(&self) -> eyre::Result<u64> {
        self.client.get_epoch()
    }
    fn get_logs(&self, pubkey: &Pubkey) -> eyre::Result<Vec<String>> {
        self.client.get_logs(pubkey)
    }
    fn get_account(&self, pubkey: Pubkey) -> eyre::Result<Account> {
        self.client.get_account(pubkey)
    }
    fn get_all(&self) -> eyre::Result<HashMap<Box<Pubkey>, Box<AccountData>>> {
        self.client.get_all()
    }
    fn get_program_accounts(
        &self,
        program_id: &Pubkey,
        config: RpcProgramAccountsConfig,
    ) -> eyre::Result<Vec<(Pubkey, Account)>> {
        self.client.get_program_accounts(program_id, config)
    }

    fn init_globalstate(&self, cmd: InitGlobalStateCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn migrate(&self, cmd: MigrateCommand) -> eyre::Result<Vec<Signature>> {
        cmd.execute(self.client)
    }
    fn get_globalstate(&self, cmd: GetGlobalStateCommand) -> eyre::Result<(Pubkey, GlobalState)> {
        cmd.execute(self.client)
    }
    fn get_globalconfig(
        &self,
        cmd: GetGlobalConfigCommand,
    ) -> eyre::Result<(Pubkey, GlobalConfig)> {
        cmd.execute(self.client)
    }
    fn set_airdrop(&self, cmd: SetAirdropCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn set_authority(&self, cmd: SetAuthorityCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn set_globalconfig(&self, cmd: SetGlobalConfigCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn set_minversion(&self, cmd: SetVersionCommand) -> eyre::Result<Signature> {
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
    fn setdevice_exchange(&self, cmd: SetDeviceExchangeCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn create_contributor(
        &self,
        cmd: CreateContributorCommand,
    ) -> eyre::Result<(Signature, Pubkey)> {
        cmd.execute(self.client)
    }
    fn get_contributor(&self, cmd: GetContributorCommand) -> eyre::Result<(Pubkey, Contributor)> {
        cmd.execute(self.client)
    }
    fn suspend_contributor(&self, cmd: SuspendContributorCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn resume_contributor(&self, cmd: ResumeContributorCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn list_contributor(
        &self,
        cmd: ListContributorCommand,
    ) -> eyre::Result<HashMap<Pubkey, Contributor>> {
        cmd.execute(self.client)
    }
    fn update_contributor(&self, cmd: UpdateContributorCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn delete_contributor(&self, cmd: DeleteContributorCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }

    fn create_tenant(&self, cmd: CreateTenantCommand) -> eyre::Result<(Signature, Pubkey)> {
        cmd.execute(self.client)
    }
    fn get_tenant(&self, cmd: GetTenantCommand) -> eyre::Result<(Pubkey, Tenant)> {
        cmd.execute(self.client)
    }
    fn list_tenant(&self, cmd: ListTenantCommand) -> eyre::Result<HashMap<Pubkey, Tenant>> {
        cmd.execute(self.client)
    }
    fn update_tenant(&self, cmd: UpdateTenantCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn delete_tenant(&self, cmd: DeleteTenantCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn add_administrator_tenant(
        &self,
        cmd: AddAdministratorTenantCommand,
    ) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn remove_administrator_tenant(
        &self,
        cmd: RemoveAdministratorTenantCommand,
    ) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn update_payment_status_tenant(
        &self,
        cmd: UpdatePaymentStatusCommand,
    ) -> eyre::Result<Signature> {
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
    fn set_device_health(&self, cmd: SetDeviceHealthCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn activate_device(&self, cmd: ActivateDeviceCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn reject_device(&self, cmd: RejectDeviceCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn closeaccount_device(&self, cmd: CloseAccountDeviceCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn activate_device_interface(
        &self,
        cmd: ActivateDeviceInterfaceCommand,
    ) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn create_device_interface(
        &self,
        cmd: CreateDeviceInterfaceCommand,
    ) -> eyre::Result<(Signature, Pubkey)> {
        cmd.execute(self.client)
    }
    fn delete_device_interface(
        &self,
        cmd: DeleteDeviceInterfaceCommand,
    ) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn remove_device_interface(
        &self,
        cmd: RemoveDeviceInterfaceCommand,
    ) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn update_device_interface(
        &self,
        cmd: UpdateDeviceInterfaceCommand,
    ) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn create_link(&self, cmd: CreateLinkCommand) -> eyre::Result<(Signature, Pubkey)> {
        cmd.execute(self.client)
    }
    fn accept_link(&self, cmd: AcceptLinkCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn get_link(&self, cmd: GetLinkCommand) -> eyre::Result<(Pubkey, Link)> {
        cmd.execute(self.client)
    }
    fn list_link(&self, cmd: ListLinkCommand) -> eyre::Result<HashMap<Pubkey, Link>> {
        cmd.execute(self.client)
    }
    fn update_link(&self, cmd: UpdateLinkCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn delete_link(&self, cmd: DeleteLinkCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn activate_link(&self, cmd: ActivateLinkCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn latency_link(&self, cmd: LatencyLinkCommand) -> eyre::Result<Vec<LinkLatencyStats>> {
        cmd.execute(self.client)
    }
    fn reject_link(&self, cmd: RejectLinkCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn set_link_health(&self, cmd: SetLinkHealthCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn closeaccount_link(&self, cmd: CloseAccountLinkCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn create_user(&self, cmd: CreateUserCommand) -> eyre::Result<(Signature, Pubkey)> {
        cmd.execute(self.client)
    }
    fn create_subscribe_user(
        &self,
        cmd: CreateSubscribeUserCommand,
    ) -> eyre::Result<(Signature, Pubkey)> {
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
    fn list_qa_allowlist(&self, cmd: ListQaAllowlistCommand) -> eyre::Result<Vec<Pubkey>> {
        cmd.execute(self.client)
    }
    fn add_qa_allowlist(&self, cmd: AddQaAllowlistCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn remove_qa_allowlist(&self, cmd: RemoveQaAllowlistCommand) -> eyre::Result<Signature> {
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
    fn add_multicastgroup_pub_allowlist(
        &self,
        cmd: AddMulticastGroupPubAllowlistCommand,
    ) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn remove_multicastgroup_pub_allowlist(
        &self,
        cmd: RemoveMulticastGroupPubAllowlistCommand,
    ) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn add_multicastgroup_sub_allowlist(
        &self,
        cmd: AddMulticastGroupSubAllowlistCommand,
    ) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn remove_multicastgroup_sub_allowlist(
        &self,
        cmd: RemoveMulticastGroupSubAllowlistCommand,
    ) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn set_accesspass(&self, cmd: SetAccessPassCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn get_accesspass(&self, cmd: GetAccessPassCommand) -> eyre::Result<(Pubkey, AccessPass)> {
        cmd.execute(self.client)
    }
    fn list_accesspass(
        &self,
        cmd: ListAccessPassCommand,
    ) -> eyre::Result<HashMap<Pubkey, AccessPass>> {
        cmd.execute(self.client)
    }
    fn close_accesspass(&self, cmd: CloseAccessPassCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn allocate_resource(&self, cmd: AllocateResourceCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn create_resource(&self, cmd: CreateResourceCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn deallocate_resource(&self, cmd: DeallocateResourceCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
    fn get_resource(
        &self,
        cmd: GetResourceCommand,
    ) -> eyre::Result<(Pubkey, ResourceExtensionOwned)> {
        cmd.execute(self.client)
    }
    fn close_resource(&self, cmd: CloseResourceCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }
}
