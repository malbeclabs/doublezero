//! Guard that prevents deleting a serviceability device while it is still
//! enabled in the shred-subscription program.

use borsh::BorshDeserialize;
use doublezero_config::{Environment, ShredSubscriptionConfig};
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_sdk::{account::Account, hash::hash, pubkey::Pubkey};
use std::{future::Future, sync::LazyLock, time::Duration};

/// PDA seed for DeviceHistory accounts (see `sdk/shreds/go/pda.go`).
const DEVICE_HISTORY_SEED: &[u8] = b"device_history";

/// Length of a shred-subscription account discriminator.
const DISCRIMINATOR_LEN: usize = 8;

/// `enabled` is bit 1 of `DeviceHistory::flags` (see `DeviceHistory::IsEnabled`
/// in `sdk/shreds/go/state.go`).
const ENABLED_FLAG: u64 = 1 << 1;

/// Bounds each Solana RPC call so a hung endpoint can't hang the delete.
const SHRED_RPC_TIMEOUT: Duration = Duration::from_secs(15);

/// Number of attempts for the DeviceHistory lookup before failing closed. The
/// default RPC is the public, rate-limited Solana endpoint, so a transient 429
/// or timeout should retry rather than abort a legitimate delete.
const SHRED_RPC_ATTEMPTS: usize = 3;

/// Delay between DeviceHistory RPC retry attempts.
const SHRED_RPC_RETRY_DELAY: Duration = Duration::from_millis(500);

/// Account discriminator: first 8 bytes of `sha256("dz::account::device_history")`
/// (Solana's `hash` is SHA-256), matching `sdk/shreds/go/discriminator.go`.
static DEVICE_HISTORY_DISCRIMINATOR: LazyLock<[u8; DISCRIMINATOR_LEN]> = LazyLock::new(|| {
    hash(b"dz::account::device_history").to_bytes()[..DISCRIMINATOR_LEN]
        .try_into()
        .expect("sha256 digest is 32 bytes")
});

/// Leading fields of the shred-subscription `DeviceHistory` account, up to the
/// `flags` we read. Mirrors `DeviceHistory` in `sdk/shreds/go/state.go`.
///
/// Do NOT add fields past `flags`: the onchain account is a fixed C-style layout
/// (read via `binary.Read` in the Go SDK) with padding after `flags`. Borsh
/// coincides with that layout only for these leading, unpadded fields; anything
/// added after would decode at the wrong offset. `deserialize` (not
/// `try_from_slice`) is used so the trailing account bytes are ignored.
#[derive(BorshDeserialize)]
struct DeviceHistoryHeader {
    #[allow(dead_code)]
    device_key: [u8; 32],
    flags: u64,
}

/// Errors out if `device_pubkey` still has an enabled DeviceHistory account in
/// the shred-subscription program.
///
/// Fail-closed: any RPC failure aborts (returns an error) rather than allowing
/// a potentially unsafe delete. Environments without a deployed
/// shred-subscription program (devnet, local) are skipped.
pub async fn ensure_device_not_enabled_in_shred_subscription(
    env: Environment,
    device_pubkey: &Pubkey,
) -> eyre::Result<()> {
    let Some(cfg) = env.shred_subscription_config() else {
        // Shred-subscription is not deployed for this environment.
        return Ok(());
    };

    let rpc = RpcClient::new_with_timeout(cfg.solana_rpc_url.clone(), SHRED_RPC_TIMEOUT);
    let rpc_url = cfg.solana_rpc_url.clone();

    check_device_history(&cfg, device_pubkey, move |history_pda| async move {
        fetch_with_retry(SHRED_RPC_ATTEMPTS, SHRED_RPC_RETRY_DELAY, || async {
            rpc.get_account_with_commitment(&history_pda, rpc.commitment())
                .await
                .map(|response| response.value)
        })
        .await
        .map_err(|e| {
            eyre::eyre!(
                "failed to query shred-subscription DeviceHistory {history_pda} on {rpc_url} \
                 after {SHRED_RPC_ATTEMPTS} attempts: {e}"
            )
        })
    })
    .await
}

/// Runs `fetch` up to `attempts` times, sleeping `delay` between failures, and
/// returns the first success or the last error. Bounds transient RPC failures
/// (e.g. a 429 from the public Solana endpoint) without silently allowing the
/// delete.
async fn fetch_with_retry<T, E, F, Fut>(attempts: usize, delay: Duration, fetch: F) -> Result<T, E>
where
    F: Fn() -> Fut,
    Fut: Future<Output = Result<T, E>>,
{
    let mut attempt = 0;
    loop {
        attempt += 1;
        match fetch().await {
            Ok(value) => return Ok(value),
            Err(e) if attempt >= attempts => return Err(e),
            Err(_) => std::thread::sleep(delay),
        }
    }
}

/// Derives the DeviceHistory PDA, fetches it via `fetch`, and decides whether the
/// delete may proceed. Split from the RPC client so the fetch can be mocked.
async fn check_device_history<F, Fut>(
    cfg: &ShredSubscriptionConfig,
    device_pubkey: &Pubkey,
    fetch: F,
) -> eyre::Result<()>
where
    F: FnOnce(Pubkey) -> Fut,
    Fut: Future<Output = eyre::Result<Option<Account>>>,
{
    let (history_pda, _bump) = Pubkey::find_program_address(
        &[DEVICE_HISTORY_SEED, device_pubkey.as_ref()],
        &cfg.program_id,
    );

    let account = fetch(history_pda).await?;

    evaluate_device_history(
        account.as_ref(),
        &cfg.program_id,
        device_pubkey,
        &history_pda,
        &cfg.solana_rpc_url,
    )
}

/// Decides whether a delete may proceed given the fetched DeviceHistory account.
///
/// Fails only when the account exists, is owned by the shred-subscription
/// program, and is enabled. An absent account, or one owned by another program,
/// is treated as "not enabled" (safe to delete).
fn evaluate_device_history(
    account: Option<&Account>,
    program_id: &Pubkey,
    device_pubkey: &Pubkey,
    history_pda: &Pubkey,
    solana_rpc_url: &str,
) -> eyre::Result<()> {
    let Some(account) = account else {
        // No DeviceHistory account: the device was never enrolled in
        // shred-subscription, so it is safe to delete.
        return Ok(());
    };

    // Only trust accounts actually owned by the shred-subscription program.
    if account.owner != *program_id {
        return Ok(());
    }

    if device_history_enabled(&account.data)? {
        eyre::bail!(
            "Device {device_pubkey} is still enabled in the shred-subscription program \
             (DeviceHistory {history_pda} on {solana_rpc_url}). Disable it there before deleting \
             the device."
        );
    }

    Ok(())
}

/// Parses a raw DeviceHistory account and returns whether it is enabled.
///
/// Validates the discriminator and length so an unexpected account layout is
/// reported as an error rather than silently misread.
fn device_history_enabled(data: &[u8]) -> eyre::Result<bool> {
    if data.len() < DISCRIMINATOR_LEN {
        eyre::bail!(
            "shred-subscription DeviceHistory account is too small: {} bytes",
            data.len()
        );
    }
    if data[..DISCRIMINATOR_LEN] != *DEVICE_HISTORY_DISCRIMINATOR {
        eyre::bail!("shred-subscription DeviceHistory account has an unexpected discriminator");
    }
    let header = DeviceHistoryHeader::deserialize(&mut &data[DISCRIMINATOR_LEN..])?;
    Ok(header.flags & ENABLED_FLAG != 0)
}

#[cfg(test)]
mod tests {
    use super::*;
    use doublezero_cli_core::testing::block_on;

    fn make_account(flags: u64, discriminator: [u8; DISCRIMINATOR_LEN]) -> Vec<u8> {
        let mut data = Vec::new();
        data.extend_from_slice(&discriminator);
        data.extend_from_slice(&[0u8; 32]); // device_key
        data.extend_from_slice(&flags.to_le_bytes());
        data.extend_from_slice(&[0u8; 16]); // trailing bytes are tolerated
        data
    }

    #[test]
    fn test_enabled_flag_set() {
        let data = make_account(ENABLED_FLAG, *DEVICE_HISTORY_DISCRIMINATOR);
        assert!(device_history_enabled(&data).unwrap());
    }

    #[test]
    fn test_enabled_flag_clear() {
        let data = make_account(0, *DEVICE_HISTORY_DISCRIMINATOR);
        assert!(!device_history_enabled(&data).unwrap());
        // Other flag bits set, but not the enabled bit.
        let data = make_account((1 << 0) | (1 << 2), *DEVICE_HISTORY_DISCRIMINATOR);
        assert!(!device_history_enabled(&data).unwrap());
    }

    #[test]
    fn test_wrong_discriminator_errors() {
        let data = make_account(ENABLED_FLAG, [0; DISCRIMINATOR_LEN]);
        assert!(device_history_enabled(&data).is_err());
    }

    #[test]
    fn test_too_short_errors() {
        assert!(device_history_enabled(&[0u8; 4]).is_err());
    }

    #[test]
    fn test_truncated_body_errors() {
        // Correct discriminator but not enough bytes for device_key + flags.
        let mut data = DEVICE_HISTORY_DISCRIMINATOR.to_vec();
        data.extend_from_slice(&[0u8; 10]);
        assert!(device_history_enabled(&data).is_err());
    }

    fn account_with(flags: u64, owner: Pubkey) -> Account {
        Account {
            data: make_account(flags, *DEVICE_HISTORY_DISCRIMINATOR),
            owner,
            ..Default::default()
        }
    }

    fn test_cfg(program_id: Pubkey) -> ShredSubscriptionConfig {
        ShredSubscriptionConfig {
            program_id,
            solana_rpc_url: "http://mock".to_string(),
        }
    }

    // The following tests drive the full check_device_history wiring (derive PDA
    // → fetch → decide) with a mocked fetcher, exercising the bail/allow paths
    // without a real RPC.

    #[test]
    fn test_enabled_account_blocks_delete() {
        let cfg = test_cfg(Pubkey::new_unique());
        let account = account_with(ENABLED_FLAG, cfg.program_id);
        let res = block_on(check_device_history(
            &cfg,
            &Pubkey::new_unique(),
            move |_pda| async move { Ok(Some(account)) },
        ));
        assert!(res.is_err());
    }

    #[test]
    fn test_disabled_account_allows_delete() {
        let cfg = test_cfg(Pubkey::new_unique());
        let account = account_with(0, cfg.program_id);
        let res = block_on(check_device_history(
            &cfg,
            &Pubkey::new_unique(),
            move |_pda| async move { Ok(Some(account)) },
        ));
        assert!(res.is_ok());
    }

    #[test]
    fn test_absent_account_allows_delete() {
        let cfg = test_cfg(Pubkey::new_unique());
        let res = block_on(check_device_history(
            &cfg,
            &Pubkey::new_unique(),
            |_pda| async move { Ok(None) },
        ));
        assert!(res.is_ok());
    }

    #[test]
    fn test_wrong_owner_allows_delete() {
        // Enabled data, but owned by a different program → not a real
        // DeviceHistory, so the delete is allowed.
        let cfg = test_cfg(Pubkey::new_unique());
        let account = account_with(ENABLED_FLAG, Pubkey::new_unique());
        let res = block_on(check_device_history(
            &cfg,
            &Pubkey::new_unique(),
            move |_pda| async move { Ok(Some(account)) },
        ));
        assert!(res.is_ok());
    }

    #[test]
    fn test_fetch_error_fails_closed() {
        // An RPC failure must abort the delete (fail-closed), not allow it.
        let cfg = test_cfg(Pubkey::new_unique());
        let res = block_on(check_device_history(
            &cfg,
            &Pubkey::new_unique(),
            |_pda| async move { Err(eyre::eyre!("rpc unavailable")) },
        ));
        assert!(res.is_err());
    }

    #[test]
    fn test_fetch_with_retry_recovers_after_transient_errors() {
        let calls = std::cell::Cell::new(0u32);
        let res: Result<u8, &str> = block_on(fetch_with_retry(3, Duration::ZERO, || {
            let n = calls.get();
            calls.set(n + 1);
            async move {
                if n < 2 {
                    Err("transient")
                } else {
                    Ok(7)
                }
            }
        }));
        assert_eq!(res.unwrap(), 7);
        assert_eq!(calls.get(), 3);
    }

    #[test]
    fn test_fetch_with_retry_gives_up_after_attempts() {
        let calls = std::cell::Cell::new(0u32);
        let res: Result<u8, &str> = block_on(fetch_with_retry(3, Duration::ZERO, || {
            calls.set(calls.get() + 1);
            async move { Err("always") }
        }));
        assert!(res.is_err());
        assert_eq!(calls.get(), 3);
    }

    #[test]
    fn test_device_history_pda_matches_known_incident() {
        // The DeviceHistory PDA is derived from the serviceability device pubkey.
        // Confirm that assumption against the real orphaned device and its
        // DeviceHistory PDA observed in the incident that motivated this guard.
        let device = Pubkey::from_str_const("4gK6BJ14TBSdSaAFLJyWaiZFkuxztvWzBz2suUSTKMYo");
        let program = Pubkey::from_str_const("dzshrr3yL57SB13sJPYHYo3TV8Bo1i1FxkyrZr3bKNE");
        let (pda, _bump) =
            Pubkey::find_program_address(&[DEVICE_HISTORY_SEED, device.as_ref()], &program);
        assert_eq!(
            pda,
            Pubkey::from_str_const("8kBDWyUxWSEGXGu4HTxJysj7r9PKDG6te2Na6XrCvSe8")
        );
    }

    #[test]
    fn test_skips_when_shred_not_deployed() {
        // Devnet and local have no shred-subscription program, so the guard is
        // a no-op and makes no network call.
        let device = Pubkey::new_unique();
        assert!(block_on(ensure_device_not_enabled_in_shred_subscription(
            Environment::Devnet,
            &device
        ))
        .is_ok());
        assert!(block_on(ensure_device_not_enabled_in_shred_subscription(
            Environment::Local,
            &device
        ))
        .is_ok());
    }
}
