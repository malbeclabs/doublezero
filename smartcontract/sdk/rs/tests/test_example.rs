use anyhow::Result;
use solana_client::rpc_client::RpcClient;
use solana_sdk::signature::{Keypair, Signer};
use testcontainers::{
    core::{IntoContainerPort, WaitFor},
    runners::AsyncRunner,
    GenericImage,
};
use tokio::{
    net::TcpStream,
    time::{sleep, Duration, Instant},
};

#[tokio::test(flavor = "multi_thread")]
async fn test_integration_solana_ledger() -> Result<()> {
    let container = GenericImage::new("snormore/solana-test-validator", "latest")
        .with_exposed_port(8899.tcp())
        .with_wait_for(WaitFor::message_on_stdout("JSON RPC URL"))
        .start()
        .await?;

    let rpc_port = container.get_host_port_ipv4(8899).await?;
    let rpc_url = format!("http://127.0.0.1:{rpc_port}");
    let client = RpcClient::new(rpc_url);

    let ok = poll_until(
        || async { TcpStream::connect(("127.0.0.1", rpc_port)).await.is_ok() },
        Duration::from_secs(30),
        Duration::from_millis(200),
    )
    .await;
    assert!(ok, "RPC TCP port {rpc_port} did not open in time");

    let ok = poll_until(
        || async { client.get_epoch_info().is_ok() },
        Duration::from_secs(30),
        Duration::from_millis(200),
    )
    .await;
    assert!(ok, "RPC did not respond to get_epoch_info() in time");

    let epoch = client.get_epoch_info()?.epoch;
    assert_eq!(epoch, 0);

    let kp = Keypair::new();
    let lamports = 1_000_000_000u64;
    let _sig = client.request_airdrop(&kp.pubkey(), lamports)?;

    let ok = poll_until(
        || async {
            client
                .get_balance(&kp.pubkey())
                .map(|b| b >= lamports)
                .unwrap_or(false)
        },
        Duration::from_secs(30),
        Duration::from_millis(200),
    )
    .await;
    assert!(ok, "expected airdropped balance >= {lamports} lamports");

    Ok(())
}

async fn poll_until<F, Fut>(mut f: F, timeout: Duration, interval: Duration) -> bool
where
    F: FnMut() -> Fut,
    Fut: std::future::Future<Output = bool>,
{
    let deadline = Instant::now() + timeout;
    loop {
        if f().await {
            return true;
        }
        if Instant::now() >= deadline {
            return false;
        }
        sleep(interval).await;
    }
}
