use crate::processor::{Processor, ProcessorStateless};
use doublezero_cli::{checkversion::check_version, doublezerocommand::CliCommandImpl};
use doublezero_sdk::{
    doublezeroclient::{AsyncDoubleZeroClient, DoubleZeroClient},
    rpckeyedaccount_decode::rpckeyedaccount_decode,
    AccountData, AsyncDZClient, DZClient, GetGlobalStateCommand, ProgramVersion,
};
use doublezero_serviceability::state::feature_flags::{is_feature_enabled, FeatureFlag};
use futures::stream::StreamExt;
use log::{error, info};
use solana_sdk::pubkey::Pubkey;
use std::{
    future::Future,
    path::PathBuf,
    sync::{atomic::AtomicBool, Arc},
};
use tokio::sync::mpsc;

pub async fn run_activator(
    rpc_url: Option<String>,
    websocket_url: Option<String>,
    program_id: Option<String>,
    keypair: Option<PathBuf>,
) -> eyre::Result<()> {
    let client = create_client(rpc_url, websocket_url, program_id, keypair)?;

    let rpc_url_clone = client.get_rpc().clone();
    let ws_url_clone = client.get_ws().clone();
    let program_id = *client.get_program_id();
    let async_client_factory =
        move || AsyncDZClient::new(rpc_url_clone.clone(), ws_url_clone.clone(), program_id);

    version_check(client.as_ref())?;

    let use_onchain_allocation = read_onchain_allocation_flag(client.as_ref())?;

    run_activator_with_client(client, async_client_factory, use_onchain_allocation).await
}

async fn run_activator_with_client<C, F, R, A>(
    client: Arc<C>,
    async_client_factory: F,
    use_onchain_allocation: bool,
) -> eyre::Result<()>
where
    C: DoubleZeroClient + Send + Sync + 'static,
    F: Fn() -> R + Send + Sync + 'static,
    R: Future<Output = eyre::Result<A>> + Send + 'static,
    A: AsyncDoubleZeroClient + Send + Sync + 'static,
{
    if use_onchain_allocation {
        run_activator_stateless(client, async_client_factory).await
    } else {
        run_activator_stateful(client, async_client_factory).await
    }
}

async fn run_activator_stateful<C, F, R, A>(
    client: Arc<C>,
    async_client_factory: F,
) -> eyre::Result<()>
where
    C: DoubleZeroClient + Send + Sync + 'static,
    F: Fn() -> R + Send + Sync + 'static,
    R: Future<Output = eyre::Result<A>> + Send + 'static,
    A: AsyncDoubleZeroClient + Send + Sync + 'static,
{
    loop {
        info!("Activator handler loop started (stateful mode)");

        let (tx, rx) = mpsc::channel(128);
        let mut processor = Processor::new(rx, client.clone())?;

        let shutdown = Arc::new(AtomicBool::new(false));

        tokio::select! {
            biased;
            _ = crate::listen_for_shutdown()? => {
                info!("Shutdown signal received, stopping activator...");
                break;
            }
            _ = websocket_task(&async_client_factory, tx.clone(), shutdown.clone()) => {
                info!("Websocket task finished, stopping activator...");
            }
            snapshot_poll_res = get_snapshot_poll(client.clone(), tx.clone(), shutdown.clone()) => {
                if let Err(err) = snapshot_poll_res {
                    error!("Snapshot poll exited unexpectedly with reason: {err:?}");
                }
                else {
                    info!("Snapshot poll task finished, stopping activator...");
                }
            }
            _ = processor.run(shutdown.clone()) => {
                info!("Processor task finished, stopping activator...");
            }
        }

        shutdown.store(true, std::sync::atomic::Ordering::Relaxed);
    }

    info!("Activator handler finished");
    Ok(())
}

async fn run_activator_stateless<C, F, R, A>(
    client: Arc<C>,
    async_client_factory: F,
) -> eyre::Result<()>
where
    C: DoubleZeroClient + Send + Sync + 'static,
    F: Fn() -> R + Send + Sync + 'static,
    R: Future<Output = eyre::Result<A>> + Send + 'static,
    A: AsyncDoubleZeroClient + Send + Sync + 'static,
{
    loop {
        info!("Activator handler loop started stateless mode (onchain allocation)");

        let (tx, rx) = mpsc::channel(128);
        let mut processor = ProcessorStateless::new(rx, client.clone())?;

        let shutdown = Arc::new(AtomicBool::new(false));

        tokio::select! {
            biased;
            _ = crate::listen_for_shutdown()? => {
                info!("Shutdown signal received, stopping activator...");
                break;
            }
            _ = websocket_task(&async_client_factory, tx.clone(), shutdown.clone()) => {
                info!("Websocket task finished, stopping activator...");
            }
            snapshot_poll_res = get_snapshot_poll(client.clone(), tx.clone(), shutdown.clone()) => {
                if let Err(err) = snapshot_poll_res {
                    error!("Snapshot poll exited unexpectedly with reason: {err:?}");
                }
                else {
                    info!("Snapshot poll task finished, stopping activator...");
                }
            }
            _ = processor.run(shutdown.clone()) => {
                info!("Processor task finished, stopping activator...");
            }
        }

        shutdown.store(true, std::sync::atomic::Ordering::Relaxed);
    }

    info!("Activator handler finished");
    Ok(())
}

fn read_onchain_allocation_flag(client: &dyn DoubleZeroClient) -> eyre::Result<bool> {
    let (_, global_state) = GetGlobalStateCommand.execute(client)?;
    let enabled = is_feature_enabled(global_state.feature_flags, FeatureFlag::OnChainAllocation);
    info!(
        "Onchain allocation feature flag: {} (feature_flags={})",
        enabled, global_state.feature_flags
    );
    Ok(enabled)
}

fn create_client(
    rpc_url: Option<String>,
    websocket_url: Option<String>,
    program_id: Option<String>,
    keypair: Option<PathBuf>,
) -> eyre::Result<Arc<DZClient>> {
    let client = DZClient::new(rpc_url, websocket_url, program_id, keypair)?;

    info!(
        "Connected to RPC url: {} ws: {} program_id: {} ",
        client.get_rpc(),
        client.get_ws(),
        client.get_program_id()
    );

    Ok(Arc::new(client))
}

fn version_check(client: &DZClient) -> eyre::Result<()> {
    let stdout = std::io::stdout();
    let mut handle = stdout.lock();

    let cli = CliCommandImpl::new(client);
    check_version(&cli, &mut handle, ProgramVersion::current())?;
    Ok(())
}

pub async fn get_snapshot_poll<T: DoubleZeroClient>(
    client: Arc<T>,
    tx: mpsc::Sender<(Box<Pubkey>, Box<AccountData>)>,
    stop_signal: Arc<AtomicBool>,
) -> eyre::Result<()> {
    while !stop_signal.load(std::sync::atomic::Ordering::Relaxed) {
        for (pubkey, data) in client.get_all()? {
            tx.send((pubkey, data)).await?;
        }
        tokio::time::sleep(std::time::Duration::from_secs(60)).await;
    }
    Ok(())
}

pub async fn websocket_task<F, R, A>(
    client_factory: &F,
    tx: mpsc::Sender<(Box<Pubkey>, Box<AccountData>)>,
    stop_signal: Arc<AtomicBool>,
) where
    F: Fn() -> R,
    R: Future<Output = eyre::Result<A>>,
    A: AsyncDoubleZeroClient,
{
    while !stop_signal.load(std::sync::atomic::Ordering::Relaxed) {
        // this is a speedbump to allow a failed client to prevent a tight reconnect loop
        tokio::time::sleep(std::time::Duration::from_secs(1)).await;

        info!("Starting websocket task");
        match client_factory().await {
            Ok(async_client) => match async_client.subscribe().await {
                Ok((mut subscription, unsubscribe)) => {
                    info!("Websocket subscription established");
                    while !stop_signal.load(std::sync::atomic::Ordering::Relaxed) {
                        if let Some(msg) = subscription.next().await {
                            let keyed_account = msg.value;
                            let pubkey = keyed_account.pubkey.clone();
                            match rpckeyedaccount_decode(keyed_account) {
                                Ok(Some((pubkey, account))) => {
                                    tx.send((pubkey, account)).await.unwrap_or_else(|e| {
                                        error!("Failed to send account data: {e}",);
                                    });
                                }
                                Ok(None) => {
                                    info!("Received account with empty data for pubkey {}", pubkey);
                                }
                                Err(e) => {
                                    error!(
                                        "Error parsing RpcKeyedAccount for pubkey {}: {e}",
                                        pubkey
                                    );
                                }
                            }
                        } else {
                            break;
                        }
                    }
                    unsubscribe().await;
                    info!("Websocket subscription ended gracefully");
                }
                Err(e) => {
                    error!("Failed to establish websocket subscription: {e}");
                }
            },
            Err(e) => {
                error!("Failed to create AsyncDZClient: {e}");
            }
        }
    }
    info!("Websocket task finished successfully");
}

#[cfg(test)]
mod tests {
    use super::*;
    use base64::Engine;
    use doublezero_sdk::{
        doublezeroclient::{RpcKeyedAccountResponse, SubscribeResult},
        AccountType, GlobalConfig, MockDoubleZeroClient, ProgramConfig, ProgramVersion,
    };
    use solana_account_decoder::{UiAccount, UiAccountData};
    use solana_rpc_client_api::response::{RpcKeyedAccount, RpcResponseContext};
    use std::{
        collections::HashMap,
        sync::{atomic::AtomicBool, Arc, Once},
    };
    use tokio::sync::mpsc;

    static INIT: Once = Once::new();

    /// Initializes the logging system only once.
    /// Call this at the start of every test function.
    pub fn setup_logging() {
        INIT.call_once(|| {
            env_logger::builder()
                .is_test(true) // Ensure it respects cargo test --nocapture
                .filter_level(log::LevelFilter::Debug)
                .try_init()
                .unwrap_or_else(|e| eprintln!("Logger failed to initialize: {}", e));

            info!("Logger initialized for test suite.");
        });
    }

    #[derive(Clone)]
    struct MockSubscription {
        messages: Vec<RpcKeyedAccountResponse>,
        idx: usize,
        repeat: bool,
    }

    impl MockSubscription {
        fn new(messages: Vec<RpcKeyedAccountResponse>) -> Self {
            Self {
                messages,
                idx: 0,
                repeat: false,
            }
        }
    }

    impl futures::Stream for MockSubscription {
        type Item = RpcKeyedAccountResponse;
        fn poll_next(
            mut self: std::pin::Pin<&mut Self>,
            _cx: &mut std::task::Context<'_>,
        ) -> std::task::Poll<Option<Self::Item>> {
            if self.repeat && self.idx >= self.messages.len() {
                self.idx = 0;
            }
            if self.idx < self.messages.len() {
                let msg = self.messages[self.idx].clone();
                self.idx += 1;
                std::task::Poll::Ready(Some(msg))
            } else {
                std::task::Poll::Ready(None)
            }
        }
    }

    struct MockAsyncDoubleZeroClient {
        subscription: MockSubscription,
    }

    fn create_program_config(version: (u32, u32, u32)) -> ProgramConfig {
        ProgramConfig {
            account_type: doublezero_sdk::AccountType::ProgramConfig,
            bump_seed: 0,
            version: ProgramVersion {
                major: version.0,
                minor: version.1,
                patch: version.2,
            },
            min_compatible_version: ProgramVersion {
                major: 0,
                minor: 0,
                patch: 0,
            },
        }
    }

    fn create_rpc_keyed_account_response(data: &ProgramConfig) -> RpcKeyedAccountResponse {
        let serialized_data = borsh::to_vec(data).unwrap();
        let base64_data = base64::engine::general_purpose::STANDARD.encode(&serialized_data);
        RpcKeyedAccountResponse {
            context: RpcResponseContext {
                slot: 0,
                api_version: None,
            },
            value: RpcKeyedAccount {
                pubkey: Pubkey::default().to_string(),
                account: UiAccount {
                    lamports: 0,
                    data: UiAccountData::Binary(
                        base64_data,
                        solana_account_decoder::UiAccountEncoding::Base64,
                    ),
                    owner: Pubkey::default().to_string(),
                    executable: false,
                    rent_epoch: 0,
                    space: None,
                },
            },
        }
    }

    impl AsyncDoubleZeroClient for MockAsyncDoubleZeroClient {
        async fn subscribe<'s>(&'s self) -> SubscribeResult<'s> {
            let unsub: futures::future::BoxFuture<'static, ()> = Box::pin(async {});
            Ok((Box::pin(self.subscription.clone()), Box::new(|| unsub)))
        }
    }

    #[tokio::test]
    async fn test_websocket_task_sends_accounts() {
        setup_logging();
        let subscription = MockSubscription::new(vec![
            create_rpc_keyed_account_response(&create_program_config((1, 0, 0))),
            create_rpc_keyed_account_response(&create_program_config((2, 0, 0))),
            create_rpc_keyed_account_response(&create_program_config((3, 0, 0))),
        ]);
        let client_factory = || async {
            Ok(MockAsyncDoubleZeroClient {
                subscription: subscription.clone(),
            })
        };
        let (tx, mut rx) = mpsc::channel(1);
        let stop_signal = Arc::new(AtomicBool::new(false));
        tokio::spawn({
            let stop_signal = stop_signal.clone();
            async move {
                for i in 1..=3 {
                    let received = rx.recv().await;
                    assert!(received.is_some());
                    let (_, recv_account) = received.unwrap();
                    let recv_account: Box<AccountData> = recv_account;
                    let AccountData::ProgramConfig(config) = *recv_account else {
                        panic!("Expected ProgramConfig account data");
                    };
                    assert_eq!(config.version.major, i);
                }
                stop_signal.store(true, std::sync::atomic::Ordering::Relaxed);
            }
        });
        websocket_task(&client_factory, tx, stop_signal.clone()).await;
    }

    #[tokio::test]
    async fn test_websocket_task_early_termination() {
        setup_logging();
        let subscription = MockSubscription::new(vec![
            create_rpc_keyed_account_response(&create_program_config((1, 0, 0))),
            create_rpc_keyed_account_response(&create_program_config((2, 0, 0))),
            create_rpc_keyed_account_response(&create_program_config((3, 0, 0))),
            create_rpc_keyed_account_response(&create_program_config((4, 0, 0))),
            create_rpc_keyed_account_response(&create_program_config((5, 0, 0))),
        ]);
        let client_factory = || async {
            Ok(MockAsyncDoubleZeroClient {
                subscription: subscription.clone(),
            })
        };
        let (tx, mut rx) = mpsc::channel(1);
        let stop_signal = Arc::new(AtomicBool::new(false));
        tokio::spawn({
            let stop_signal = stop_signal.clone();
            async move {
                // read only 1 message before stop is sent
                let received = rx.recv().await;
                assert!(received.is_some());
                let (_, recv_account) = received.unwrap();
                let recv_account: Box<AccountData> = recv_account;
                let AccountData::ProgramConfig(config) = *recv_account else {
                    panic!("Expected ProgramConfig account data");
                };
                assert_eq!(config.version.major, 1);
                stop_signal.store(true, std::sync::atomic::Ordering::Relaxed);
            }
        });
        websocket_task(&client_factory, tx, stop_signal.clone()).await;
    }

    #[tokio::test]
    async fn test_run_activator_with_client_success() {
        // run activator and terminate with SIGTERM
        let mut mock_client = MockDoubleZeroClient::new();
        let client_factory = || async {
            Ok(MockAsyncDoubleZeroClient {
                subscription: MockSubscription::new(vec![]),
            })
        };
        let program_id = Pubkey::new_unique();
        mock_client
            .expect_get_program_id()
            .returning(move || program_id);
        let config = GlobalConfig {
            account_type: AccountType::GlobalConfig,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            local_asn: 123,
            remote_asn: 456,
            device_tunnel_block: "1.0.0.0/24".parse().unwrap(),
            user_tunnel_block: "1.0.1.0/24".parse().unwrap(),
            multicastgroup_block: "239.239.239.0/24".parse().unwrap(),
            multicast_publisher_block: "148.51.120.0/21".parse().unwrap(),
            next_bgp_community: 65535,
        };
        mock_client
            .expect_get()
            .returning(move |_| Ok(AccountData::GlobalConfig(config.clone())));
        mock_client
            .expect_gets()
            .returning(move |_| Ok(HashMap::new()));
        mock_client
            .expect_get_all()
            .returning(move || Ok(HashMap::new()));
        mock_client.expect_get_epoch().returning(|| Ok(0));
        let client = Arc::new(mock_client);
        tokio::spawn(async {
            tokio::time::sleep(std::time::Duration::from_secs(2)).await;
            unsafe {
                libc::raise(libc::SIGTERM);
            }
        });
        let result = run_activator_with_client(client, client_factory, false).await;
        assert!(result.is_ok());
    }
}
