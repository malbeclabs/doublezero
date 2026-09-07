//! Minimal harness for the builder-stake program tests.
//!
//! Instruction builders live here rather than in the crate because tests are the only caller so
//! far. Move them into the crate when the relayer or a CLI needs them.

#![allow(dead_code)]

use doublezero_builder_stake::{
    instruction::BuilderStakeInstructionData,
    state::{self, BuilderStake, ProgramConfig},
    DOUBLEZERO_MINT_KEY, ID,
};
use solana_loader_v3_interface::{get_program_data_address, state::UpgradeableLoaderState};
use solana_program_test::{ProgramTest, ProgramTestContext};
use solana_sdk::{
    account::Account,
    instruction::{AccountMeta, Instruction},
    program_pack::Pack,
    pubkey::Pubkey,
    signature::{Keypair, Signer},
    transaction::Transaction,
    transport::TransportError,
};
use solana_system_interface::program as system_program;
use spl_token_interface::state::{
    Account as TokenAccount, AccountState as SplTokenAccountState, Mint,
};

/// Enough 2Z to cover any tier in the tests without thinking about it.
pub const TEST_2Z_SUPPLY: u64 = 1_000_000_000_000_000;

pub struct TestSetup {
    pub context: ProgramTestContext,
    /// The program's upgrade authority, which is what `SetAdmin` checks.
    pub upgrade_authority: Keypair,
    pub builder: Keypair,
    /// A 2Z token account owned by `builder`, pre-funded.
    pub builder_2z_key: Pubkey,
}

pub async fn start_test() -> TestSetup {
    let mut program_test = ProgramTest::new("doublezero_builder_stake", ID, None);
    program_test.prefer_bpf(true);

    let upgrade_authority = Keypair::new();
    let builder = Keypair::new();

    // Stand in for the BPF Upgradeable Loader's program data account so `SetAdmin` can check the
    // upgrade authority.
    program_test.add_account(
        get_program_data_address(&ID),
        Account {
            lamports: 69,
            data: bincode::serialize(&UpgradeableLoaderState::ProgramData {
                slot: 0,
                upgrade_authority_address: Some(upgrade_authority.pubkey()),
            })
            .unwrap(),
            ..Default::default()
        },
    );

    let mut mint_data = vec![0; Mint::LEN];
    Mint {
        mint_authority: upgrade_authority.pubkey().into(),
        supply: TEST_2Z_SUPPLY,
        decimals: 8,
        is_initialized: true,
        freeze_authority: None.into(),
    }
    .pack_into_slice(&mut mint_data);
    program_test.add_account(
        DOUBLEZERO_MINT_KEY,
        Account {
            lamports: 69,
            owner: spl_token_interface::ID,
            data: mint_data,
            ..Default::default()
        },
    );

    let builder_2z_key = Pubkey::new_unique();
    program_test.add_account(
        builder_2z_key,
        token_account(&builder.pubkey(), TEST_2Z_SUPPLY),
    );

    // The builder pays rent for its own stake and token account.
    program_test.add_account(
        builder.pubkey(),
        Account {
            lamports: 10_000_000_000,
            owner: system_program::ID,
            ..Default::default()
        },
    );

    let context = program_test.start_with_context().await;

    TestSetup {
        context,
        upgrade_authority,
        builder,
        builder_2z_key,
    }
}

pub fn token_account(owner: &Pubkey, amount: u64) -> Account {
    let mut data = vec![0; TokenAccount::LEN];
    TokenAccount {
        mint: DOUBLEZERO_MINT_KEY,
        owner: *owner,
        amount,
        state: SplTokenAccountState::Initialized,
        ..Default::default()
    }
    .pack_into_slice(&mut data);

    Account {
        lamports: 69,
        owner: spl_token_interface::ID,
        data,
        ..Default::default()
    }
}

impl TestSetup {
    pub async fn send(
        &mut self,
        ix: Instruction,
        signers: &[&Keypair],
    ) -> Result<(), TransportError> {
        let blockhash = self
            .context
            .banks_client
            .get_latest_blockhash()
            .await
            .unwrap();
        let payer = self.context.payer.insecure_clone();

        let mut all: Vec<&Keypair> = vec![&payer];
        all.extend_from_slice(signers);

        let tx = Transaction::new_signed_with_payer(&[ix], Some(&payer.pubkey()), &all, blockhash);
        self.context
            .banks_client
            .process_transaction(tx)
            .await
            .map_err(Into::into)
    }

    /// Initialize, set the admin, and unpause. The state every test that touches a stake needs.
    pub async fn initialize_and_unpause(&mut self) {
        let payer = self.context.payer.pubkey();
        self.send(initialize_program(&payer), &[]).await.unwrap();

        let admin = self.upgrade_authority.pubkey();
        let upgrade_authority = self.upgrade_authority.insecure_clone();
        self.send(set_admin(&admin), &[&upgrade_authority])
            .await
            .unwrap();
        self.send(set_paused(&admin, false), &[&upgrade_authority])
            .await
            .unwrap();
    }

    pub async fn read_builder_stake(&mut self, key: &Pubkey) -> BuilderStake {
        let data = self
            .context
            .banks_client
            .get_account(*key)
            .await
            .unwrap()
            .unwrap()
            .data;
        *doublezero_program_tools::zero_copy::checked_from_bytes_with_discriminator::<BuilderStake>(
            &data,
        )
        .unwrap()
        .0
    }

    pub async fn read_program_config(&mut self) -> ProgramConfig {
        let data = self
            .context
            .banks_client
            .get_account(ProgramConfig::find_address().0)
            .await
            .unwrap()
            .unwrap()
            .data;
        *doublezero_program_tools::zero_copy::checked_from_bytes_with_discriminator::<ProgramConfig>(
            &data,
        )
        .unwrap()
        .0
    }

    pub async fn token_amount(&mut self, key: &Pubkey) -> u64 {
        let data = self
            .context
            .banks_client
            .get_account(*key)
            .await
            .unwrap()
            .unwrap()
            .data;
        TokenAccount::unpack(&data).unwrap().amount
    }
}

//
// Instruction builders.
//

fn encode(data: &BuilderStakeInstructionData) -> Vec<u8> {
    borsh::to_vec(data).unwrap()
}

pub fn initialize_program(payer: &Pubkey) -> Instruction {
    Instruction {
        program_id: ID,
        accounts: vec![
            AccountMeta::new(*payer, true),
            AccountMeta::new(ProgramConfig::find_address().0, false),
            AccountMeta::new_readonly(system_program::ID, false),
        ],
        data: encode(&BuilderStakeInstructionData::InitializeProgram),
    }
}

pub fn set_admin(admin: &Pubkey) -> Instruction {
    Instruction {
        program_id: ID,
        accounts: vec![
            AccountMeta::new_readonly(get_program_data_address(&ID), false),
            AccountMeta::new_readonly(*admin, true),
            AccountMeta::new(ProgramConfig::find_address().0, false),
        ],
        data: encode(&BuilderStakeInstructionData::SetAdmin(*admin)),
    }
}

pub fn set_paused(admin: &Pubkey, paused: bool) -> Instruction {
    use doublezero_builder_stake::instruction::{ProgramConfiguration, ProgramFlagConfiguration};

    Instruction {
        program_id: ID,
        accounts: vec![
            AccountMeta::new_readonly(*admin, true),
            AccountMeta::new(ProgramConfig::find_address().0, false),
        ],
        data: encode(&BuilderStakeInstructionData::ConfigureProgram(
            ProgramConfiguration::Flag(ProgramFlagConfiguration::IsPaused(paused)),
        )),
    }
}

pub fn initialize_builder_stake(
    builder: &Pubkey,
    stake_index: u64,
    committed_rate_bits_per_sec: u64,
) -> Instruction {
    let stake_key = BuilderStake::find_address(builder, stake_index).0;

    Instruction {
        program_id: ID,
        accounts: vec![
            AccountMeta::new_readonly(ProgramConfig::find_address().0, false),
            AccountMeta::new(*builder, true),
            AccountMeta::new(stake_key, false),
            AccountMeta::new(state::find_2z_token_pda_address(&stake_key).0, false),
            AccountMeta::new_readonly(DOUBLEZERO_MINT_KEY, false),
            AccountMeta::new_readonly(spl_token_interface::ID, false),
            AccountMeta::new_readonly(system_program::ID, false),
        ],
        data: encode(&BuilderStakeInstructionData::InitializeBuilderStake {
            stake_index,
            committed_rate_bits_per_sec,
        }),
    }
}

pub fn deposit(builder: &Pubkey, stake_index: u64, source: &Pubkey, amount: u64) -> Instruction {
    let stake_key = BuilderStake::find_address(builder, stake_index).0;

    Instruction {
        program_id: ID,
        accounts: vec![
            AccountMeta::new_readonly(ProgramConfig::find_address().0, false),
            AccountMeta::new_readonly(*builder, true),
            AccountMeta::new(stake_key, false),
            AccountMeta::new(state::find_2z_token_pda_address(&stake_key).0, false),
            AccountMeta::new(*source, false),
            AccountMeta::new_readonly(spl_token_interface::ID, false),
        ],
        data: encode(&BuilderStakeInstructionData::Deposit { amount }),
    }
}
