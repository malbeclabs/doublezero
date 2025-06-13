#[cfg(test)]
pub mod utils {
    use doublezero_sdk::{AccountData, AccountType, DoubleZeroClient, MockDoubleZeroClient};
    use doublezero_serviceability::{
        pda::{get_device_pda, get_globalstate_pda, get_link_pda, get_user_pda},
        state::globalstate::GlobalState,
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    pub fn create_test_client() -> MockDoubleZeroClient {
        let mut client = MockDoubleZeroClient::new();

        // Program ID
        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);

        // Global State
        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
        let globalstate = GlobalState {
            account_type: AccountType::GlobalState,
            bump_seed: 0,
            account_index: 0,
            foundation_allowlist: vec![],
            device_allowlist: vec![],
            user_allowlist: vec![],
        };

        client
            .expect_get()
            .with(predicate::eq(globalstate_pubkey))
            .returning(move |_| Ok(AccountData::GlobalState(globalstate.clone())));

        let payer = Pubkey::new_unique();
        client.expect_get_payer().returning(move || payer);

        client
    }

    pub fn get_device_bump_seed(client: &MockDoubleZeroClient) -> u8 {
        let (_, bump_seed) = get_device_pda(&client.get_program_id(), 0);
        bump_seed
    }

    pub fn get_tunnel_bump_seed(client: &MockDoubleZeroClient) -> u8 {
        let (_, bump_seed) = get_link_pda(&client.get_program_id(), 0);
        bump_seed
    }

    pub fn get_user_bump_seed(client: &MockDoubleZeroClient) -> u8 {
        let (_, bump_seed) = get_user_pda(&client.get_program_id(), 0);
        bump_seed
    }
}

#[cfg(test)]
mod ledger_tests {
    use bollard::Docker;
    use cargo_metadata::MetadataCommand;
    use doublezero_cli::doublezerocommand::{CliCommand, CliCommandImpl};
    use doublezero_sdk::{
        commands::{
            globalconfig::set::SetGlobalConfigCommand, globalstate::init::InitGlobalStateCommand,
        },
        networkv4_parse, ClientConfig, DZClient,
    };
    use solana_client::rpc_client::RpcClient;
    use solana_sdk::{
        commitment_config::CommitmentConfig,
        pubkey::Pubkey,
        signature::{Keypair, Signer},
    };
    use std::{env, fs::File, io::Write, path::PathBuf};
    use tempfile::{tempdir, TempDir};
    use testcontainers::{
        core::{IntoContainerPort, Mount},
        runners::AsyncRunner,
        ContainerAsync, GenericImage, ImageExt,
    };
    use tokio::time::{sleep, Duration};

    use crate::{activator::Activator, influxdb_metrics_service::create_influxdb_metrics_service};

    // NOTE: The solana RPC client needs to be on a multi-threaded runtime.
    #[tokio::test(flavor = "multi_thread")]
    async fn test_with_ledger() {
        let temp_dir = tempdir().unwrap();

        // Start a ledger container.
        let (_container, program_id, ledger_rpc_url, ledger_ws_url) =
            start_ledger_container(&temp_dir).await;

        // Initialize and fund a manager keypair.
        let manager_keypair_path = temp_dir.path().join("dz-manager-keypair.json");
        let manager_keypair = generate_and_save_keypair(manager_keypair_path.clone());
        solana_request_airdrop(ledger_rpc_url.clone(), manager_keypair.pubkey()).await;

        // Configure doublezero CLI, since we use it to initialize the global state and config.
        configure_doublezero_cli(&temp_dir, &manager_keypair_path, &program_id);

        // Initialize smartcontract global state and config onchain.
        let dzclient = DZClient::new(
            Some(ledger_rpc_url.clone()),
            Some(ledger_ws_url.clone()),
            Some(program_id.to_string()),
            Some(manager_keypair_path.display().to_string()),
        )
        .unwrap();
        let client = CliCommandImpl::new(&dzclient);
        client.init_global_state(InitGlobalStateCommand {}).unwrap();
        client
            .set_globalconfig(SetGlobalConfigCommand {
                local_asn: 65000,
                remote_asn: 65342,
                tunnel_tunnel_block: networkv4_parse("172.16.0.0/16"),
                user_tunnel_block: networkv4_parse("169.254.0.0/16"),
                multicastgroup_block: networkv4_parse("233.84.178.0/24"),
            })
            .unwrap();

        // The activator checks for a global config onchain in the constructor.
        let (metrics_service, _) = create_influxdb_metrics_service(None, None, None, None);
        let _activator = Activator::new(
            Some(ledger_rpc_url.clone()),
            Some(ledger_ws_url.clone()),
            Some(program_id.to_string()),
            Some(manager_keypair_path.display().to_string()),
            metrics_service,
        )
        .await
        .unwrap();

        println!("Activator initialized");
    }

    pub async fn start_ledger_container(
        temp_dir: &TempDir,
    ) -> (ContainerAsync<GenericImage>, Pubkey, String, String) {
        let workspace_root = MetadataCommand::new().exec().unwrap().workspace_root;
        let dotenv_path = workspace_root.join("e2e/.env.local");
        dotenv::from_path(dotenv_path).ok();

        let program_keypair_path = temp_dir.path().join("program_keypair.json");
        let program_keypair = generate_and_save_keypair(program_keypair_path.clone());

        let program_id = program_keypair.pubkey();

        let image = env::var("DZ_LEDGER_IMAGE").unwrap();
        let docker = Docker::connect_with_local_defaults().unwrap();
        docker.inspect_image(&image).await.unwrap_or_else(|_| {
            panic!("Docker image \"{image}\" not found. You might need to run `make build-e2e` in the workspace root to build it.")
        });
        let (image, tag) = image.split_once(':').unwrap();
        println!("Ledger image: {image}:{tag}");

        let container_program_keypair_path = "/etc/doublezero/dz-program-keypair.json";
        let image = GenericImage::new(image, tag)
            .with_exposed_port(8899.tcp())
            .with_exposed_port(8900.tcp())
            .with_mount(Mount::bind_mount(
                program_keypair_path.as_path().to_str().unwrap(),
                container_program_keypair_path,
            ))
            .with_env_var("DZ_PROGRAM_KEYPAIR_PATH", container_program_keypair_path);

        let container = image.start().await.unwrap();

        let rpc_port = container.get_host_port_ipv4(8899.tcp()).await.unwrap();
        let ws_port = container.get_host_port_ipv4(8900.tcp()).await.unwrap();

        let rpc_url = format!("http://localhost:{rpc_port}");
        let ws_url = format!("ws://localhost:{ws_port}");

        (container, program_id, rpc_url, ws_url)
    }

    pub fn generate_and_save_keypair(path: PathBuf) -> Keypair {
        let keypair = Keypair::new();
        let serialized = serde_json::to_string(&keypair.to_bytes().to_vec()).unwrap();
        let mut file = File::create(path).unwrap();
        file.write_all(serialized.as_bytes()).unwrap();

        keypair
    }

    pub fn configure_doublezero_cli(
        temp_dir: &TempDir,
        keypair_path: &PathBuf,
        program_id: &Pubkey,
    ) {
        let cli_config_path = temp_dir.path().join("cli-config.yml");
        env::set_var(
            "DOUBLEZERO_CONFIG_FILE",
            cli_config_path.display().to_string(),
        );
        let mut file = File::create(cli_config_path).unwrap();
        file.write_all(
            serde_json::to_string(&ClientConfig {
                keypair_path: keypair_path.display().to_string(),
                program_id: Some(program_id.to_string()),
                ..Default::default()
            })
            .unwrap()
            .as_bytes(),
        )
        .unwrap();
    }

    pub async fn solana_request_airdrop(rpc_url: String, account: Pubkey) {
        let client = RpcClient::new_with_commitment(rpc_url, CommitmentConfig::confirmed());

        let lamports = 1_000_000_000;
        let sig = loop {
            match client.request_airdrop(&account, lamports) {
                Ok(sig) => break sig,
                Err(e) => {
                    println!("Airdrop failed, retrying...: {e}");
                    sleep(Duration::from_secs(2)).await;
                }
            }
        };

        println!("Airdrop signature: {}", sig);

        let mut retries = 10;
        loop {
            match client.get_signature_status_with_commitment(&sig, CommitmentConfig::confirmed()) {
                Ok(Some(result)) if result.is_ok() => break,
                Ok(Some(result)) => panic!("Transaction failed: {:?}", result),
                Ok(None) => {
                    if retries == 0 {
                        panic!("Transaction not confirmed after retries");
                    }
                    retries -= 1;
                    sleep(Duration::from_secs(2)).await;
                }
                Err(e) => panic!("Error fetching signature status: {e}"),
            }
        }

        let balance = client.get_balance(&account).expect("Failed to get balance");
        println!("Airdrop confirmed, new balance = {} lamports", balance);
    }
}
