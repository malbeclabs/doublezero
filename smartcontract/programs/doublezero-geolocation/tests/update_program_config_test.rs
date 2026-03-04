use doublezero_geolocation::{
    entrypoint::process_instruction,
    error::GeolocationError,
    instructions::GeolocationInstruction,
    pda::get_program_config_pda,
    processors::program_config::{init::InitProgramConfigArgs, update::UpdateProgramConfigArgs},
    state::program_config::GeolocationProgramConfig,
};
use solana_loader_v3_interface::state::UpgradeableLoaderState;
#[allow(deprecated)]
use solana_program::bpf_loader_upgradeable;
use solana_program_test::*;
use solana_sdk::{
    account::AccountSharedData,
    instruction::{AccountMeta, Instruction, InstructionError},
    pubkey::Pubkey,
    signature::Signer,
    transaction::{Transaction, TransactionError},
};

/// Builds a bincode-serialized UpgradeableLoaderState::ProgramData account
/// with the given upgrade authority. This is needed because ProgramTest with
/// processor!() deploys programs as builtins without creating the BPF
/// upgradeable loader program_data account that the geolocation program
/// requires for upgrade-authority verification.
fn build_program_data_account(upgrade_authority: &Pubkey) -> AccountSharedData {
    let state = UpgradeableLoaderState::ProgramData {
        slot: 0,
        upgrade_authority_address: Some(*upgrade_authority),
    };
    let data = bincode::serde::encode_to_vec(state, bincode::config::legacy()).unwrap();

    let mut account =
        AccountSharedData::new(1_000_000_000, data.len(), &bpf_loader_upgradeable::id());
    account.set_data_from_slice(&data);
    account
}

fn build_accounts(program_id: &Pubkey, payer: &Pubkey) -> Vec<AccountMeta> {
    let (program_config_pda, _) = get_program_config_pda(program_id);
    let (program_data_pda, _) =
        Pubkey::find_program_address(&[program_id.as_ref()], &bpf_loader_upgradeable::id());

    vec![
        AccountMeta::new(program_config_pda, false),
        AccountMeta::new_readonly(program_data_pda, false),
        AccountMeta::new(*payer, true),
        AccountMeta::new_readonly(solana_program::system_program::id(), false),
    ]
}

fn build_instruction(
    program_id: &Pubkey,
    instruction: &GeolocationInstruction,
    accounts: Vec<AccountMeta>,
) -> Instruction {
    Instruction::new_with_bytes(*program_id, &borsh::to_vec(instruction).unwrap(), accounts)
}

async fn read_program_config(
    banks_client: &mut BanksClient,
    program_id: &Pubkey,
) -> GeolocationProgramConfig {
    let (pda, _) = get_program_config_pda(program_id);
    let account = banks_client
        .get_account(pda)
        .await
        .unwrap()
        .expect("ProgramConfig account must exist");
    GeolocationProgramConfig::try_from(&account.data[..]).unwrap()
}

async fn setup() -> (BanksClient, solana_sdk::signature::Keypair, Pubkey) {
    let program_id = Pubkey::new_unique();
    let program_test = ProgramTest::new(
        "doublezero_geolocation",
        program_id,
        processor!(process_instruction),
    );
    let mut context = program_test.start_with_context().await;

    // Inject the program_data account that the upgrade-authority check expects.
    let (program_data_pda, _) =
        Pubkey::find_program_address(&[program_id.as_ref()], &bpf_loader_upgradeable::id());
    let program_data_account = build_program_data_account(&context.payer.pubkey());
    context.set_account(&program_data_pda, &program_data_account);

    (context.banks_client, context.payer, program_id)
}

async fn init_program_config(
    banks_client: &mut BanksClient,
    payer: &solana_sdk::signature::Keypair,
    program_id: &Pubkey,
) {
    let accounts = build_accounts(program_id, &payer.pubkey());
    let ix = build_instruction(
        program_id,
        &GeolocationInstruction::InitProgramConfig(InitProgramConfigArgs {}),
        accounts,
    );
    let blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let mut tx = Transaction::new_with_payer(&[ix], Some(&payer.pubkey()));
    tx.sign(&[payer], blockhash);
    banks_client.process_transaction(tx).await.unwrap();
}

async fn send_update(
    banks_client: &mut BanksClient,
    payer: &solana_sdk::signature::Keypair,
    program_id: &Pubkey,
    args: UpdateProgramConfigArgs,
) -> Result<(), BanksClientError> {
    let accounts = build_accounts(program_id, &payer.pubkey());
    let ix = build_instruction(
        program_id,
        &GeolocationInstruction::UpdateProgramConfig(args),
        accounts,
    );
    let blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let mut tx = Transaction::new_with_payer(&[ix], Some(&payer.pubkey()));
    tx.sign(&[payer], blockhash);
    banks_client.process_transaction(tx).await
}

fn assert_geolocation_error(result: Result<(), BanksClientError>, expected: GeolocationError) {
    let expected_code = expected.clone() as u32;
    match result {
        Ok(_) => panic!("Expected error {:?}, but got Ok", expected),
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            _,
            InstructionError::Custom(code),
        ))) => assert_eq!(
            code, expected_code,
            "Expected {:?} ({}), got {}",
            expected, expected_code, code
        ),
        Err(other) => panic!("Expected {:?}, got {:?}", expected, other),
    }
}

#[tokio::test]
async fn test_update_program_config_version_downgrade_below_min_compatible_version() {
    let (mut banks_client, payer, program_id) = setup().await;
    init_program_config(&mut banks_client, &payer, &program_id).await;

    // Bump to version=5, min_compatible_version=3
    send_update(
        &mut banks_client,
        &payer,
        &program_id,
        UpdateProgramConfigArgs {
            version: Some(5),
            min_compatible_version: Some(3),
        },
    )
    .await
    .unwrap();

    let config = read_program_config(&mut banks_client, &program_id).await;
    assert_eq!(config.version, 5);
    assert_eq!(config.min_compatible_version, 3);

    // Attempt to downgrade version below the existing min_compatible_version
    let result = send_update(
        &mut banks_client,
        &payer,
        &program_id,
        UpdateProgramConfigArgs {
            version: Some(1),
            min_compatible_version: None,
        },
    )
    .await;
    assert_geolocation_error(result, GeolocationError::InvalidMinCompatibleVersion);

    // State must be unchanged
    let config = read_program_config(&mut banks_client, &program_id).await;
    assert_eq!(config.version, 5);
    assert_eq!(config.min_compatible_version, 3);
}

#[tokio::test]
async fn test_update_program_config_min_compatible_version_exceeds_version() {
    let (mut banks_client, payer, program_id) = setup().await;
    init_program_config(&mut banks_client, &payer, &program_id).await;

    // min_compatible_version=5 exceeds current version=1
    let result = send_update(
        &mut banks_client,
        &payer,
        &program_id,
        UpdateProgramConfigArgs {
            version: None,
            min_compatible_version: Some(5),
        },
    )
    .await;
    assert_geolocation_error(result, GeolocationError::InvalidMinCompatibleVersion);
}

#[tokio::test]
async fn test_update_program_config_success() {
    let (mut banks_client, payer, program_id) = setup().await;
    init_program_config(&mut banks_client, &payer, &program_id).await;

    let config = read_program_config(&mut banks_client, &program_id).await;
    assert_eq!(config.version, 1);
    assert_eq!(config.min_compatible_version, 1);

    // Update version to 5
    send_update(
        &mut banks_client,
        &payer,
        &program_id,
        UpdateProgramConfigArgs {
            version: Some(5),
            min_compatible_version: None,
        },
    )
    .await
    .unwrap();

    // Update min_compatible_version to 3
    send_update(
        &mut banks_client,
        &payer,
        &program_id,
        UpdateProgramConfigArgs {
            version: None,
            min_compatible_version: Some(3),
        },
    )
    .await
    .unwrap();

    let config = read_program_config(&mut banks_client, &program_id).await;
    assert_eq!(config.version, 5);
    assert_eq!(config.min_compatible_version, 3);
}
