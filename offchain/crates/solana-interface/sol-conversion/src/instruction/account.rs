use doublezero_program_tools::get_program_data_address;
use doublezero_revenue_distribution::state::find_withdraw_sol_authority_address;
use solana_instruction::AccountMeta;
use solana_pubkey::Pubkey;

use crate::{
    ID,
    state::{ConfigurationRegistry, DenyListRegistry, ProgramState},
};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct InitializeSystemAccounts {
    pub new_configuration_registry_key: Pubkey,
    pub new_program_state_key: Pubkey,
    pub new_deny_list_registry_key: Pubkey,
    pub fills_registry_key: Pubkey,
    pub withdraw_authority_key: Pubkey,
    pub program_data_key: Pubkey,
    pub upgrade_authority_key: Pubkey,
}

impl InitializeSystemAccounts {
    pub fn new(fills_registry_key: &Pubkey, upgrade_authority_key: &Pubkey) -> Self {
        Self {
            new_configuration_registry_key: ConfigurationRegistry::find_address().0,
            new_program_state_key: ProgramState::find_address().0,
            new_deny_list_registry_key: DenyListRegistry::find_address().0,
            fills_registry_key: *fills_registry_key,
            withdraw_authority_key: find_withdraw_sol_authority_address(&ID).0,
            program_data_key: get_program_data_address(&ID).0,
            upgrade_authority_key: *upgrade_authority_key,
        }
    }
}

impl From<InitializeSystemAccounts> for Vec<AccountMeta> {
    fn from(accounts: InitializeSystemAccounts) -> Self {
        let InitializeSystemAccounts {
            new_configuration_registry_key,
            new_program_state_key,
            new_deny_list_registry_key,
            fills_registry_key,
            withdraw_authority_key,
            program_data_key,
            upgrade_authority_key,
        } = accounts;

        vec![
            AccountMeta::new(new_configuration_registry_key, false),
            AccountMeta::new(new_program_state_key, false),
            AccountMeta::new(new_deny_list_registry_key, false),
            AccountMeta::new(fills_registry_key, false),
            AccountMeta::new_readonly(withdraw_authority_key, false),
            AccountMeta::new_readonly(ID, false),
            AccountMeta::new_readonly(program_data_key, false),
            AccountMeta::new_readonly(solana_system_interface::program::ID, false),
            AccountMeta::new(upgrade_authority_key, true),
        ]
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UpdateConfigurationRegistryAccounts {
    pub configuration_registry_key: Pubkey,
    pub program_state_key: Pubkey,
    pub admin_key: Pubkey,
}

impl UpdateConfigurationRegistryAccounts {
    pub fn new(admin_key: &Pubkey) -> Self {
        Self {
            configuration_registry_key: ConfigurationRegistry::find_address().0,
            program_state_key: ProgramState::find_address().0,
            admin_key: *admin_key,
        }
    }
}

impl From<UpdateConfigurationRegistryAccounts> for Vec<AccountMeta> {
    fn from(accounts: UpdateConfigurationRegistryAccounts) -> Self {
        let UpdateConfigurationRegistryAccounts {
            configuration_registry_key,
            program_state_key,
            admin_key,
        } = accounts;

        vec![
            AccountMeta::new(configuration_registry_key, false),
            AccountMeta::new_readonly(program_state_key, false),
            AccountMeta::new_readonly(admin_key, true),
        ]
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetFillsConsumerAccounts {
    pub configuration_registry_key: Pubkey,
    pub program_state_key: Pubkey,
    pub admin_key: Pubkey,
}

impl SetFillsConsumerAccounts {
    pub fn new(admin_key: &Pubkey) -> Self {
        Self {
            configuration_registry_key: ConfigurationRegistry::find_address().0,
            program_state_key: ProgramState::find_address().0,
            admin_key: *admin_key,
        }
    }
}

impl From<SetFillsConsumerAccounts> for Vec<AccountMeta> {
    fn from(accounts: SetFillsConsumerAccounts) -> Self {
        let SetFillsConsumerAccounts {
            configuration_registry_key,
            program_state_key,
            admin_key,
        } = accounts;

        vec![
            AccountMeta::new(configuration_registry_key, false),
            AccountMeta::new_readonly(program_state_key, false),
            AccountMeta::new_readonly(admin_key, true),
        ]
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetAdminAccounts {
    pub upgrade_authority_key: Pubkey,
    pub program_state_key: Pubkey,
    pub program_data_key: Pubkey,
}

impl SetAdminAccounts {
    pub fn new(upgrade_authority_key: &Pubkey) -> Self {
        Self {
            upgrade_authority_key: *upgrade_authority_key,
            program_state_key: ProgramState::find_address().0,
            program_data_key: get_program_data_address(&ID).0,
        }
    }
}

impl From<SetAdminAccounts> for Vec<AccountMeta> {
    fn from(accounts: SetAdminAccounts) -> Self {
        let SetAdminAccounts {
            upgrade_authority_key,
            program_state_key,
            program_data_key,
        } = accounts;

        vec![
            AccountMeta::new_readonly(upgrade_authority_key, true),
            AccountMeta::new(program_state_key, false),
            AccountMeta::new_readonly(ID, false),
            AccountMeta::new_readonly(program_data_key, false),
        ]
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ToggleSystemStateAccounts {
    pub admin_key: Pubkey,
    pub program_state_key: Pubkey,
}

impl ToggleSystemStateAccounts {
    pub fn new(admin_key: &Pubkey) -> Self {
        Self {
            admin_key: *admin_key,
            program_state_key: ProgramState::find_address().0,
        }
    }
}

impl From<ToggleSystemStateAccounts> for Vec<AccountMeta> {
    fn from(accounts: ToggleSystemStateAccounts) -> Self {
        let ToggleSystemStateAccounts {
            admin_key,
            program_state_key,
        } = accounts;

        vec![
            AccountMeta::new_readonly(admin_key, true),
            AccountMeta::new(program_state_key, false),
        ]
    }
}
