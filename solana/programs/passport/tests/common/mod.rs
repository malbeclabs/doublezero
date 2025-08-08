#![allow(dead_code)]

use doublezero_passport::{
    instruction::{
        account::{
            ConfigureProgramAccounts, DenyAccessAccounts, GrantAccessAccounts,
            InitializeProgramAccounts, RequestAccessAccounts, SetAdminAccounts,
        },
        AccessMode, PassportInstructionData, ProgramConfiguration,
    },
    state::{AccessRequest, ProgramConfig},
    ID,
};
use doublezero_program_tools::{
    instruction::try_build_instruction, zero_copy::checked_from_bytes_with_discriminator,
};
use solana_loader_v3_interface::{get_program_data_address, state::UpgradeableLoaderState};
use solana_program_test::{BanksClient, BanksClientError, ProgramTest};
use solana_pubkey::Pubkey;
use solana_sdk::{
    account::Account,
    hash::Hash,
    instruction::Instruction,
    message::{v0::Message, VersionedMessage},
    signature::{Keypair, Signer},
    transaction::VersionedTransaction,
};

pub struct TestAccount {
    pub key: Pubkey,
    pub info: Account,
}

pub struct ProgramTestWithOwner {
    pub banks_client: BanksClient,
    pub payer_signer: Keypair,
    pub recent_blockhash: Hash,
    pub owner_signer: Keypair,
}

pub async fn start_test_with_accounts(accounts: Vec<TestAccount>) -> ProgramTestWithOwner {
    let mut program_test = ProgramTest::new("doublezero_passport", ID, None);
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

    for TestAccount { key, info } in accounts.into_iter() {
        program_test.add_account(key, info);
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
    start_test_with_accounts(Default::default()).await
}

pub fn program_data_key() -> Pubkey {
    get_program_data_address(&ID)
}

impl ProgramTestWithOwner {
    pub async fn transfer_lamports(
        &mut self,
        dst_key: &Pubkey,
        amount: u64,
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.payer_signer;

        let transfer_ix =
            solana_system_interface::instruction::transfer(&payer_signer.pubkey(), dst_key, amount);

        let new_blockhash = process_instructions_for_test(
            &self.banks_client,
            self.recent_blockhash,
            &[transfer_ix],
            &[payer_signer],
        )
        .await?;

        self.recent_blockhash = new_blockhash;

        Ok(self)
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

    pub async fn set_admin(&mut self, admin_key: &Pubkey) -> Result<&mut Self, BanksClientError> {
        let owner_signer = &self.owner_signer;
        let payer_signer = &self.payer_signer;

        let set_admin_ix = try_build_instruction(
            &ID,
            SetAdminAccounts::new(&program_data_key(), &owner_signer.pubkey()),
            &PassportInstructionData::SetAdmin(*admin_key),
        )
        .unwrap();

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

    pub async fn request_access(
        &mut self,
        service_key: &Pubkey,
        validator_id: &Pubkey,
        signature: [u8; 64],
    ) -> Result<&mut Self, BanksClientError> {
        let payer_signer = &self.payer_signer;

        let request_access_ix = try_build_instruction(
            &ID,
            RequestAccessAccounts::new(&payer_signer.pubkey(), service_key),
            &PassportInstructionData::RequestAccess(AccessMode::SolanaValidator {
                validator_id: *validator_id,
                service_key: *service_key,
                ed25519_signature: signature,
            }),
        )
        .unwrap();

        let new_blockhash = process_instructions_for_test(
            &self.banks_client,
            self.recent_blockhash,
            &[request_access_ix],
            &[payer_signer],
        )
        .await?;

        self.recent_blockhash = new_blockhash;

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

        let new_blockhash = process_instructions_for_test(
            &self.banks_client,
            self.recent_blockhash,
            &[grant_access_ix],
            &[payer_signer, dz_ledger_sentinel],
        )
        .await?;

        self.recent_blockhash = new_blockhash;

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

        let new_blockhash = process_instructions_for_test(
            &self.banks_client,
            self.recent_blockhash,
            &[deny_access_ix],
            &[payer_signer, dz_ledger_sentinel],
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
