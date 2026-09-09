//! Minimal harness for the builder-stake program tests.
//!
//! Instruction builders live here rather than in the crate because tests are the only caller so
//! far. Move them into the crate when the relayer or a CLI needs them.

#![allow(dead_code)]

use doublezero_builder_stake::{
    state::{BuilderStake, ProgramConfig},
    DOUBLEZERO_MINT_KEY, ID,
};
use solana_loader_v3_interface::{get_program_data_address, state::UpgradeableLoaderState};
use solana_program_test::{ProgramTest, ProgramTestContext};
use solana_sdk::{
    account::Account,
    instruction::{Instruction, InstructionError},
    program_pack::Pack,
    pubkey::Pubkey,
    signature::{Keypair, Signer},
    transaction::{Transaction, TransactionError},
    transport::TransportError,
};
use solana_system_interface::program as system_program;
use spl_token_interface::state::{
    Account as TokenAccount, AccountState as SplTokenAccountState, Mint,
};

/// Enough 2Z to cover any tier in the tests without thinking about it.
pub const TEST_2Z_SUPPLY: u64 = 1_000_000_000_000_000;

/// Small, ordered stand-ins for RFC-28's $100k/$200k/$500k, so a test can assert on exact numbers.
pub const TIER_1GBPS: u64 = 100_000;
pub const TIER_5GBPS: u64 = 200_000;
pub const TIER_UNMETERED: u64 = 500_000;

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

/// Simulate one instruction and return the error together with the program's logs.
///
/// Several rejections share a `ProgramError`: a paused program and a hold that has not elapsed
/// both return `InvalidAccountData`. The error alone cannot tell a test which check fired, so the
/// log line is what pins it, the way the revenue-distribution tests do.
pub async fn simulate_error(
    t: &mut TestSetup,
    ix: Instruction,
    signers: &[&Keypair],
) -> (TransactionError, Vec<String>) {
    let blockhash = t.context.get_new_latest_blockhash().await.unwrap();
    let payer = t.context.payer.insecure_clone();

    let mut all: Vec<&Keypair> = vec![&payer];
    all.extend_from_slice(signers);

    let tx = Transaction::new_signed_with_payer(&[ix], Some(&payer.pubkey()), &all, blockhash);
    let simulated = t
        .context
        .banks_client
        .simulate_transaction(tx)
        .await
        .unwrap();

    (
        simulated.result.unwrap().unwrap_err(),
        simulated.simulation_details.unwrap().logs,
    )
}

/// Assert the program logged `expected` somewhere in `logs`.
///
/// By content rather than by index: the index moves whenever a `msg!` is added earlier in the
/// instruction, and what the test cares about is which check fired.
pub fn assert_logged(logs: &[String], expected: &str) {
    assert!(
        logs.iter().any(|l| l.contains(expected)),
        "expected a log containing {expected:?}, got {logs:#?}"
    );
}

/// Assert a transaction failed with exactly `expected` at instruction 0. Without this a test that
/// expects one rejection passes on any other, and a later change can move the real reject.
pub fn assert_instruction_error(err: TransportError, expected: InstructionError) {
    match err {
        TransportError::TransactionError(TransactionError::InstructionError(0, actual)) => {
            assert_eq!(actual, expected)
        }
        other => panic!("expected InstructionError(0, {expected:?}), got {other:?}"),
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
    /// Send one instruction, always on a blockhash no earlier send used.
    ///
    /// Two identical instructions in a row would otherwise build byte-identical transactions,
    /// which share a signature, and the runtime drops the second as already processed. The test
    /// then sees the first one's effect and reads it as the second having done nothing.
    pub async fn send(
        &mut self,
        ix: Instruction,
        signers: &[&Keypair],
    ) -> Result<(), TransportError> {
        let blockhash = self.context.get_new_latest_blockhash().await.unwrap();
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

    /// Initialize, set the admin, set the tier table, and unpause. The state every test that
    /// touches a stake needs.
    pub async fn initialize_and_unpause(&mut self) {
        self.initialize_and_set_admin().await;

        let admin = self.upgrade_authority.pubkey();
        let upgrade_authority = self.upgrade_authority.insecure_clone();
        self.send(
            set_tier_parameters(&admin, TIER_1GBPS, TIER_5GBPS, TIER_UNMETERED),
            &[&upgrade_authority],
        )
        .await
        .unwrap();
        self.send(set_paused(&admin, false), &[&upgrade_authority])
            .await
            .unwrap();
    }

    /// Initialize and set the admin, leaving the tier table unset and the program paused.
    pub async fn initialize_and_set_admin(&mut self) {
        let payer = self.context.payer.pubkey();
        self.send(initialize_program(&payer), &[]).await.unwrap();

        let admin = self.upgrade_authority.pubkey();
        let upgrade_authority = self.upgrade_authority.insecure_clone();
        self.send(set_admin(&admin), &[&upgrade_authority])
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

// The instruction builders moved into the crate, where a caller outside these tests can reach
// them. Re-exported so the test files that use them read unchanged.
pub use doublezero_builder_stake::instruction::builders::*;
