#![allow(dead_code)]

use doublezero_program_tools::{
    instruction::try_build_instruction, zero_copy::checked_from_bytes_with_discriminator,
};
use doublezero_revenue_distribution::{
    instruction::{
        account::{
            ConfigureContributorRewardsAccounts, ConfigureDistributionDebtAccounts,
            ConfigureDistributionRewardsAccounts, ConfigureProgramAccounts,
            DistributeRewardsAccounts, FinalizeDistributionDebtAccounts,
            FinalizeDistributionRewardsAccounts, ForgiveSolanaValidatorDebtAccounts,
            InitializeContributorRewardsAccounts, InitializeDistributionAccounts,
            InitializeJournalAccounts, InitializeProgramAccounts,
            InitializeSolanaValidatorDepositAccounts, InitializeSwapDestinationAccounts,
            PaySolanaValidatorDebtAccounts, SetAdminAccounts, SetRewardsManagerAccounts,
            SweepDistributionTokensAccounts, VerifyDistributionMerkleRootAccounts,
        },
        ContributorRewardsConfiguration, DistributionMerkleRootKind, ProgramConfiguration,
        RevenueDistributionInstructionData,
    },
    state::{
        self, ContributorRewards, Distribution, Journal, ProgramConfig, SolanaValidatorDeposit,
    },
    types::{DoubleZeroEpoch, RewardShare, SolanaValidatorDebt},
    DOUBLEZERO_MINT_KEY, ID,
};
use solana_loader_v3_interface::{get_program_data_address, state::UpgradeableLoaderState};
use solana_program_pack::Pack;
use solana_program_test::{
    BanksClient, BanksClientError, ProgramTest, ProgramTestBanksClientExt, ProgramTestContext,
};
use solana_pubkey::Pubkey;
use solana_sdk::{
    account::Account,
    clock::Clock,
    hash::Hash,
    instruction::Instruction,
    message::{v0::Message, VersionedMessage},
    signature::{Keypair, Signer},
    transaction::{TransactionError, VersionedTransaction},
};
use spl_token::{
    instruction as token_instruction,
    state::{Account as TokenAccount, AccountState as SplTokenAccountState, Mint},
};
use svm_hash::merkle::MerkleProof;

pub const TOTAL_2Z_SUPPLY: u64 = 10_000_000_000 * u64::pow(10, 8);

pub struct TestAccount {
    pub key: Pubkey,
    pub info: Account,
}

pub struct ProgramTestWithOwner {
    pub context: ProgramTestContext,
    pub owner_signer: Keypair,
    pub treasury_2z_key: Pubkey,
    pub sol_2z_swap_fills_registry_key: Pubkey,
}

pub async fn start_test_with_accounts(accounts: Vec<TestAccount>) -> ProgramTestWithOwner {
    let mut program_test = ProgramTest::new("doublezero_revenue_distribution", ID, None);
    program_test.prefer_bpf(true);

    program_test.add_program("mock_swap_sol_2z", mock_swap_sol_2z::ID, None);

    let owner_signer = Keypair::new();

    // Fake the BPF Upgradeable Program's program data account for the Revenue Distribution Program.
    let program_data_acct = Account {
        lamports: 69,
        data: bincode::serialize(&UpgradeableLoaderState::ProgramData {
            slot: 0,
            upgrade_authority_address: Some(owner_signer.pubkey()),
        })
        .unwrap(),
        ..Default::default()
    };
    program_test.add_account(get_program_data_address(&ID), program_data_acct);

    let mint_data = Mint {
        mint_authority: owner_signer.pubkey().into(),
        supply: TOTAL_2Z_SUPPLY,
        decimals: 8,
        is_initialized: true,
        freeze_authority: owner_signer.pubkey().into(),
    };

    let mut mint_account_data = vec![0; Mint::LEN];
    mint_data.pack_into_slice(&mut mint_account_data);

    // Add the 2Z mint.
    let mint_acct = Account {
        lamports: 69,
        owner: spl_token::ID,
        data: mint_account_data,
        ..Default::default()
    };
    program_test.add_account(DOUBLEZERO_MINT_KEY, mint_acct);

    let treasury_token_account_data = TokenAccount {
        mint: DOUBLEZERO_MINT_KEY,
        owner: owner_signer.pubkey(),
        amount: TOTAL_2Z_SUPPLY,
        state: SplTokenAccountState::Initialized,
        ..Default::default()
    };

    let mut treasury_account_data = vec![0; TokenAccount::LEN];
    treasury_token_account_data.pack_into_slice(&mut treasury_account_data);

    let treasury_2z_key = Pubkey::new_unique();

    // Add 2Z test treasury.
    let treasury_token_acct = Account {
        lamports: 69,
        owner: spl_token::ID,
        data: treasury_account_data,
        ..Default::default()
    };
    program_test.add_account(treasury_2z_key, treasury_token_acct);

    for TestAccount { key, info } in accounts.into_iter() {
        program_test.add_account(key, info);
    }

    let mut context = program_test.start_with_context().await;

    let banks_client = &mut context.banks_client;
    let payer_signer = &context.payer;
    let cached_blockhash = &context.last_blockhash;

    let sol_2z_swap_fills_registry_signer = Keypair::new();
    let sol_2z_swap_fills_registry_key = sol_2z_swap_fills_registry_signer.pubkey();

    // Initialize the mock swap sol 2z program's fills tracker.
    let cached_blockhash = {
        let (create_account_ix, initialize_fills_tracker_ix) =
            mock_swap_sol_2z::instruction::create_and_initialize_fills_tracker(
                &payer_signer.pubkey(),
                &sol_2z_swap_fills_registry_key,
            );

        process_instructions_for_test(
            banks_client,
            cached_blockhash,
            &[create_account_ix, initialize_fills_tracker_ix],
            &[payer_signer, &sol_2z_swap_fills_registry_signer],
        )
        .await
        .unwrap()
    };

    context.last_blockhash = cached_blockhash;

    ProgramTestWithOwner {
        context,
        owner_signer,
        treasury_2z_key,
        sol_2z_swap_fills_registry_key,
    }
}

pub async fn start_test() -> ProgramTestWithOwner {
    start_test_with_accounts(Default::default()).await
}

pub fn generate_token_accounts_for_test(mint_key: &Pubkey, owners: &[Pubkey]) -> Vec<TestAccount> {
    owners
        .iter()
        .map(|&owner| {
            let token_account = TokenAccount {
                mint: *mint_key,
                owner,
                state: SplTokenAccountState::Initialized,
                ..Default::default()
            };

            let mut token_account_data = vec![0; TokenAccount::LEN];
            token_account.pack_into_slice(&mut token_account_data);

            TestAccount {
                key: Pubkey::new_unique(),
                info: Account {
                    lamports: 69,
                    owner: spl_token::ID,
                    data: token_account_data,
                    ..Default::default()
                },
            }
        })
        .collect()
}

pub struct IndexedProgramLog<'a> {
    pub index: usize,
    pub message: &'a str,
}

impl ProgramTestWithOwner {
    pub fn payer_signer(&self) -> &Keypair {
        &self.context.payer
    }

    pub async fn get_clock(&self) -> Clock {
        self.context
            .banks_client
            .get_sysvar::<Clock>()
            .await
            .unwrap()
    }

    pub async fn warp_timestamp_by(&mut self, seconds: u32) -> Result<&mut Self, BanksClientError> {
        let mut clock = self.get_clock().await;
        clock.unix_timestamp += i64::from(seconds);
        self.context.set_sysvar::<Clock>(&clock);

        Ok(self)
    }

    pub async fn get_latest_blockhash(&mut self) -> Result<Hash, BanksClientError> {
        self.context
            .get_new_latest_blockhash()
            .await
            .map_err(Into::into)
    }

    // TODO: Is there a better way to do this?
    pub async fn unwrap_simulation_error(
        &mut self,
        instructions: &[Instruction],
        signers: &[&Keypair],
    ) -> (TransactionError, Vec<String>) {
        let recent_blockhash = self.get_latest_blockhash().await.unwrap();

        let payer_signer = &self.context.payer;

        let mut tx_signers = vec![payer_signer];
        tx_signers.extend_from_slice(signers);

        let transaction = new_transaction(instructions, &tx_signers, recent_blockhash);

        let simulated_tx = self
            .context
            .banks_client
            .simulate_transaction(transaction)
            .await
            .unwrap();

        let tx_err = simulated_tx.result.unwrap().unwrap_err();

        self.context.last_blockhash = recent_blockhash;

        (tx_err, simulated_tx.simulation_details.unwrap().logs)
    }

    pub async fn transfer_lamports(
        &mut self,
        dst_key: &Pubkey,
        amount: u64,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.context.payer;

        let transfer_ix =
            solana_system_interface::instruction::transfer(&payer_signer.pubkey(), dst_key, amount);

        self.context.last_blockhash = process_instructions_for_test(
            &mut self.context.banks_client,
            &self.context.last_blockhash,
            &[transfer_ix],
            &[payer_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn create_2z_ata(
        &mut self,
        owner_key: &Pubkey,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.context.payer;
        let payer_key = payer_signer.pubkey();

        // No consequence if the ATA already exists.
        let create_ix = spl_associated_token_account_interface::instruction::create_associated_token_account_idempotent(
            &payer_key,
            owner_key,
            &DOUBLEZERO_MINT_KEY,
            &spl_token::ID,
        );

        self.context.last_blockhash = process_instructions_for_test(
            &mut self.context.banks_client,
            &self.context.last_blockhash,
            &[create_ix],
            &[payer_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn transfer_2z(
        &mut self,
        dst_token_account_key: &Pubkey,
        amount: u64,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.context.payer;
        let owner_signer = &self.owner_signer;

        let token_transfer_ix = token_instruction::transfer(
            &spl_token::ID,
            &self.treasury_2z_key,
            dst_token_account_key,
            &owner_signer.pubkey(),
            &[],
            amount,
        )
        .unwrap();

        self.context.last_blockhash = process_instructions_for_test(
            &mut self.context.banks_client,
            &self.context.last_blockhash,
            &[token_transfer_ix],
            &[payer_signer, owner_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn initialize_program(&mut self) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.context.payer;
        let program_config_key = ProgramConfig::find_address().0;

        let initialize_program_ix = try_build_instruction(
            &ID,
            InitializeProgramAccounts::new(&payer_signer.pubkey(), &DOUBLEZERO_MINT_KEY),
            &RevenueDistributionInstructionData::InitializeProgram,
        )
        .unwrap();

        let remove_me_ix = solana_system_interface::instruction::transfer(
            &payer_signer.pubkey(),
            &program_config_key,
            1,
        );

        self.context.last_blockhash = process_instructions_for_test(
            &mut self.context.banks_client,
            &self.context.last_blockhash,
            &[remove_me_ix, initialize_program_ix],
            &[payer_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn set_admin(&mut self, admin_key: &Pubkey) -> Result<&mut Self, BanksClientError> {
        let owner_signer = &self.owner_signer;
        let payer_signer = &self.context.payer;

        let set_admin_ix = try_build_instruction(
            &ID,
            SetAdminAccounts::new(&ID, &owner_signer.pubkey()),
            &RevenueDistributionInstructionData::SetAdmin(*admin_key),
        )
        .unwrap();

        self.context.last_blockhash = process_instructions_for_test(
            &mut self.context.banks_client,
            &self.context.last_blockhash,
            &[set_admin_ix],
            &[payer_signer, owner_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn configure_program<const N: usize>(
        &mut self,
        admin_signer: &Keypair,
        settings: [ProgramConfiguration; N],
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.context.payer;

        let configure_program_ixs = settings
            .into_iter()
            .map(|setting| {
                try_build_instruction(
                    &ID,
                    ConfigureProgramAccounts::new(&admin_signer.pubkey()),
                    &RevenueDistributionInstructionData::ConfigureProgram(setting),
                )
                .unwrap()
            })
            .collect::<Vec<_>>();

        self.context.last_blockhash = process_instructions_for_test(
            &mut self.context.banks_client,
            &self.context.last_blockhash,
            &configure_program_ixs,
            &[payer_signer, admin_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn initialize_journal(&mut self) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.context.payer;
        let journal_key = Journal::find_address().0;

        let initialize_journal_ix = try_build_instruction(
            &ID,
            InitializeJournalAccounts::new(&payer_signer.pubkey(), &DOUBLEZERO_MINT_KEY),
            &RevenueDistributionInstructionData::InitializeJournal,
        )
        .unwrap();

        let remove_me_ix =
            solana_system_interface::instruction::transfer(&payer_signer.pubkey(), &journal_key, 1);

        self.context.last_blockhash = process_instructions_for_test(
            &mut self.context.banks_client,
            &self.context.last_blockhash,
            &[remove_me_ix, initialize_journal_ix],
            &[payer_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn initialize_distribution(
        &mut self,
        accountant_signer: &Keypair,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.context.payer;

        let (_, program_config, _) = self.fetch_program_config().await;

        let initialize_distribution_ix = try_build_instruction(
            &ID,
            InitializeDistributionAccounts::new(
                &accountant_signer.pubkey(),
                &payer_signer.pubkey(),
                program_config.next_dz_epoch,
                &DOUBLEZERO_MINT_KEY,
            ),
            &RevenueDistributionInstructionData::InitializeDistribution,
        )
        .unwrap();

        self.context.last_blockhash = process_instructions_for_test(
            &mut self.context.banks_client,
            &self.context.last_blockhash,
            &[initialize_distribution_ix],
            &[payer_signer, accountant_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn configure_distribution_debt(
        &mut self,
        dz_epoch: DoubleZeroEpoch,
        debt_accountant_signer: &Keypair,
        total_validators: u32,
        total_debt: u64,
        merkle_root: Hash,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.context.payer;

        let configure_distribution_debt_ix = try_build_instruction(
            &ID,
            ConfigureDistributionDebtAccounts::new(&debt_accountant_signer.pubkey(), dz_epoch),
            &RevenueDistributionInstructionData::ConfigureDistributionDebt {
                total_validators,
                total_debt,
                merkle_root,
            },
        )
        .unwrap();

        self.context.last_blockhash = process_instructions_for_test(
            &mut self.context.banks_client,
            &self.context.last_blockhash,
            &[configure_distribution_debt_ix],
            &[payer_signer, debt_accountant_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn finalize_distribution_debt(
        &mut self,
        dz_epoch: DoubleZeroEpoch,
        debt_accountant_signer: &Keypair,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.context.payer;

        let finalize_distribution_debt_ix = try_build_instruction(
            &ID,
            FinalizeDistributionDebtAccounts::new(
                &debt_accountant_signer.pubkey(),
                dz_epoch,
                &payer_signer.pubkey(),
            ),
            &RevenueDistributionInstructionData::FinalizeDistributionDebt,
        )
        .unwrap();

        self.context.last_blockhash = process_instructions_for_test(
            &mut self.context.banks_client,
            &self.context.last_blockhash,
            &[finalize_distribution_debt_ix],
            &[payer_signer, debt_accountant_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn configure_distribution_rewards(
        &mut self,
        dz_epoch: DoubleZeroEpoch,
        accountant_signer: &Keypair,
        total_contributors: u32,
        merkle_root: Hash,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.context.payer;

        let configure_distribution_rewards_ix = try_build_instruction(
            &ID,
            ConfigureDistributionRewardsAccounts::new(&accountant_signer.pubkey(), dz_epoch),
            &RevenueDistributionInstructionData::ConfigureDistributionRewards {
                total_contributors,
                merkle_root,
            },
        )
        .unwrap();

        self.context.last_blockhash = process_instructions_for_test(
            &mut self.context.banks_client,
            &self.context.last_blockhash,
            &[configure_distribution_rewards_ix],
            &[payer_signer, accountant_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn finalize_distribution_rewards(
        &mut self,
        dz_epoch: DoubleZeroEpoch,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.context.payer;

        let finalize_distribution_rewards_ix = try_build_instruction(
            &ID,
            FinalizeDistributionRewardsAccounts::new(&payer_signer.pubkey(), dz_epoch),
            &RevenueDistributionInstructionData::FinalizeDistributionRewards,
        )
        .unwrap();

        self.context.last_blockhash = process_instructions_for_test(
            &mut self.context.banks_client,
            &self.context.last_blockhash,
            &[finalize_distribution_rewards_ix],
            &[payer_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn distribute_rewards(
        &mut self,
        dz_epoch: DoubleZeroEpoch,
        reward_share: &RewardShare,
        dz_mint_key: &Pubkey,
        relayer_key: &Pubkey,
        recipient_keys: &[&Pubkey],
        proof: MerkleProof,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.context.payer;

        let contributor_key = &reward_share.contributor_key;
        let unit_share = reward_share.unit_share;
        let economic_burn_rate = reward_share.economic_burn_rate();

        let distribute_rewards_ix = try_build_instruction(
            &ID,
            DistributeRewardsAccounts::new(
                dz_epoch,
                contributor_key,
                dz_mint_key,
                relayer_key,
                recipient_keys,
            ),
            &RevenueDistributionInstructionData::DistributeRewards {
                unit_share,
                economic_burn_rate,
                proof,
            },
        )
        .unwrap();

        self.context.last_blockhash = process_instructions_for_test(
            &mut self.context.banks_client,
            &self.context.last_blockhash,
            &[distribute_rewards_ix],
            &[payer_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn initialize_contributor_rewards(
        &mut self,
        service_key: &Pubkey,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.context.payer;

        let initialize_contributor_rewards_ix = try_build_instruction(
            &ID,
            InitializeContributorRewardsAccounts::new(&payer_signer.pubkey(), service_key),
            &RevenueDistributionInstructionData::InitializeContributorRewards(*service_key),
        )
        .unwrap();

        self.context.last_blockhash = process_instructions_for_test(
            &mut self.context.banks_client,
            &self.context.last_blockhash,
            &[initialize_contributor_rewards_ix],
            &[payer_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn set_rewards_manager(
        &mut self,
        service_key: &Pubkey,
        contributor_manager_signer: &Keypair,
        rewards_manager_key: &Pubkey,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.context.payer;

        let set_rewards_manager_ix = try_build_instruction(
            &ID,
            SetRewardsManagerAccounts::new(&contributor_manager_signer.pubkey(), service_key),
            &RevenueDistributionInstructionData::SetRewardsManager(*rewards_manager_key),
        )
        .unwrap();

        self.context.last_blockhash = process_instructions_for_test(
            &mut self.context.banks_client,
            &self.context.last_blockhash,
            &[set_rewards_manager_ix],
            &[payer_signer, contributor_manager_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn configure_contributor_rewards<const N: usize>(
        &mut self,
        service_key: &Pubkey,
        rewards_manager_signer: &Keypair,
        setting: [ContributorRewardsConfiguration; N],
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.context.payer;

        let configure_contributor_rewards_ixs = setting
            .into_iter()
            .map(|setting| {
                try_build_instruction(
                    &ID,
                    ConfigureContributorRewardsAccounts::new(
                        &rewards_manager_signer.pubkey(),
                        service_key,
                    ),
                    &RevenueDistributionInstructionData::ConfigureContributorRewards(setting),
                )
                .unwrap()
            })
            .collect::<Vec<_>>();

        process_instructions_for_test(
            &mut self.context.banks_client,
            &self.context.last_blockhash,
            &configure_contributor_rewards_ixs,
            &[payer_signer, rewards_manager_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn verify_distribution_merkle_root(
        &mut self,
        dz_epoch: DoubleZeroEpoch,
        distribution_merkle_root_kinds_and_proofs: Vec<(DistributionMerkleRootKind, MerkleProof)>,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.context.payer;

        let verify_distribution_merkle_root_ixs = distribution_merkle_root_kinds_and_proofs
            .into_iter()
            .map(|(kind, proof)| {
                try_build_instruction(
                    &ID,
                    VerifyDistributionMerkleRootAccounts::new(dz_epoch),
                    &RevenueDistributionInstructionData::VerifyDistributionMerkleRoot {
                        kind,
                        proof,
                    },
                )
                .unwrap()
            })
            .collect::<Vec<_>>();

        self.context.last_blockhash = process_instructions_for_test(
            &mut self.context.banks_client,
            &self.context.last_blockhash,
            &verify_distribution_merkle_root_ixs,
            &[payer_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn initialize_solana_validator_deposit(
        &mut self,
        node_id: &Pubkey,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.context.payer;

        let initialize_solana_validator_deposit_ix = try_build_instruction(
            &ID,
            InitializeSolanaValidatorDepositAccounts::new(&payer_signer.pubkey(), node_id),
            &RevenueDistributionInstructionData::InitializeSolanaValidatorDeposit(*node_id),
        )
        .unwrap();

        self.context.last_blockhash = process_instructions_for_test(
            &mut self.context.banks_client,
            &self.context.last_blockhash,
            &[initialize_solana_validator_deposit_ix],
            &[payer_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn pay_solana_validator_debt(
        &mut self,
        dz_epoch: DoubleZeroEpoch,
        node_id: &Pubkey,
        amount: u64,
        proof: MerkleProof,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.context.payer;

        let pay_solana_validator_debt_ix = try_build_instruction(
            &ID,
            PaySolanaValidatorDebtAccounts::new(dz_epoch, node_id),
            &RevenueDistributionInstructionData::PaySolanaValidatorDebt { amount, proof },
        )
        .unwrap();

        self.context.last_blockhash = process_instructions_for_test(
            &mut self.context.banks_client,
            &self.context.last_blockhash,
            &[pay_solana_validator_debt_ix],
            &[payer_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn forgive_solana_validator_debt(
        &mut self,
        dz_epoch: DoubleZeroEpoch,
        next_dz_epoch: DoubleZeroEpoch,
        debt_accountant_signer: &Keypair,
        debt: &SolanaValidatorDebt,
        proof: MerkleProof,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.context.payer;

        let forgive_solana_validator_debt_ix = try_build_instruction(
            &ID,
            ForgiveSolanaValidatorDebtAccounts::new(
                &debt_accountant_signer.pubkey(),
                dz_epoch,
                next_dz_epoch,
            ),
            &RevenueDistributionInstructionData::ForgiveSolanaValidatorDebt { debt: *debt, proof },
        )
        .unwrap();

        self.context.last_blockhash = process_instructions_for_test(
            &mut self.context.banks_client,
            &self.context.last_blockhash,
            &[forgive_solana_validator_debt_ix],
            &[payer_signer, debt_accountant_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn initialize_swap_destination(
        &mut self,
        mint_key: &Pubkey,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.context.payer;

        let initialize_swap_destination_ix = try_build_instruction(
            &ID,
            InitializeSwapDestinationAccounts::new(&payer_signer.pubkey(), mint_key),
            &RevenueDistributionInstructionData::InitializeSwapDestination,
        )
        .unwrap();

        self.context.last_blockhash = process_instructions_for_test(
            &mut self.context.banks_client,
            &self.context.last_blockhash,
            &[initialize_swap_destination_ix],
            &[payer_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn sweep_distribution_tokens(
        &mut self,
        dz_epoch: DoubleZeroEpoch,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.context.payer;
        let sol_2z_swap_fills_registry_key = self.sol_2z_swap_fills_registry_key;

        let sweep_distribution_tokens_ix = try_build_instruction(
            &ID,
            SweepDistributionTokensAccounts::new(
                dz_epoch,
                &mock_swap_sol_2z::ID,
                &sol_2z_swap_fills_registry_key,
            ),
            &RevenueDistributionInstructionData::SweepDistributionTokens,
        )
        .unwrap();

        self.context.last_blockhash = process_instructions_for_test(
            &mut self.context.banks_client,
            &self.context.last_blockhash,
            &[sweep_distribution_tokens_ix],
            &[payer_signer],
        )
        .await?;

        Ok(self)
    }

    //
    // Mock Swap SOL/2Z integration.
    //

    pub async fn mock_buy_sol(
        &mut self,
        source_2z_token_account_key: &Pubkey,
        transfer_authority_signer: &Keypair,
        sol_destination_key: &Pubkey,
        amount_2z_in: u64,
        amount_sol_out: u64,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.context.payer;
        let fills_tracker_key = self.sol_2z_swap_fills_registry_key;

        let buy_sol_ix = mock_swap_sol_2z::instruction::buy_sol(
            &fills_tracker_key,
            source_2z_token_account_key,
            &transfer_authority_signer.pubkey(),
            sol_destination_key,
            amount_2z_in,
            amount_sol_out,
        );

        self.context.last_blockhash = process_instructions_for_test(
            &mut self.context.banks_client,
            &self.context.last_blockhash,
            &[buy_sol_ix],
            &[payer_signer, transfer_authority_signer],
        )
        .await?;

        Ok(self)
    }

    //
    // Account fetchers.
    //

    pub async fn fetch_token_account(
        &self,
        token_account_key: &Pubkey,
    ) -> Result<TokenAccount, BanksClientError> {
        let token_account_data = self
            .context
            .banks_client
            .get_account(*token_account_key)
            .await?
            .unwrap_or_default()
            .data;

        TokenAccount::unpack(&token_account_data)
            .map_err(|_| BanksClientError::ClientError("not SPL token account"))
    }

    pub async fn fetch_program_config(&self) -> (Pubkey, ProgramConfig, TokenAccount) {
        let program_config_key = ProgramConfig::find_address().0;

        let program_config_account_data = self
            .context
            .banks_client
            .get_account(program_config_key)
            .await
            .unwrap()
            .unwrap()
            .data;

        let token_pda_key = state::find_2z_token_pda_address(&program_config_key).0;
        let reserve_2z_data = self
            .context
            .banks_client
            .get_account(token_pda_key)
            .await
            .unwrap()
            .unwrap()
            .data;

        let token_pda = TokenAccount::unpack(&reserve_2z_data).unwrap();

        (
            program_config_key,
            *checked_from_bytes_with_discriminator(&program_config_account_data)
                .unwrap()
                .0,
            token_pda,
        )
    }

    pub async fn fetch_journal(&self) -> (Pubkey, Journal, TokenAccount) {
        let journal_key = Journal::find_address().0;

        let program_config_account_data = self
            .context
            .banks_client
            .get_account(journal_key)
            .await
            .unwrap()
            .unwrap()
            .data;

        let (journal, _) =
            checked_from_bytes_with_discriminator(&program_config_account_data).unwrap();

        let token_pda_key = state::find_2z_token_pda_address(&journal_key).0;
        let journal_2z_token_pda_data = self
            .context
            .banks_client
            .get_account(token_pda_key)
            .await
            .unwrap()
            .unwrap()
            .data;

        let token_pda = TokenAccount::unpack(&journal_2z_token_pda_data).unwrap();

        (journal_key, *journal, token_pda)
    }

    pub async fn fetch_distribution(
        &self,
        dz_epoch: DoubleZeroEpoch,
    ) -> (Pubkey, Distribution, Vec<u8>, u64, TokenAccount) {
        let distribution_key = Distribution::find_address(dz_epoch).0;

        let distribution_account_info = self
            .context
            .banks_client
            .get_account(distribution_key)
            .await
            .unwrap()
            .unwrap();

        let (distribution, distribution_remaining_data) =
            checked_from_bytes_with_discriminator(&distribution_account_info.data).unwrap();

        let token_pda_key = state::find_2z_token_pda_address(&distribution_key).0;
        let distribution_2z_token_pda_data = self
            .context
            .banks_client
            .get_account(token_pda_key)
            .await
            .unwrap()
            .unwrap()
            .data;

        let token_pda = TokenAccount::unpack(&distribution_2z_token_pda_data).unwrap();

        (
            distribution_key,
            *distribution,
            distribution_remaining_data.to_vec(),
            distribution_account_info.lamports,
            token_pda,
        )
    }

    pub async fn fetch_contributor_rewards(
        &self,
        service_key: &Pubkey,
    ) -> (Pubkey, ContributorRewards) {
        let contributor_rewards_key = ContributorRewards::find_address(service_key).0;

        let contributor_rewards_account_data = self
            .context
            .banks_client
            .get_account(contributor_rewards_key)
            .await
            .unwrap()
            .unwrap()
            .data;

        let contributor_rewards =
            *checked_from_bytes_with_discriminator(&contributor_rewards_account_data)
                .unwrap()
                .0;

        (contributor_rewards_key, contributor_rewards)
    }

    pub async fn fetch_solana_validator_deposit(
        &self,
        node_id: &Pubkey,
    ) -> (Pubkey, SolanaValidatorDeposit) {
        let solana_validator_deposit_key = SolanaValidatorDeposit::find_address(node_id).0;

        let solana_validator_deposit_account_data = self
            .context
            .banks_client
            .get_account(solana_validator_deposit_key)
            .await
            .unwrap()
            .unwrap()
            .data;

        (
            solana_validator_deposit_key,
            *checked_from_bytes_with_discriminator(&solana_validator_deposit_account_data)
                .unwrap()
                .0,
        )
    }
}

pub async fn process_instructions_for_test(
    banks_client: &mut BanksClient,
    cached_blockhash: &Hash,
    instructions: &[Instruction],
    signers: &[&Keypair],
) -> Result<Hash, BanksClientError> {
    let recent_blockhash = banks_client
        .get_new_latest_blockhash(cached_blockhash)
        .await
        .map_err(|_| BanksClientError::ClientError("failed to get new blockhash"))?;

    let transaction = new_transaction(instructions, signers, recent_blockhash);

    banks_client.process_transaction(transaction).await?;

    Ok(recent_blockhash)
}

fn new_transaction(
    instructions: &[Instruction],
    signers: &[&Keypair],
    recent_blockhash: Hash,
) -> VersionedTransaction {
    let message =
        Message::try_compile(&signers[0].pubkey(), instructions, &[], recent_blockhash).unwrap();

    VersionedTransaction::try_new(VersionedMessage::V0(message), signers).unwrap()
}
