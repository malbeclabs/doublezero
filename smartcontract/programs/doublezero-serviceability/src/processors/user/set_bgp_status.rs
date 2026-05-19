use crate::{
    error::DoubleZeroError,
    processors::validation::validate_program_account,
    serializer::try_acc_write,
    state::{
        device::Device,
        user::{BGPStatus, User},
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    clock::Clock,
    entrypoint::ProgramResult,
    pubkey::Pubkey,
    sysvar::Sysvar,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone)]
pub struct SetUserBGPStatusArgs {
    pub bgp_status: BGPStatus,
    /// Smoothed BGP TCP RTT in nanoseconds, sourced from the kernel via INET_DIAG.
    /// 0 means no sample. Old (1-byte) payloads deserialize with this defaulted to 0
    /// thanks to BorshDeserializeIncremental.
    pub bgp_rtt_ns: u64,
}

impl fmt::Debug for SetUserBGPStatusArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "bgp_status: {}, bgp_rtt_ns: {}",
            self.bgp_status, self.bgp_rtt_ns
        )
    }
}

pub fn process_set_bgp_status_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &SetUserBGPStatusArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let device_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    assert!(
        payer_account.is_signer,
        "Metrics publisher must be a signer"
    );

    validate_program_account!(user_account, program_id, writable = true, "User");
    validate_program_account!(device_account, program_id, writable = false, "Device");

    let device = Device::try_from(device_account)?;

    if device.metrics_publisher_pk != *payer_account.key {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut user = User::try_from(user_account)?;

    if user.device_pk != *device_account.key {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let slot = Clock::get()?.slot;
    user.bgp_status = value.bgp_status;
    user.last_bgp_reported_at = slot;
    user.bgp_rtt_ns = value.bgp_rtt_ns;
    if value.bgp_status == BGPStatus::Up {
        user.last_bgp_up_at = slot;
    }

    try_acc_write(&user, user_account, payer_account, accounts)?;

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_set_user_bgp_status_args_incremental_decode_old_payload() {
        // Old senders that predate `bgp_rtt_ns` serialize only the 1-byte status enum.
        // `BorshDeserializeIncremental` must accept the short payload and default
        // `bgp_rtt_ns` to 0 so we never reject historical telemetry submitters.
        let old_payload = [BGPStatus::Up as u8];
        let args = SetUserBGPStatusArgs::try_from(&old_payload[..]).unwrap();
        assert_eq!(args.bgp_status, BGPStatus::Up);
        assert_eq!(args.bgp_rtt_ns, 0);
    }

    #[test]
    fn test_set_user_bgp_status_args_new_payload_roundtrip() {
        let args = SetUserBGPStatusArgs {
            bgp_status: BGPStatus::Up,
            bgp_rtt_ns: 12_345_678,
        };
        let payload = borsh::to_vec(&args).unwrap();
        assert_eq!(payload.len(), 9, "1 byte status + 8 bytes u64 = 9 bytes");
        let decoded = SetUserBGPStatusArgs::try_from(&payload[..]).unwrap();
        assert_eq!(decoded, args);
    }
}
