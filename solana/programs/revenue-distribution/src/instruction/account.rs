use doublezero_program_tools::get_program_data_address;
use solana_instruction::AccountMeta;
use solana_pubkey::Pubkey;
use solana_system_interface::program as system_program;
use spl_associated_token_account_interface::address::get_associated_token_address;

use crate::{
    state::{
        find_2z_token_pda_address, find_swap_authority_address,
        find_withdraw_sol_authority_address, ContributorRewards, Distribution, Journal,
        ProgramConfig, SolanaValidatorDeposit,
    },
    types::DoubleZeroEpoch,
};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct InitializeProgramAccounts {
    pub payer_key: Pubkey,
    pub new_program_config_key: Pubkey,
    pub new_reserve_2z_key: Pubkey,
    pub dz_mint_key: Pubkey,
}

impl InitializeProgramAccounts {
    pub fn new(payer_key: &Pubkey, dz_mint_key: &Pubkey) -> Self {
        let new_program_config_key = ProgramConfig::find_address().0;

        Self {
            payer_key: *payer_key,
            new_program_config_key,
            new_reserve_2z_key: find_2z_token_pda_address(&new_program_config_key).0,
            dz_mint_key: *dz_mint_key,
        }
    }
}

impl From<InitializeProgramAccounts> for Vec<AccountMeta> {
    fn from(accounts: InitializeProgramAccounts) -> Self {
        let InitializeProgramAccounts {
            payer_key,
            new_program_config_key,
            new_reserve_2z_key,
            dz_mint_key,
        } = accounts;

        vec![
            AccountMeta::new(payer_key, true),
            AccountMeta::new(new_program_config_key, false),
            AccountMeta::new(new_reserve_2z_key, false),
            AccountMeta::new_readonly(dz_mint_key, false),
            AccountMeta::new_readonly(spl_token_interface::ID, false),
            AccountMeta::new_readonly(system_program::ID, false),
        ]
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetAdminAccounts {
    pub program_data_key: Pubkey,
    pub upgrade_authority_key: Pubkey,
    pub program_config_key: Pubkey,
}

impl SetAdminAccounts {
    pub fn new(program_id: &Pubkey, upgrade_authority_key: &Pubkey) -> Self {
        Self {
            program_data_key: get_program_data_address(program_id).0,
            upgrade_authority_key: *upgrade_authority_key,
            program_config_key: ProgramConfig::find_address().0,
        }
    }
}

impl From<SetAdminAccounts> for Vec<AccountMeta> {
    fn from(accounts: SetAdminAccounts) -> Self {
        let SetAdminAccounts {
            program_data_key,
            upgrade_authority_key,
            program_config_key,
        } = accounts;

        vec![
            AccountMeta::new_readonly(program_data_key, false),
            AccountMeta::new_readonly(upgrade_authority_key, true),
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
pub struct InitializeJournalAccounts {
    pub payer_key: Pubkey,
    pub new_journal_key: Pubkey,
    pub new_journal_2z_token_pda_key: Pubkey,
    pub dz_mint_key: Pubkey,
}

impl InitializeJournalAccounts {
    pub fn new(payer_key: &Pubkey, dz_mint_key: &Pubkey) -> Self {
        let new_journal_key = Journal::find_address().0;

        Self {
            payer_key: *payer_key,
            new_journal_key,
            new_journal_2z_token_pda_key: find_2z_token_pda_address(&new_journal_key).0,
            dz_mint_key: *dz_mint_key,
        }
    }
}

impl From<InitializeJournalAccounts> for Vec<AccountMeta> {
    fn from(accounts: InitializeJournalAccounts) -> Self {
        let InitializeJournalAccounts {
            payer_key,
            new_journal_key,
            new_journal_2z_token_pda_key,
            dz_mint_key,
        } = accounts;

        vec![
            AccountMeta::new(payer_key, true),
            AccountMeta::new(new_journal_key, false),
            AccountMeta::new(new_journal_2z_token_pda_key, false),
            AccountMeta::new_readonly(dz_mint_key, false),
            AccountMeta::new_readonly(spl_token_interface::ID, false),
            AccountMeta::new_readonly(system_program::ID, false),
        ]
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ConfigureJournalAccounts {
    pub program_config_key: Pubkey,
    pub admin_key: Pubkey,
    pub journal_key: Pubkey,
}

impl ConfigureJournalAccounts {
    pub fn new(admin_key: &Pubkey) -> Self {
        Self {
            program_config_key: ProgramConfig::find_address().0,
            admin_key: *admin_key,
            journal_key: Journal::find_address().0,
        }
    }
}

impl From<ConfigureJournalAccounts> for Vec<AccountMeta> {
    fn from(accounts: ConfigureJournalAccounts) -> Self {
        let ConfigureJournalAccounts {
            program_config_key,
            admin_key,
            journal_key,
        } = accounts;

        vec![
            AccountMeta::new_readonly(program_config_key, false),
            AccountMeta::new_readonly(admin_key, true),
            AccountMeta::new(journal_key, false),
        ]
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct InitializeDistributionAccounts {
    pub program_config_key: Pubkey,
    pub debt_accountant_key: Pubkey,
    pub payer_key: Pubkey,
    pub new_distribution_key: Pubkey,
    pub new_distribution_2z_token_pda_key: Pubkey,
    pub dz_mint_key: Pubkey,
    pub journal_key: Pubkey,
    pub journal_2z_token_pda_key: Pubkey,
    pub journal_ata_key: Pubkey,
}

impl InitializeDistributionAccounts {
    pub fn new(
        debt_accountant_key: &Pubkey,
        payer_key: &Pubkey,
        dz_epoch: DoubleZeroEpoch,
        dz_mint_key: &Pubkey,
    ) -> Self {
        let new_distribution_key = Distribution::find_address(dz_epoch).0;
        let journal_key = Journal::find_address().0;

        Self {
            program_config_key: ProgramConfig::find_address().0,
            debt_accountant_key: *debt_accountant_key,
            payer_key: *payer_key,
            new_distribution_key,
            new_distribution_2z_token_pda_key: find_2z_token_pda_address(&new_distribution_key).0,
            dz_mint_key: *dz_mint_key,
            journal_key,
            journal_2z_token_pda_key: find_2z_token_pda_address(&journal_key).0,
            journal_ata_key: get_associated_token_address(&journal_key, dz_mint_key),
        }
    }
}

impl From<InitializeDistributionAccounts> for Vec<AccountMeta> {
    fn from(accounts: InitializeDistributionAccounts) -> Self {
        let InitializeDistributionAccounts {
            program_config_key,
            debt_accountant_key,
            payer_key,
            new_distribution_key,
            new_distribution_2z_token_pda_key,
            dz_mint_key,
            journal_key,
            journal_2z_token_pda_key,
            journal_ata_key,
        } = accounts;

        vec![
            AccountMeta::new(program_config_key, false),
            AccountMeta::new_readonly(debt_accountant_key, true),
            AccountMeta::new(payer_key, true),
            AccountMeta::new(new_distribution_key, false),
            AccountMeta::new(new_distribution_2z_token_pda_key, false),
            AccountMeta::new_readonly(dz_mint_key, false),
            AccountMeta::new_readonly(spl_token_interface::ID, false),
            AccountMeta::new(journal_key, false),
            AccountMeta::new(journal_2z_token_pda_key, false),
            AccountMeta::new(journal_ata_key, false),
            AccountMeta::new_readonly(system_program::ID, false),
        ]
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ConfigureDistributionDebtAccounts {
    pub program_config_key: Pubkey,
    pub debt_accountant_key: Pubkey,
    pub distribution_key: Pubkey,
}

impl ConfigureDistributionDebtAccounts {
    pub fn new(debt_accountant_key: &Pubkey, dz_epoch: DoubleZeroEpoch) -> Self {
        Self {
            program_config_key: ProgramConfig::find_address().0,
            debt_accountant_key: *debt_accountant_key,
            distribution_key: Distribution::find_address(dz_epoch).0,
        }
    }
}

impl From<ConfigureDistributionDebtAccounts> for Vec<AccountMeta> {
    fn from(accounts: ConfigureDistributionDebtAccounts) -> Self {
        let ConfigureDistributionDebtAccounts {
            program_config_key,
            debt_accountant_key,
            distribution_key,
        } = accounts;

        vec![
            AccountMeta::new_readonly(program_config_key, false),
            AccountMeta::new_readonly(debt_accountant_key, true),
            AccountMeta::new(distribution_key, false),
        ]
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FinalizeDistributionDebtAccounts {
    pub program_config_key: Pubkey,
    pub debt_accountant_key: Pubkey,
    pub distribution_key: Pubkey,
    pub payer_key: Pubkey,
}

impl FinalizeDistributionDebtAccounts {
    pub fn new(
        debt_accountant_key: &Pubkey,
        dz_epoch: DoubleZeroEpoch,
        payer_key: &Pubkey,
    ) -> Self {
        Self {
            program_config_key: ProgramConfig::find_address().0,
            debt_accountant_key: *debt_accountant_key,
            distribution_key: Distribution::find_address(dz_epoch).0,
            payer_key: *payer_key,
        }
    }
}

impl From<FinalizeDistributionDebtAccounts> for Vec<AccountMeta> {
    fn from(accounts: FinalizeDistributionDebtAccounts) -> Self {
        let FinalizeDistributionDebtAccounts {
            program_config_key,
            debt_accountant_key,
            distribution_key,
            payer_key,
        } = accounts;

        vec![
            AccountMeta::new_readonly(program_config_key, false),
            AccountMeta::new_readonly(debt_accountant_key, true),
            AccountMeta::new(distribution_key, false),
            AccountMeta::new(payer_key, true),
            AccountMeta::new_readonly(system_program::ID, false),
        ]
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ConfigureDistributionRewardsAccounts {
    pub program_config_key: Pubkey,
    pub rewards_accountant_key: Pubkey,
    pub distribution_key: Pubkey,
}

impl ConfigureDistributionRewardsAccounts {
    pub fn new(rewards_accountant_key: &Pubkey, dz_epoch: DoubleZeroEpoch) -> Self {
        Self {
            program_config_key: ProgramConfig::find_address().0,
            rewards_accountant_key: *rewards_accountant_key,
            distribution_key: Distribution::find_address(dz_epoch).0,
        }
    }
}

impl From<ConfigureDistributionRewardsAccounts> for Vec<AccountMeta> {
    fn from(accounts: ConfigureDistributionRewardsAccounts) -> Self {
        let ConfigureDistributionRewardsAccounts {
            program_config_key,
            rewards_accountant_key,
            distribution_key,
        } = accounts;

        vec![
            AccountMeta::new_readonly(program_config_key, false),
            AccountMeta::new_readonly(rewards_accountant_key, true),
            AccountMeta::new(distribution_key, false),
        ]
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FinalizeDistributionRewardsAccounts {
    pub program_config_key: Pubkey,
    pub distribution_key: Pubkey,
    pub payer_key: Pubkey,
}

impl FinalizeDistributionRewardsAccounts {
    pub fn new(payer_key: &Pubkey, dz_epoch: DoubleZeroEpoch) -> Self {
        Self {
            program_config_key: ProgramConfig::find_address().0,
            distribution_key: Distribution::find_address(dz_epoch).0,
            payer_key: *payer_key,
        }
    }
}

impl From<FinalizeDistributionRewardsAccounts> for Vec<AccountMeta> {
    fn from(accounts: FinalizeDistributionRewardsAccounts) -> Self {
        let FinalizeDistributionRewardsAccounts {
            program_config_key,
            distribution_key,
            payer_key,
        } = accounts;

        vec![
            AccountMeta::new_readonly(program_config_key, false),
            AccountMeta::new(distribution_key, false),
            AccountMeta::new(payer_key, true),
            AccountMeta::new_readonly(system_program::ID, false),
        ]
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DistributeRewardsAccounts {
    pub program_config_key: Pubkey,
    pub distribution_key: Pubkey,
    pub contributor_rewards_key: Pubkey,
    pub distribution_2z_token_pda_key: Pubkey,
    pub dz_mint_key: Pubkey,
    pub relayer_key: Pubkey,
    pub recipient_ata_keys: Vec<Pubkey>,
}

impl DistributeRewardsAccounts {
    pub fn new(
        dz_epoch: DoubleZeroEpoch,
        service_key: &Pubkey,
        dz_mint_key: &Pubkey,
        relayer_key: &Pubkey,
        recipient_keys: &[&Pubkey],
    ) -> Self {
        let distribution_key = Distribution::find_address(dz_epoch).0;
        let recipient_ata_keys = recipient_keys
            .iter()
            .map(|owner_key| get_associated_token_address(owner_key, dz_mint_key))
            .collect();

        Self {
            program_config_key: ProgramConfig::find_address().0,
            distribution_key,
            contributor_rewards_key: ContributorRewards::find_address(service_key).0,
            distribution_2z_token_pda_key: find_2z_token_pda_address(&distribution_key).0,
            dz_mint_key: *dz_mint_key,
            relayer_key: *relayer_key,
            recipient_ata_keys,
        }
    }
}

impl From<DistributeRewardsAccounts> for Vec<AccountMeta> {
    fn from(accounts: DistributeRewardsAccounts) -> Self {
        let DistributeRewardsAccounts {
            program_config_key,
            distribution_key,
            contributor_rewards_key,
            distribution_2z_token_pda_key,
            dz_mint_key,
            relayer_key,
            recipient_ata_keys,
        } = accounts;

        let mut accounts = vec![
            AccountMeta::new_readonly(program_config_key, false),
            AccountMeta::new(distribution_key, false),
            AccountMeta::new_readonly(contributor_rewards_key, false),
            AccountMeta::new(distribution_2z_token_pda_key, false),
            AccountMeta::new(dz_mint_key, false),
            AccountMeta::new(relayer_key, false),
            AccountMeta::new_readonly(spl_token_interface::ID, false),
        ];

        let recipient_ata_accounts = recipient_ata_keys
            .into_iter()
            .map(|key| AccountMeta::new(key, false));

        accounts.extend(recipient_ata_accounts);

        accounts
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct InitializeContributorRewardsAccounts {
    pub payer_key: Pubkey,
    pub new_contributor_rewards_key: Pubkey,
}

impl InitializeContributorRewardsAccounts {
    pub fn new(payer_key: &Pubkey, service_key: &Pubkey) -> Self {
        Self {
            payer_key: *payer_key,
            new_contributor_rewards_key: ContributorRewards::find_address(service_key).0,
        }
    }
}

impl From<InitializeContributorRewardsAccounts> for Vec<AccountMeta> {
    fn from(accounts: InitializeContributorRewardsAccounts) -> Self {
        let InitializeContributorRewardsAccounts {
            payer_key,
            new_contributor_rewards_key,
        } = accounts;

        vec![
            AccountMeta::new(payer_key, true),
            AccountMeta::new(new_contributor_rewards_key, false),
            AccountMeta::new_readonly(system_program::ID, false),
        ]
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetRewardsManagerAccounts {
    pub program_config_key: Pubkey,
    pub contributor_manager_key: Pubkey,
    pub contributor_rewards_key: Pubkey,
}

impl SetRewardsManagerAccounts {
    pub fn new(contributor_manager_key: &Pubkey, service_key: &Pubkey) -> Self {
        Self {
            program_config_key: ProgramConfig::find_address().0,
            contributor_manager_key: *contributor_manager_key,
            contributor_rewards_key: ContributorRewards::find_address(service_key).0,
        }
    }
}

impl From<SetRewardsManagerAccounts> for Vec<AccountMeta> {
    fn from(accounts: SetRewardsManagerAccounts) -> Self {
        let SetRewardsManagerAccounts {
            program_config_key,
            contributor_manager_key,
            contributor_rewards_key,
        } = accounts;

        vec![
            AccountMeta::new_readonly(program_config_key, false),
            AccountMeta::new_readonly(contributor_manager_key, true),
            AccountMeta::new(contributor_rewards_key, false),
        ]
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ConfigureContributorRewardsAccounts {
    pub program_config_key: Pubkey,
    pub contributor_rewards_key: Pubkey,
    pub rewards_manager_key: Pubkey,
}

impl ConfigureContributorRewardsAccounts {
    pub fn new(rewards_manager_key: &Pubkey, service_key: &Pubkey) -> Self {
        Self {
            program_config_key: ProgramConfig::find_address().0,
            contributor_rewards_key: ContributorRewards::find_address(service_key).0,
            rewards_manager_key: *rewards_manager_key,
        }
    }
}

impl From<ConfigureContributorRewardsAccounts> for Vec<AccountMeta> {
    fn from(accounts: ConfigureContributorRewardsAccounts) -> Self {
        let ConfigureContributorRewardsAccounts {
            program_config_key,
            contributor_rewards_key,
            rewards_manager_key,
        } = accounts;

        vec![
            AccountMeta::new_readonly(program_config_key, false),
            AccountMeta::new(contributor_rewards_key, false),
            AccountMeta::new_readonly(rewards_manager_key, true),
        ]
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct VerifyDistributionMerkleRootAccounts {
    pub distribution_key: Pubkey,
}

impl VerifyDistributionMerkleRootAccounts {
    pub fn new(dz_epoch: DoubleZeroEpoch) -> Self {
        Self {
            distribution_key: Distribution::find_address(dz_epoch).0,
        }
    }
}

impl From<VerifyDistributionMerkleRootAccounts> for Vec<AccountMeta> {
    fn from(accounts: VerifyDistributionMerkleRootAccounts) -> Self {
        let VerifyDistributionMerkleRootAccounts { distribution_key } = accounts;

        vec![AccountMeta::new_readonly(distribution_key, false)]
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct InitializeSolanaValidatorDepositAccounts {
    pub new_solana_validator_deposit_key: Pubkey,
    pub payer_key: Pubkey,
}

impl InitializeSolanaValidatorDepositAccounts {
    pub fn new(payer_key: &Pubkey, node_id: &Pubkey) -> Self {
        Self {
            new_solana_validator_deposit_key: SolanaValidatorDeposit::find_address(node_id).0,
            payer_key: *payer_key,
        }
    }
}

impl From<InitializeSolanaValidatorDepositAccounts> for Vec<AccountMeta> {
    fn from(accounts: InitializeSolanaValidatorDepositAccounts) -> Self {
        let InitializeSolanaValidatorDepositAccounts {
            new_solana_validator_deposit_key,
            payer_key,
        } = accounts;

        vec![
            AccountMeta::new(new_solana_validator_deposit_key, false),
            AccountMeta::new(payer_key, true),
            AccountMeta::new_readonly(system_program::ID, false),
        ]
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PaySolanaValidatorDebtAccounts {
    pub program_config_key: Pubkey,
    pub distribution_key: Pubkey,
    pub solana_validator_deposit_key: Pubkey,
    pub journal_key: Pubkey,
}

impl PaySolanaValidatorDebtAccounts {
    pub fn new(dz_epoch: DoubleZeroEpoch, node_id: &Pubkey) -> Self {
        Self {
            program_config_key: ProgramConfig::find_address().0,
            distribution_key: Distribution::find_address(dz_epoch).0,
            solana_validator_deposit_key: SolanaValidatorDeposit::find_address(node_id).0,
            journal_key: Journal::find_address().0,
        }
    }
}

impl From<PaySolanaValidatorDebtAccounts> for Vec<AccountMeta> {
    fn from(accounts: PaySolanaValidatorDebtAccounts) -> Self {
        let PaySolanaValidatorDebtAccounts {
            program_config_key,
            distribution_key,
            solana_validator_deposit_key,
            journal_key,
        } = accounts;

        vec![
            AccountMeta::new_readonly(program_config_key, false),
            AccountMeta::new(distribution_key, false),
            AccountMeta::new(solana_validator_deposit_key, false),
            AccountMeta::new(journal_key, false),
        ]
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct EnableSolanaValidatorDebtWriteOffAccounts {
    pub program_config_key: Pubkey,
    pub distribution_key: Pubkey,
    pub payer_key: Pubkey,
}

impl EnableSolanaValidatorDebtWriteOffAccounts {
    pub fn new(dz_epoch: DoubleZeroEpoch, payer_key: &Pubkey) -> Self {
        Self {
            program_config_key: ProgramConfig::find_address().0,
            distribution_key: Distribution::find_address(dz_epoch).0,
            payer_key: *payer_key,
        }
    }
}

impl From<EnableSolanaValidatorDebtWriteOffAccounts> for Vec<AccountMeta> {
    fn from(accounts: EnableSolanaValidatorDebtWriteOffAccounts) -> Self {
        let EnableSolanaValidatorDebtWriteOffAccounts {
            program_config_key,
            distribution_key,
            payer_key,
        } = accounts;

        vec![
            AccountMeta::new_readonly(program_config_key, false),
            AccountMeta::new(distribution_key, false),
            AccountMeta::new(payer_key, true),
            AccountMeta::new_readonly(system_program::ID, false),
        ]
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct WriteOffSolanaValidatorDebtAccounts {
    pub program_config_key: Pubkey,
    pub debt_accountant_key: Pubkey,
    pub distribution_key: Pubkey,
    pub solana_validator_deposit_key: Pubkey,
    pub write_off_distribution_key: Pubkey,
}

impl WriteOffSolanaValidatorDebtAccounts {
    pub fn new(
        debt_accountant_key: &Pubkey,
        dz_epoch: DoubleZeroEpoch,
        node_id: &Pubkey,
        write_off_dz_epoch: DoubleZeroEpoch,
    ) -> Self {
        Self {
            program_config_key: ProgramConfig::find_address().0,
            debt_accountant_key: *debt_accountant_key,
            distribution_key: Distribution::find_address(dz_epoch).0,
            solana_validator_deposit_key: SolanaValidatorDeposit::find_address(node_id).0,
            write_off_distribution_key: Distribution::find_address(write_off_dz_epoch).0,
        }
    }
}

impl From<WriteOffSolanaValidatorDebtAccounts> for Vec<AccountMeta> {
    fn from(accounts: WriteOffSolanaValidatorDebtAccounts) -> Self {
        let WriteOffSolanaValidatorDebtAccounts {
            program_config_key,
            debt_accountant_key,
            distribution_key,
            solana_validator_deposit_key,
            write_off_distribution_key,
        } = accounts;

        vec![
            AccountMeta::new_readonly(program_config_key, false),
            AccountMeta::new_readonly(debt_accountant_key, true),
            AccountMeta::new(distribution_key, false),
            AccountMeta::new(solana_validator_deposit_key, false),
            AccountMeta::new(write_off_distribution_key, false),
        ]
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct InitializeSwapDestinationAccounts {
    pub program_config_key: Pubkey,
    pub payer_key: Pubkey,
    pub swap_authority_key: Pubkey,
    pub new_swap_destination_key: Pubkey,
    pub mint_key: Pubkey,
}

impl InitializeSwapDestinationAccounts {
    pub fn new(payer_key: &Pubkey, mint_key: &Pubkey) -> Self {
        let swap_authority_key = find_swap_authority_address().0;

        Self {
            program_config_key: ProgramConfig::find_address().0,
            payer_key: *payer_key,
            swap_authority_key,
            new_swap_destination_key: find_2z_token_pda_address(&swap_authority_key).0,
            mint_key: *mint_key,
        }
    }
}

impl From<InitializeSwapDestinationAccounts> for Vec<AccountMeta> {
    fn from(accounts: InitializeSwapDestinationAccounts) -> Self {
        let InitializeSwapDestinationAccounts {
            program_config_key,
            payer_key,
            swap_authority_key,
            new_swap_destination_key,
            mint_key,
        } = accounts;

        vec![
            AccountMeta::new(program_config_key, false),
            AccountMeta::new(payer_key, true),
            AccountMeta::new_readonly(swap_authority_key, false),
            AccountMeta::new(new_swap_destination_key, false),
            AccountMeta::new_readonly(mint_key, false),
            AccountMeta::new_readonly(spl_token_interface::ID, false),
            AccountMeta::new_readonly(system_program::ID, false),
        ]
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DequeueFillsCpiAccounts {
    pub configuration_registry_key: Pubkey,
    pub program_state_key: Pubkey,
    pub fills_registry_key: Pubkey,
    pub journal_key: Pubkey,
    pub sol_2z_swap_program_id: Option<Pubkey>,
}

impl DequeueFillsCpiAccounts {
    pub fn new(sol_2z_swap_program_id: &Pubkey, fills_registery_key: &Pubkey) -> Self {
        Self {
            configuration_registry_key: Pubkey::find_program_address(
                &[b"system_config"],
                sol_2z_swap_program_id,
            )
            .0,
            program_state_key: Pubkey::find_program_address(&[b"state"], sol_2z_swap_program_id).0,
            fills_registry_key: *fills_registery_key,
            journal_key: Journal::find_address().0,
            sol_2z_swap_program_id: Some(*sol_2z_swap_program_id),
        }
    }
}

impl From<DequeueFillsCpiAccounts> for Vec<AccountMeta> {
    fn from(accounts: DequeueFillsCpiAccounts) -> Self {
        let DequeueFillsCpiAccounts {
            configuration_registry_key,
            program_state_key,
            fills_registry_key,
            journal_key,
            sol_2z_swap_program_id: _,
        } = accounts;

        vec![
            AccountMeta::new_readonly(configuration_registry_key, false),
            AccountMeta::new_readonly(program_state_key, false),
            AccountMeta::new(fills_registry_key, false),
            AccountMeta::new_readonly(journal_key, true),
        ]
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SweepDistributionTokensAccounts {
    pub program_config_key: Pubkey,
    pub distribution_key: Pubkey,
    pub journal_key: Pubkey,
    pub dequeue_fills_cpi_keys: DequeueFillsCpiAccounts,
    pub distribution_2z_token_pda_key: Pubkey,
    pub swap_authority_key: Pubkey,
    pub swap_2z_token_pda_key: Pubkey,
}

impl SweepDistributionTokensAccounts {
    pub fn new(
        dz_epoch: DoubleZeroEpoch,
        sol_2z_swap_program_id: &Pubkey,
        sol_2z_swap_fills_registry_key: &Pubkey,
    ) -> Self {
        let distribution_key = Distribution::find_address(dz_epoch).0;
        let swap_authority_key = find_swap_authority_address().0;

        let dequeue_fills_cpi_keys =
            DequeueFillsCpiAccounts::new(sol_2z_swap_program_id, sol_2z_swap_fills_registry_key);

        Self {
            program_config_key: ProgramConfig::find_address().0,
            distribution_key,
            journal_key: Journal::find_address().0,
            dequeue_fills_cpi_keys,
            distribution_2z_token_pda_key: find_2z_token_pda_address(&distribution_key).0,
            swap_authority_key,
            swap_2z_token_pda_key: find_2z_token_pda_address(&swap_authority_key).0,
        }
    }
}

impl From<SweepDistributionTokensAccounts> for Vec<AccountMeta> {
    fn from(accounts: SweepDistributionTokensAccounts) -> Self {
        let SweepDistributionTokensAccounts {
            program_config_key,
            distribution_key,
            journal_key,
            dequeue_fills_cpi_keys,
            distribution_2z_token_pda_key,
            swap_authority_key,
            swap_2z_token_pda_key,
        } = accounts;

        // This method assumes that the dequeue fills CPI accounts were created
        // using the `new` method, so this unwrap could fail if the struct were
        // created by populating its members directly and the SOL/2Z Swap
        // program ID was not provided.
        let sol_2z_swap_program_id = dequeue_fills_cpi_keys.sol_2z_swap_program_id.unwrap();

        let mut dequeue_fills_cpi_accounts = Vec::from(dequeue_fills_cpi_keys);

        // Drop the journal account from the dequeue fills CPI accounts.
        dequeue_fills_cpi_accounts.pop().unwrap();

        let sol_2z_swap_fills_registry_account_meta = dequeue_fills_cpi_accounts.pop().unwrap();
        let sol_2z_swap_program_state_account_meta = dequeue_fills_cpi_accounts.pop().unwrap();
        let sol_2z_swap_configuration_registry_account_meta =
            dequeue_fills_cpi_accounts.pop().unwrap();
        debug_assert!(dequeue_fills_cpi_accounts.is_empty());

        vec![
            AccountMeta::new_readonly(program_config_key, false),
            AccountMeta::new(distribution_key, false),
            AccountMeta::new(journal_key, false),
            sol_2z_swap_configuration_registry_account_meta,
            sol_2z_swap_program_state_account_meta,
            sol_2z_swap_fills_registry_account_meta,
            AccountMeta::new_readonly(sol_2z_swap_program_id, false),
            AccountMeta::new(distribution_2z_token_pda_key, false),
            AccountMeta::new_readonly(swap_authority_key, false),
            AccountMeta::new(swap_2z_token_pda_key, false),
            AccountMeta::new_readonly(spl_token_interface::ID, false),
        ]
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct WithdrawSolAccounts {
    pub program_config_key: Pubkey,
    pub withdraw_sol_authority_key: Pubkey,
    pub journal_key: Pubkey,
    pub sol_destination_key: Pubkey,
}

impl WithdrawSolAccounts {
    /// NOTE: The swap program should not use this method when invoking the
    /// withdraw SOL instruction because the find program address calls cost
    /// 1,500 CU per bump iteration. It is recommended to instantiate the
    /// struct by defining its members directly. Please only use this method
    /// for testing purposes.
    pub fn new(sol_2z_swap_program_id: &Pubkey, sol_destination_key: &Pubkey) -> Self {
        Self {
            program_config_key: ProgramConfig::find_address().0,
            withdraw_sol_authority_key: find_withdraw_sol_authority_address(sol_2z_swap_program_id)
                .0,
            journal_key: Journal::find_address().0,
            sol_destination_key: *sol_destination_key,
        }
    }
}

impl From<WithdrawSolAccounts> for Vec<AccountMeta> {
    fn from(accounts: WithdrawSolAccounts) -> Self {
        let WithdrawSolAccounts {
            program_config_key,
            withdraw_sol_authority_key,
            journal_key,
            sol_destination_key,
        } = accounts;

        vec![
            AccountMeta::new_readonly(program_config_key, false),
            AccountMeta::new_readonly(withdraw_sol_authority_key, true),
            AccountMeta::new(journal_key, false),
            AccountMeta::new(sol_destination_key, false),
        ]
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_from_sweep_distribution_tokens() {
        let accounts = SweepDistributionTokensAccounts::new(
            DoubleZeroEpoch::new(69),
            &Pubkey::new_unique(),
            &Pubkey::new_unique(),
        );

        // Debug assert should not panic.
        let accounts = Vec::from(accounts);
        assert_eq!(accounts.len(), 11);
    }
}
