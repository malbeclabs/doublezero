#![allow(dead_code)]

use doublezero_program_tools::{
    instruction::try_build_instruction, zero_copy::checked_from_bytes_with_discriminator,
};
use doublezero_revenue_distribution::{
    instruction::{
        account::{
            ConfigureContributorRewardsAccounts, ConfigureDistributionPaymentsAccounts,
            ConfigureDistributionRewardsAccounts, ConfigureJournalAccounts,
            ConfigureProgramAccounts, DenyPrepaidConnectionAccessAccounts,
            FinalizeDistributionRewardsAccounts, GrantPrepaidConnectionAccessAccounts,
            InitializeContributorRewardsAccounts, InitializeDistributionAccounts,
            InitializeJournalAccounts, InitializePrepaidConnectionAccounts,
            InitializeProgramAccounts, LoadPrepaidConnectionAccounts, SetAdminAccounts,
            SetRewardsManagerAccounts, TerminatePrepaidConnectionAccounts,
            VerifyDistributionMerkleRootAccounts,
        },
        ContributorRewardsConfiguration, DistributionMerkleRootKind,
        DistributionPaymentsConfiguration, JournalConfiguration, ProgramConfiguration,
        RevenueDistributionInstructionData,
    },
    state::{
        self, ContributorRewards, Distribution, Journal, JournalEntries, PrepaidConnection,
        ProgramConfig,
    },
    types::DoubleZeroEpoch,
    DOUBLEZERO_MINT_KEY, ID,
};
use solana_loader_v3_interface::{get_program_data_address, state::UpgradeableLoaderState};
use solana_program_pack::Pack;
use solana_program_test::{BanksClient, BanksClientError, ProgramTest, ProgramTestBanksClientExt};
use solana_pubkey::Pubkey;
use solana_sdk::{
    account::Account,
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
    pub banks_client: BanksClient,
    pub payer_signer: Keypair,
    pub cached_blockhash: Hash,
    pub owner_signer: Keypair,
    pub treasury_2z_key: Pubkey,
}

pub async fn start_test_with_accounts(accounts: Vec<TestAccount>) -> ProgramTestWithOwner {
    let mut program_test = ProgramTest::new("doublezero_revenue_distribution", ID, None);
    program_test.prefer_bpf(true);

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
    program_test.add_account(program_data_key(), program_data_acct);

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

    let (banks_client, payer_signer, cached_blockhash) = program_test.start().await;

    ProgramTestWithOwner {
        banks_client,
        payer_signer,
        cached_blockhash,
        owner_signer,
        treasury_2z_key,
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

pub fn program_data_key() -> Pubkey {
    get_program_data_address(&ID)
}

pub struct IndexedProgramLog<'a> {
    pub index: usize,
    pub message: &'a str,
}

impl ProgramTestWithOwner {
    pub async fn get_latest_blockhash(&mut self) -> Result<Hash, BanksClientError> {
        self.banks_client
            .get_new_latest_blockhash(&self.cached_blockhash)
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

        let payer_signer = &self.payer_signer;

        let mut tx_signers = vec![payer_signer];
        tx_signers.extend_from_slice(signers);

        let transaction = new_transaction(instructions, &tx_signers, recent_blockhash);

        let simulated_tx = self
            .banks_client
            .simulate_transaction(transaction)
            .await
            .unwrap();

        let tx_err = simulated_tx.result.unwrap().unwrap_err();

        self.cached_blockhash = recent_blockhash;

        (tx_err, simulated_tx.simulation_details.unwrap().logs)
    }

    pub async fn transfer_lamports(
        &mut self,
        dst_key: &Pubkey,
        amount: u64,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.payer_signer;

        let transfer_ix =
            solana_system_interface::instruction::transfer(&payer_signer.pubkey(), dst_key, amount);

        self.cached_blockhash = process_instructions_for_test(
            &mut self.banks_client,
            &self.cached_blockhash,
            &[transfer_ix],
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
        let payer_signer = &self.payer_signer;
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

        self.cached_blockhash = process_instructions_for_test(
            &mut self.banks_client,
            &self.cached_blockhash,
            &[token_transfer_ix],
            &[payer_signer, owner_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn initialize_program(&mut self) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.payer_signer;
        let program_config_key = ProgramConfig::find_address().0;

        let initialize_program_ix = try_build_instruction(
            &ID,
            InitializeProgramAccounts::new(&payer_signer.pubkey()),
            &RevenueDistributionInstructionData::InitializeProgram,
        )
        .unwrap();

        // TODO: Remove from here and use this for happy path testing.
        let remove_me_ix = solana_system_interface::instruction::transfer(
            &payer_signer.pubkey(),
            &program_config_key,
            1,
        );

        self.cached_blockhash = process_instructions_for_test(
            &mut self.banks_client,
            &self.cached_blockhash,
            &[remove_me_ix, initialize_program_ix],
            &[payer_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn set_admin(&mut self, admin_key: &Pubkey) -> Result<&mut Self, BanksClientError> {
        let owner_signer = &self.owner_signer;
        let payer_signer = &self.payer_signer;

        let set_admin_ix = try_build_instruction(
            &ID,
            SetAdminAccounts::new(&program_data_key(), &owner_signer.pubkey()),
            &RevenueDistributionInstructionData::SetAdmin(*admin_key),
        )
        .unwrap();

        self.cached_blockhash = process_instructions_for_test(
            &mut self.banks_client,
            &self.cached_blockhash,
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
        let payer_signer = &self.payer_signer;

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

        self.cached_blockhash = process_instructions_for_test(
            &mut self.banks_client,
            &self.cached_blockhash,
            &configure_program_ixs,
            &[payer_signer, admin_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn initialize_journal(&mut self) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.payer_signer;
        let journal_key = Journal::find_address().0;

        let initialize_journal_ix = try_build_instruction(
            &ID,
            InitializeJournalAccounts::new(&payer_signer.pubkey()),
            &RevenueDistributionInstructionData::InitializeJournal,
        )
        .unwrap();

        // TODO: Remove from here and use this for happy path testing.
        let remove_me_ix =
            solana_system_interface::instruction::transfer(&payer_signer.pubkey(), &journal_key, 1);

        self.cached_blockhash = process_instructions_for_test(
            &mut self.banks_client,
            &self.cached_blockhash,
            &[remove_me_ix, initialize_journal_ix],
            &[payer_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn configure_journal<const N: usize>(
        &mut self,
        admin_signer: &Keypair,
        settings: [JournalConfiguration; N],
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.payer_signer;

        let configure_program_ixs = settings
            .into_iter()
            .map(|setting| {
                try_build_instruction(
                    &ID,
                    ConfigureJournalAccounts::new(&admin_signer.pubkey()),
                    &RevenueDistributionInstructionData::ConfigureJournal(setting),
                )
                .unwrap()
            })
            .collect::<Vec<_>>();

        process_instructions_for_test(
            &mut self.banks_client,
            &self.cached_blockhash,
            &configure_program_ixs,
            &[payer_signer, admin_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn initialize_distribution(
        &mut self,
        accountant_signer: &Keypair,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.payer_signer;

        let (_, program_config, _) = self.fetch_program_config().await;

        let initialize_distribution_ix = try_build_instruction(
            &ID,
            InitializeDistributionAccounts::new(
                &accountant_signer.pubkey(),
                &payer_signer.pubkey(),
                program_config.next_dz_epoch,
            ),
            &RevenueDistributionInstructionData::InitializeDistribution,
        )
        .unwrap();

        self.cached_blockhash = process_instructions_for_test(
            &mut self.banks_client,
            &self.cached_blockhash,
            &[initialize_distribution_ix],
            &[payer_signer, accountant_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn configure_distribution_payments<const N: usize>(
        &mut self,
        dz_epoch: DoubleZeroEpoch,
        payments_accountant_signer: &Keypair,
        setting: [DistributionPaymentsConfiguration; N],
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.payer_signer;

        let configure_distribution_payments_ixs = setting
            .into_iter()
            .map(|setting| {
                try_build_instruction(
                    &ID,
                    ConfigureDistributionPaymentsAccounts::new(
                        &payments_accountant_signer.pubkey(),
                        dz_epoch,
                    ),
                    &RevenueDistributionInstructionData::ConfigureDistributionPayments(setting),
                )
                .unwrap()
            })
            .collect::<Vec<_>>();

        self.cached_blockhash = process_instructions_for_test(
            &mut self.banks_client,
            &self.cached_blockhash,
            &configure_distribution_payments_ixs,
            &[payer_signer, payments_accountant_signer],
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
        let payer_signer = &self.payer_signer;

        let configure_distribution_rewards_ix = try_build_instruction(
            &ID,
            ConfigureDistributionRewardsAccounts::new(&accountant_signer.pubkey(), dz_epoch),
            &RevenueDistributionInstructionData::ConfigureDistributionRewards {
                total_contributors,
                merkle_root,
            },
        )
        .unwrap();

        self.cached_blockhash = process_instructions_for_test(
            &mut self.banks_client,
            &self.cached_blockhash,
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
        let payer_signer = &self.payer_signer;

        let finalize_distribution_rewards_ix = try_build_instruction(
            &ID,
            FinalizeDistributionRewardsAccounts::new(&payer_signer.pubkey(), dz_epoch),
            &RevenueDistributionInstructionData::FinalizeDistributionRewards,
        )
        .unwrap();

        self.cached_blockhash = process_instructions_for_test(
            &mut self.banks_client,
            &self.cached_blockhash,
            &[finalize_distribution_rewards_ix],
            &[payer_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn initialize_prepaid_connection(
        &mut self,
        user_key: &Pubkey,
        token_transfer_authority_signer: &Keypair,
        source_2z_token_account_key: &Pubkey,
        decimals: u8,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.payer_signer;

        let initialize_prepaid_connection_ix = try_build_instruction(
            &ID,
            InitializePrepaidConnectionAccounts::new(
                source_2z_token_account_key,
                &token_transfer_authority_signer.pubkey(),
                &payer_signer.pubkey(),
                user_key,
            ),
            &RevenueDistributionInstructionData::InitializePrepaidConnection {
                user_key: *user_key,
                decimals,
            },
        )
        .unwrap();

        self.cached_blockhash = process_instructions_for_test(
            &mut self.banks_client,
            &self.cached_blockhash,
            &[initialize_prepaid_connection_ix],
            &[payer_signer, token_transfer_authority_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn grant_prepaid_connection_access(
        &mut self,
        dz_ledger_sentinel_signer: &Keypair,
        user_key: &Pubkey,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.payer_signer;

        let grant_prepaid_connection_access_ix = try_build_instruction(
            &ID,
            GrantPrepaidConnectionAccessAccounts::new(
                &dz_ledger_sentinel_signer.pubkey(),
                user_key,
            ),
            &RevenueDistributionInstructionData::GrantPrepaidConnectionAccess,
        )
        .unwrap();

        self.cached_blockhash = process_instructions_for_test(
            &mut self.banks_client,
            &self.cached_blockhash,
            &[grant_prepaid_connection_access_ix],
            &[payer_signer, dz_ledger_sentinel_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn deny_prepaid_connection_access(
        &mut self,
        dz_ledger_sentinel_signer: &Keypair,
        activation_funder_key: &Pubkey,
        user_key: &Pubkey,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.payer_signer;

        let deny_prepaid_connection_access_ix = try_build_instruction(
            &ID,
            DenyPrepaidConnectionAccessAccounts::new(
                &dz_ledger_sentinel_signer.pubkey(),
                activation_funder_key,
                &payer_signer.pubkey(),
                user_key,
            ),
            &RevenueDistributionInstructionData::DenyPrepaidConnectionAccess,
        )
        .unwrap();

        self.cached_blockhash = process_instructions_for_test(
            &mut self.banks_client,
            &self.cached_blockhash,
            &[deny_prepaid_connection_access_ix],
            &[payer_signer, dz_ledger_sentinel_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn load_prepaid_connection(
        &mut self,
        user_key: &Pubkey,
        token_transfer_authority_signer: &Keypair,
        source_2z_token_account_key: &Pubkey,
        valid_through_dz_epoch: DoubleZeroEpoch,
        decimals: u8,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.payer_signer;

        let initialize_prepaid_connection_ix = try_build_instruction(
            &ID,
            LoadPrepaidConnectionAccounts::new(
                source_2z_token_account_key,
                &token_transfer_authority_signer.pubkey(),
                user_key,
            ),
            &RevenueDistributionInstructionData::LoadPrepaidConnection {
                valid_through_dz_epoch,
                decimals,
            },
        )
        .unwrap();

        self.cached_blockhash = process_instructions_for_test(
            &mut self.banks_client,
            &self.cached_blockhash,
            &[initialize_prepaid_connection_ix],
            &[payer_signer, token_transfer_authority_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn terminate_prepaid_connection(
        &mut self,
        user_key: &Pubkey,
        termination_beneficiary: &Pubkey,
        termination_relayer: Option<&Pubkey>,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.payer_signer;

        let terminate_prepaid_connection_ix = try_build_instruction(
            &ID,
            TerminatePrepaidConnectionAccounts::new(
                user_key,
                termination_beneficiary,
                termination_relayer,
            ),
            &RevenueDistributionInstructionData::TerminatePrepaidConnection,
        )
        .unwrap();

        self.cached_blockhash = process_instructions_for_test(
            &mut self.banks_client,
            &self.cached_blockhash,
            &[terminate_prepaid_connection_ix],
            &[payer_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn initialize_contributor_rewards(
        &mut self,
        service_key: &Pubkey,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.payer_signer;

        let initialize_contributor_rewards_ix = try_build_instruction(
            &ID,
            InitializeContributorRewardsAccounts::new(&payer_signer.pubkey(), service_key),
            &RevenueDistributionInstructionData::InitializeContributorRewards(*service_key),
        )
        .unwrap();

        self.cached_blockhash = process_instructions_for_test(
            &mut self.banks_client,
            &self.cached_blockhash,
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
        let payer_signer = &self.payer_signer;

        let set_rewards_manager_ix = try_build_instruction(
            &ID,
            SetRewardsManagerAccounts::new(&contributor_manager_signer.pubkey(), service_key),
            &RevenueDistributionInstructionData::SetRewardsManager(*rewards_manager_key),
        )
        .unwrap();

        self.cached_blockhash = process_instructions_for_test(
            &mut self.banks_client,
            &self.cached_blockhash,
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
        let payer_signer = &self.payer_signer;

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
            &mut self.banks_client,
            &self.cached_blockhash,
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
        let payer_signer = &self.payer_signer;

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

        self.cached_blockhash = process_instructions_for_test(
            &mut self.banks_client,
            &self.cached_blockhash,
            &verify_distribution_merkle_root_ixs,
            &[payer_signer],
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
            .banks_client
            .get_account(program_config_key)
            .await
            .unwrap()
            .unwrap()
            .data;

        let token_pda_key = state::find_2z_token_pda_address(&program_config_key).0;
        let reserve_2z_data = self
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

    pub async fn fetch_journal(&self) -> (Pubkey, Journal, JournalEntries, TokenAccount) {
        let journal_key = Journal::find_address().0;

        let program_config_account_data = self
            .banks_client
            .get_account(journal_key)
            .await
            .unwrap()
            .unwrap()
            .data;

        let (journal, remaining_data) =
            checked_from_bytes_with_discriminator(&program_config_account_data).unwrap();

        let journal_entries = Journal::checked_journal_entries(remaining_data).unwrap();

        let token_pda_key = state::find_2z_token_pda_address(&journal_key).0;
        let journal_2z_token_pda_data = self
            .banks_client
            .get_account(token_pda_key)
            .await
            .unwrap()
            .unwrap()
            .data;

        let token_pda = TokenAccount::unpack(&journal_2z_token_pda_data).unwrap();

        (journal_key, *journal, journal_entries, token_pda)
    }

    pub async fn fetch_distribution(
        &self,
        dz_epoch: DoubleZeroEpoch,
    ) -> (Pubkey, Distribution, u64, TokenAccount) {
        let distribution_key = Distribution::find_address(dz_epoch).0;

        let distribution_account_info = self
            .banks_client
            .get_account(distribution_key)
            .await
            .unwrap()
            .unwrap();

        let distribution = *checked_from_bytes_with_discriminator(&distribution_account_info.data)
            .unwrap()
            .0;

        let token_pda_key = state::find_2z_token_pda_address(&distribution_key).0;
        let distribution_2z_token_pda_data = self
            .banks_client
            .get_account(token_pda_key)
            .await
            .unwrap()
            .unwrap()
            .data;

        let token_pda = TokenAccount::unpack(&distribution_2z_token_pda_data).unwrap();

        (
            distribution_key,
            distribution,
            distribution_account_info.lamports,
            token_pda,
        )
    }

    pub async fn fetch_prepaid_connection(&self, user_key: &Pubkey) -> (Pubkey, PrepaidConnection) {
        let prepaid_connection_key = PrepaidConnection::find_address(user_key).0;

        let prepaid_connection_account_data = self
            .banks_client
            .get_account(prepaid_connection_key)
            .await
            .unwrap()
            .unwrap()
            .data;

        (
            prepaid_connection_key,
            *checked_from_bytes_with_discriminator(&prepaid_connection_account_data)
                .unwrap()
                .0,
        )
    }

    pub async fn fetch_contributor_rewards(
        &self,
        service_key: &Pubkey,
    ) -> (Pubkey, ContributorRewards) {
        let contributor_rewards_key = ContributorRewards::find_address(service_key).0;

        let contributor_rewards_account_data = self
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
