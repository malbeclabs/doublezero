use solana_instruction::AccountMeta;
use solana_pubkey::Pubkey;

use crate::state::{AccessRequest, ProgramConfig};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct InitializeProgramAccounts {
    pub payer_key: Pubkey,
    pub new_program_config_key: Pubkey,
}

impl InitializeProgramAccounts {
    pub fn new(payer_key: &Pubkey) -> Self {
        let new_program_config_key = ProgramConfig::find_address().0;

        Self {
            payer_key: *payer_key,
            new_program_config_key,
        }
    }
}

impl From<InitializeProgramAccounts> for Vec<AccountMeta> {
    fn from(accounts: InitializeProgramAccounts) -> Self {
        let InitializeProgramAccounts {
            payer_key,
            new_program_config_key,
        } = accounts;

        vec![
            AccountMeta::new(payer_key, true),
            AccountMeta::new(new_program_config_key, false),
            AccountMeta::new_readonly(solana_system_interface::program::ID, false),
        ]
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetAdminAccounts {
    pub program_data_key: Pubkey,
    pub owner_key: Pubkey,
    pub program_config_key: Pubkey,
}

impl SetAdminAccounts {
    pub fn new(program_data_key: &Pubkey, owner_key: &Pubkey) -> Self {
        Self {
            program_data_key: *program_data_key,
            owner_key: *owner_key,
            program_config_key: ProgramConfig::find_address().0,
        }
    }
}

impl From<SetAdminAccounts> for Vec<AccountMeta> {
    fn from(accounts: SetAdminAccounts) -> Self {
        let SetAdminAccounts {
            program_data_key,
            owner_key,
            program_config_key,
        } = accounts;

        vec![
            AccountMeta::new_readonly(program_data_key, false),
            AccountMeta::new_readonly(owner_key, true),
            AccountMeta::new(program_config_key, false),
        ]
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ConfigureProgramAccounts {
    pub program_config_key: Pubkey,
    pub admin_key: Pubkey,
}

impl ConfigureProgramAccounts {
    pub fn new(admin_key: &Pubkey) -> Self {
        Self {
            program_config_key: ProgramConfig::find_address().0,
            admin_key: *admin_key,
        }
    }
}

impl From<ConfigureProgramAccounts> for Vec<AccountMeta> {
    fn from(accounts: ConfigureProgramAccounts) -> Self {
        let ConfigureProgramAccounts {
            program_config_key,
            admin_key,
        } = accounts;

        vec![
            AccountMeta::new(program_config_key, false),
            AccountMeta::new_readonly(admin_key, true),
        ]
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RequestAccessAccounts {
    pub program_config_key: Pubkey,
    pub payer_key: Pubkey,
    pub new_access_request_key: Pubkey,
}

impl RequestAccessAccounts {
    pub fn new(payer_key: &Pubkey, service_key: &Pubkey) -> Self {
        Self {
            program_config_key: ProgramConfig::find_address().0,
            payer_key: *payer_key,
            new_access_request_key: AccessRequest::find_address(service_key).0,
        }
    }
}

impl From<RequestAccessAccounts> for Vec<AccountMeta> {
    fn from(accounts: RequestAccessAccounts) -> Self {
        let RequestAccessAccounts {
            program_config_key,
            payer_key,
            new_access_request_key,
        } = accounts;

        vec![
            AccountMeta::new_readonly(program_config_key, false),
            AccountMeta::new(payer_key, true),
            AccountMeta::new(new_access_request_key, false),
            AccountMeta::new_readonly(solana_system_interface::program::ID, false),
        ]
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct GrantAccessAccounts {
    pub program_config_key: Pubkey,
    pub dz_ledger_sentinel_key: Pubkey,
    pub access_request_key: Pubkey,
    pub rent_beneficiary_key: Pubkey,
}

impl GrantAccessAccounts {
    pub fn new(
        dz_ledger_sentinel_key: &Pubkey,
        access_request_key: &Pubkey,
        rent_beneficiary_key: &Pubkey,
    ) -> Self {
        Self {
            program_config_key: ProgramConfig::find_address().0,
            dz_ledger_sentinel_key: *dz_ledger_sentinel_key,
            access_request_key: *access_request_key,
            rent_beneficiary_key: *rent_beneficiary_key,
        }
    }
}

impl From<GrantAccessAccounts> for Vec<AccountMeta> {
    fn from(accounts: GrantAccessAccounts) -> Self {
        let GrantAccessAccounts {
            program_config_key,
            dz_ledger_sentinel_key,
            access_request_key,
            rent_beneficiary_key,
        } = accounts;

        vec![
            AccountMeta::new_readonly(program_config_key, false),
            AccountMeta::new(dz_ledger_sentinel_key, true),
            AccountMeta::new(access_request_key, false),
            AccountMeta::new(rent_beneficiary_key, false),
        ]
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DenyAccessAccounts {
    pub program_config_key: Pubkey,
    pub dz_ledger_sentinel_key: Pubkey,
    pub access_request_key: Pubkey,
}

impl DenyAccessAccounts {
    pub fn new(dz_ledger_sentinel_key: &Pubkey, access_request_key: &Pubkey) -> Self {
        Self {
            program_config_key: ProgramConfig::find_address().0,
            dz_ledger_sentinel_key: *dz_ledger_sentinel_key,
            access_request_key: *access_request_key,
        }
    }
}

impl From<DenyAccessAccounts> for Vec<AccountMeta> {
    fn from(accounts: DenyAccessAccounts) -> Self {
        let DenyAccessAccounts {
            program_config_key,
            dz_ledger_sentinel_key,
            access_request_key,
        } = accounts;

        vec![
            AccountMeta::new_readonly(program_config_key, false),
            AccountMeta::new(dz_ledger_sentinel_key, true),
            AccountMeta::new(access_request_key, false),
        ]
    }
}
