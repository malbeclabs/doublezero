use base64::{prelude::BASE64_STANDARD, Engine};
use doublezero_program_tools::zero_copy::checked_from_bytes_with_discriminator;
use doublezero_revenue_distribution::{
    state::Distribution,
    types::DoubleZeroEpoch,
    {
        instruction::{
            AdminKey, ConfigureDistributionData, ConfigureProgramSetting,
            RevenueDistributionInstructionData,
        },
        state::{self, Journal, ProgramConfig},
        DOUBLEZERO_MINT, ID,
    },
};
use solana_loader_v3_interface::{get_program_data_address, state::UpgradeableLoaderState};
use solana_program_pack::Pack;
use solana_program_test::{BanksClient, BanksClientError, ProgramTest};
use solana_pubkey::Pubkey;
use solana_sdk::{
    account::Account,
    hash::Hash,
    instruction::{AccountMeta, Instruction},
    message::{v0::Message, VersionedMessage},
    signature::{Keypair, Signer},
    transaction::VersionedTransaction,
};
use spl_token::state::Account as TokenAccount;

pub struct ProgramTestWithOwner {
    pub banks_client: BanksClient,
    pub payer_signer: Keypair,
    pub recent_blockhash: Hash,
    pub owner_signer: Keypair,
}

pub async fn start_test_with_accounts<const N: usize>(
    accounts: [(Pubkey, Account); N],
) -> ProgramTestWithOwner {
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

    // Add the 2Z Mint.
    let mint_acct = Account {
        lamports: 69,
        owner: spl_token::ID,
        data: BASE64_STANDARD.decode("AAAAAE1jnR8S73ewuG1cltefhmHehgZSBXMl+4ukrwX7lnXwAADBb/KGIwAGAQAAAABNY50fEu93sLhtXJbXn4Zh3oYGUgVzJfuLpK8F+5Z18A==").unwrap(),
        ..Default::default()
    };
    program_test.add_account(DOUBLEZERO_MINT, mint_acct);

    for (key, account) in accounts.into_iter() {
        program_test.add_account(key, account);
    }

    let (banks_client, payer_signer, recent_blockhash) = program_test.start().await;

    ProgramTestWithOwner {
        banks_client,
        payer_signer,
        recent_blockhash,
        owner_signer,
    }
}

pub async fn start_test() -> ProgramTestWithOwner {
    start_test_with_accounts([]).await
}

pub fn program_data_key() -> Pubkey {
    get_program_data_address(&ID)
}

impl ProgramTestWithOwner {
    pub async fn initialize_program(&mut self) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.payer_signer;
        let program_config_key = ProgramConfig::find_address().0;

        let initialize_program_ix = Instruction {
            program_id: ID,
            accounts: vec![
                AccountMeta::new(payer_signer.pubkey(), true),
                AccountMeta::new(program_config_key, false),
                AccountMeta::new_readonly(solana_system_interface::program::ID, false),
            ],
            data: borsh::to_vec(&RevenueDistributionInstructionData::InitializeProgram).unwrap(),
        };

        // TODO: Remove from here and use this for happy path testing.
        let remove_me_ix = solana_system_interface::instruction::transfer(
            &payer_signer.pubkey(),
            &program_config_key,
            1,
        );

        let new_blockhash = process_instructions_for_test(
            &self.banks_client,
            self.recent_blockhash,
            &[remove_me_ix, initialize_program_ix],
            &[payer_signer],
        )
        .await?;

        self.recent_blockhash = new_blockhash;

        Ok(self)
    }

    pub async fn set_admin(&mut self, admin_key: AdminKey) -> Result<&mut Self, BanksClientError> {
        let owner_signer = &self.owner_signer;
        let payer_signer = &self.payer_signer;

        let set_admin_ix = Instruction {
            program_id: ID,
            accounts: vec![
                AccountMeta::new_readonly(program_data_key(), false),
                AccountMeta::new_readonly(owner_signer.pubkey(), true),
                AccountMeta::new(ProgramConfig::find_address().0, false),
            ],
            data: admin_key.try_into().unwrap(),
        };

        let new_blockhash = process_instructions_for_test(
            &self.banks_client,
            self.recent_blockhash,
            &[set_admin_ix],
            &[payer_signer, owner_signer],
        )
        .await?;

        self.recent_blockhash = new_blockhash;

        Ok(self)
    }

    pub async fn configure_program<const N: usize>(
        &mut self,
        settings: [ConfigureProgramSetting; N],
        admin_signer: &Keypair,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.payer_signer;

        let configure_program_ixs = settings
            .into_iter()
            .map(|setting| Instruction {
                program_id: ID,
                accounts: vec![
                    AccountMeta::new(ProgramConfig::find_address().0, false),
                    AccountMeta::new_readonly(admin_signer.pubkey(), true),
                ],
                data: setting.try_into().unwrap(),
            })
            .collect::<Vec<_>>();

        let new_blockhash = process_instructions_for_test(
            &self.banks_client,
            self.recent_blockhash,
            &configure_program_ixs,
            &[payer_signer, admin_signer],
        )
        .await?;

        self.recent_blockhash = new_blockhash;

        Ok(self)
    }

    pub async fn initialize_journal(&mut self) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.payer_signer;
        let journal_key = Journal::find_address().0;

        let initialize_journal_ix = Instruction {
            program_id: ID,
            accounts: vec![
                AccountMeta::new(payer_signer.pubkey(), true),
                AccountMeta::new(journal_key, false),
                AccountMeta::new(state::find_custodied_2z_address(&journal_key).0, false),
                AccountMeta::new_readonly(DOUBLEZERO_MINT, false),
                AccountMeta::new_readonly(spl_token::ID, false),
                AccountMeta::new_readonly(solana_system_interface::program::ID, false),
            ],
            data: borsh::to_vec(&RevenueDistributionInstructionData::InitializeJournal).unwrap(),
        };

        // TODO: Remove from here and use this for happy path testing.
        let remove_me_ix =
            solana_system_interface::instruction::transfer(&payer_signer.pubkey(), &journal_key, 1);

        let new_blockhash = process_instructions_for_test(
            &self.banks_client,
            self.recent_blockhash,
            &[remove_me_ix, initialize_journal_ix],
            &[payer_signer],
        )
        .await?;

        self.recent_blockhash = new_blockhash;

        Ok(self)
    }

    pub async fn initialize_distribution(
        &mut self,
        accountant_signer: &Keypair,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.payer_signer;

        let (program_config_key, program_config) = self.fetch_program_config().await;
        let epoch_distribution_key = Distribution::find_address(program_config.next_dz_epoch).0;

        let initialize_epoch_distribution_ix = Instruction {
            program_id: ID,
            accounts: vec![
                AccountMeta::new(program_config_key, false),
                AccountMeta::new_readonly(accountant_signer.pubkey(), true),
                AccountMeta::new(payer_signer.pubkey(), true),
                AccountMeta::new(epoch_distribution_key, false),
                AccountMeta::new(
                    state::find_custodied_2z_address(&epoch_distribution_key).0,
                    false,
                ),
                AccountMeta::new_readonly(DOUBLEZERO_MINT, false),
                AccountMeta::new_readonly(spl_token::ID, false),
                AccountMeta::new_readonly(solana_system_interface::program::ID, false),
            ],
            data: borsh::to_vec(&RevenueDistributionInstructionData::InitializeDistribution)
                .unwrap(),
        };

        // TODO: Remove from here and use this for happy path testing.
        let remove_me_ix = solana_system_interface::instruction::transfer(
            &payer_signer.pubkey(),
            &epoch_distribution_key,
            1,
        );

        let new_blockhash = process_instructions_for_test(
            &self.banks_client,
            self.recent_blockhash,
            &[remove_me_ix, initialize_epoch_distribution_ix],
            &[payer_signer, accountant_signer],
        )
        .await?;

        self.recent_blockhash = new_blockhash;

        Ok(self)
    }

    pub async fn configure_distribution<const N: usize>(
        &mut self,
        dz_epoch: DoubleZeroEpoch,
        data: [ConfigureDistributionData; N],
        accountant_signer: &Keypair,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.payer_signer;

        let configure_program_ixs = data
            .into_iter()
            .map(|data| Instruction {
                program_id: ID,
                accounts: vec![
                    AccountMeta::new_readonly(ProgramConfig::find_address().0, false),
                    AccountMeta::new_readonly(accountant_signer.pubkey(), true),
                    AccountMeta::new(Distribution::find_address(dz_epoch).0, false),
                ],
                data: data.try_into().unwrap(),
            })
            .collect::<Vec<_>>();

        let new_blockhash = process_instructions_for_test(
            &self.banks_client,
            self.recent_blockhash,
            &configure_program_ixs,
            &[payer_signer, accountant_signer],
        )
        .await?;

        self.recent_blockhash = new_blockhash;

        Ok(self)
    }

    //
    // Account fetchers.
    //

    pub async fn fetch_program_config(&self) -> (Pubkey, ProgramConfig) {
        let program_config_key = ProgramConfig::find_address().0;

        let program_config_account_data = self
            .banks_client
            .get_account(program_config_key)
            .await
            .unwrap()
            .unwrap()
            .data;

        (
            program_config_key,
            *checked_from_bytes_with_discriminator(&program_config_account_data)
                .unwrap()
                .0,
        )
    }

    pub async fn fetch_distribution(
        &self,
        dz_epoch: DoubleZeroEpoch,
    ) -> (Pubkey, Distribution, TokenAccount) {
        let distribution_key = Distribution::find_address(dz_epoch).0;

        let distribution_account_data = self
            .banks_client
            .get_account(distribution_key)
            .await
            .unwrap()
            .unwrap()
            .data;

        let distribution = *checked_from_bytes_with_discriminator(&distribution_account_data)
            .unwrap()
            .0;

        let custodied_2z_key = state::find_custodied_2z_address(&distribution_key).0;
        let distribution_custody_account_data = self
            .banks_client
            .get_account(custodied_2z_key)
            .await
            .unwrap()
            .unwrap()
            .data;

        let custodied_2z_token_account =
            TokenAccount::unpack(&distribution_custody_account_data).unwrap();

        (distribution_key, distribution, custodied_2z_token_account)
    }
}

pub async fn process_instructions_for_test(
    banks_client: &BanksClient,
    recent_blockhash: Hash,
    instructions: &[Instruction],
    signers: &[&Keypair],
) -> Result<Hash, BanksClientError> {
    let message =
        Message::try_compile(&signers[0].pubkey(), instructions, &[], recent_blockhash).unwrap();

    let transaction =
        VersionedTransaction::try_new(VersionedMessage::V0(message), signers).unwrap();

    banks_client.process_transaction(transaction).await?;

    banks_client.get_latest_blockhash().await
}
