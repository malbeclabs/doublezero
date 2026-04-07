#![allow(dead_code)]

#[ctor::ctor]
fn init_logger() {
    static ONCE: std::sync::Once = std::sync::Once::new();
    ONCE.call_once(|| {
        let mut builder = env_logger::builder();

        // If DEBUG is set, show the Solana program logs.
        if std::env::var_os("DEBUG").is_some() {
            builder.filter_level(log::LevelFilter::Error);
            builder.filter(
                Some("solana_runtime::message_processor::stable_log"),
                log::LevelFilter::Debug,
            );
        }

        let _ = builder.try_init();
    });
}

use doublezero_passport::{
    instruction::{
        account::{
            ConfigureProgramAccounts, DenyAccessAccounts, GrantAccessAccounts,
            InitializeProgramAccounts, RequestAccessAccounts, SetAdminAccounts,
        },
        AccessMode, PassportInstructionData, ProgramConfiguration, ProgramFlagConfiguration,
    },
    state::{AccessRequest, ProgramConfig},
    ID,
};
use doublezero_program_tools::{
    instruction::try_build_instruction, zero_copy::checked_from_bytes_with_discriminator,
};
use solana_loader_v3_interface::{get_program_data_address, state::UpgradeableLoaderState};
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

pub struct TestAccount {
    pub key: Pubkey,
    pub info: Account,
}

pub struct ProgramTestWithOwner {
    pub banks_client: BanksClient,
    pub payer_signer: Keypair,
    pub cached_blockhash: Hash,
    pub owner_signer: Keypair,
}

pub struct ConfiguredProgramState {
    pub admin_signer: Keypair,
    pub sentinel_signer: Keypair,
}

pub async fn start_test_with_accounts(accounts: Vec<TestAccount>) -> ProgramTestWithOwner {
    let mut program_test = ProgramTest::new("doublezero_passport", ID, None);
    program_test.prefer_bpf(true);

    let owner_signer = Keypair::new();

    // Fake the BPF Upgradeable Program's program data account for the Passport
    // Program.
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

    for TestAccount { key, info } in accounts.into_iter() {
        program_test.add_account(key, info);
    }

    let (banks_client, payer_signer, cached_blockhash) = program_test.start().await;

    ProgramTestWithOwner {
        banks_client,
        payer_signer,
        cached_blockhash,
        owner_signer,
    }
}

pub async fn start_test() -> ProgramTestWithOwner {
    start_test_with_accounts(Default::default()).await
}

impl ProgramTestWithOwner {
    pub async fn get_latest_blockhash(&mut self) -> Result<Hash, BanksClientError> {
        self.banks_client
            .get_new_latest_blockhash(&self.cached_blockhash)
            .await
            .map_err(Into::into)
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

    pub async fn unwrap_simulation_error(
        &mut self,
        instructions: &[Instruction],
        signers: &[&Keypair],
    ) -> Result<(TransactionError, Vec<String>), BanksClientError> {
        let recent_blockhash = self.get_latest_blockhash().await?;
        let payer_signer = &self.payer_signer;

        let mut tx_signers = vec![payer_signer];
        tx_signers.extend_from_slice(signers);

        let transaction = new_transaction(instructions, &tx_signers, recent_blockhash);

        let simulated_tx = self.banks_client.simulate_transaction(transaction).await?;

        let tx_err = simulated_tx
            .result
            .ok_or(BanksClientError::ClientError(
                "simulation returned no result",
            ))?
            .unwrap_err();

        self.cached_blockhash = recent_blockhash;

        Ok((tx_err, simulated_tx.simulation_details.unwrap().logs))
    }

    pub async fn setup_configured_program(
        &mut self,
    ) -> Result<ConfiguredProgramState, BanksClientError> {
        let admin_signer = Keypair::new();
        let sentinel_signer = Keypair::new();

        self.transfer_lamports(&sentinel_signer.pubkey(), 128 * 6_960)
            .await?
            .initialize_program()
            .await?
            .set_admin(&admin_signer.pubkey())
            .await?
            .configure_program(
                [
                    ProgramConfiguration::Flag(ProgramFlagConfiguration::IsPaused(false)),
                    ProgramConfiguration::DoubleZeroLedgerSentinel(sentinel_signer.pubkey()),
                    ProgramConfiguration::AccessRequestDeposit {
                        request_deposit_lamports: 10_000_000,
                        request_fee_lamports: 10_000,
                    },
                    ProgramConfiguration::SolanaValidatorBackupIdsLimit(2),
                ],
                &admin_signer,
            )
            .await?;

        Ok(ConfiguredProgramState {
            admin_signer,
            sentinel_signer,
        })
    }

    pub async fn initialize_program(&mut self) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.payer_signer;
        let program_config_key = ProgramConfig::find_address().0;

        let initialize_program_ix = try_build_instruction(
            &ID,
            InitializeProgramAccounts::new(&payer_signer.pubkey()),
            &PassportInstructionData::InitializeProgram,
        )
        .unwrap();

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
            SetAdminAccounts::new(&ID, &owner_signer.pubkey()),
            &PassportInstructionData::SetAdmin(*admin_key),
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
        settings: [ProgramConfiguration; N],
        admin_signer: &Keypair,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.payer_signer;

        let configure_program_ixs = settings
            .into_iter()
            .map(|setting| {
                try_build_instruction(
                    &ID,
                    ConfigureProgramAccounts::new(&admin_signer.pubkey()),
                    &PassportInstructionData::ConfigureProgram(setting),
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

    pub async fn request_access(
        &mut self,
        service_key: &Pubkey,
        access_mode: AccessMode,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.payer_signer;

        let request_access_ix = try_build_instruction(
            &ID,
            RequestAccessAccounts::new(&payer_signer.pubkey(), service_key),
            &PassportInstructionData::RequestAccess(access_mode),
        )
        .unwrap();

        self.cached_blockhash = process_instructions_for_test(
            &mut self.banks_client,
            &self.cached_blockhash,
            &[request_access_ix],
            &[payer_signer],
        )
        .await?;

        Ok(self)
    }

    pub async fn grant_access(
        &mut self,
        dz_ledger_sentinel: &Keypair,
        access_request_key: &Pubkey,
        rent_beneficiary_key: &Pubkey,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.payer_signer;

        let grant_access_ix = try_build_instruction(
            &ID,
            GrantAccessAccounts::new(
                &dz_ledger_sentinel.pubkey(),
                access_request_key,
                rent_beneficiary_key,
            ),
            &PassportInstructionData::GrantAccess,
        )
        .unwrap();

        self.cached_blockhash = process_instructions_for_test(
            &mut self.banks_client,
            &self.cached_blockhash,
            &[grant_access_ix],
            &[payer_signer, dz_ledger_sentinel],
        )
        .await?;

        Ok(self)
    }

    pub async fn deny_access(
        &mut self,
        dz_ledger_sentinel: &Keypair,
        access_request_key: &Pubkey,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.payer_signer;

        let deny_access_ix = try_build_instruction(
            &ID,
            DenyAccessAccounts::new(&dz_ledger_sentinel.pubkey(), access_request_key),
            &PassportInstructionData::DenyAccess,
        )
        .unwrap();

        self.cached_blockhash = process_instructions_for_test(
            &mut self.banks_client,
            &self.cached_blockhash,
            &[deny_access_ix],
            &[payer_signer, dz_ledger_sentinel],
        )
        .await?;

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

    pub async fn fetch_access_request(&self, service_key: &Pubkey) -> (Pubkey, AccessRequest) {
        let access_request_key = AccessRequest::find_address(service_key).0;

        let access_request_account_data = self
            .banks_client
            .get_account(access_request_key)
            .await
            .unwrap()
            .unwrap()
            .data;

        (
            access_request_key,
            *checked_from_bytes_with_discriminator(&access_request_account_data)
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
