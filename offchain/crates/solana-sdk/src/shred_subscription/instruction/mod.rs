pub mod account;

use std::io;

use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_tools::{DISCRIMINATOR_LEN, Discriminator};

/// Identifier for a single claim holding account, used as a payload element
/// in `ClaimValidatorClientRewards`. Mirrors the on-chain struct byte for
/// byte: `subscription_epoch: u64` followed by `bump_seed: u8`.
#[derive(Clone, Copy, Debug, PartialEq, Eq, BorshSerialize, BorshDeserialize)]
pub struct ClaimHoldingId {
    pub subscription_epoch: u64,
    pub bump_seed: u8,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ShredSubscriptionInstructionData {
    /// Initialize a client seat for a (device, client_ip) pair.
    InitializeClientSeat { client_ip: u32 },
    /// Initialize a payment escrow for a (seat, withdraw_authority) pair.
    InitializePaymentEscrow,
    /// Close a payment escrow and refund any remaining USDC.
    ClosePaymentEscrow,
    /// Fund a payment escrow with USDC.
    FundPaymentEscrowUsdc(u64),
    /// Request instant allocation for a funded seat (skips auction settlement).
    RequestInstantSeatAllocation,
    /// Request instant seat withdrawal.
    RequestInstantSeatWithdrawal,
    /// Request instant seat withdrawal with a prorated USDC refund based on
    /// the remaining slots in the epoch. Superset of
    /// `RequestInstantSeatWithdrawal` (more accounts).
    RequestProratedInstantSeatWithdrawal,
    /// Set the rewards proportion for a validator client.
    SetValidatorClientRewardsProportion(u16),
    /// Permissionless. Initialize a non-ATA claim holding token account
    /// owned by the `ValidatorClientRewards` parent PDA for
    /// `(subscription_epoch, mint)`. Payload is the subscription epoch.
    InitializeClaimHolding(u64),
    /// `ValidatorClientRewards.manager_key`-signed. Drain N claim holdings
    /// into a destination token account and close each, recovering rent to
    /// `program_config.shred_oracle_key`.
    ClaimValidatorClientRewards(Vec<ClaimHoldingId>),
    /// Validates the provided CLI version against the onchain minimum.
    CheckCliVersion { major: u32, minor: u32, patch: u32 },
}

impl ShredSubscriptionInstructionData {
    pub const INITIALIZE_CLIENT_SEAT: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::initialize_client_seat");
    pub const INITIALIZE_PAYMENT_ESCROW: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::initialize_payment_escrow");
    pub const CLOSE_PAYMENT_ESCROW: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::close_payment_escrow");
    pub const FUND_PAYMENT_ESCROW_USDC: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::fund_payment_escrow_usdc");
    pub const REQUEST_INSTANT_SEAT_ALLOCATION: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::request_instant_seat_allocation");
    pub const REQUEST_INSTANT_SEAT_WITHDRAWAL: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::request_instant_seat_withdrawal");
    pub const REQUEST_PRORATED_INSTANT_SEAT_WITHDRAWAL: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::request_prorated_instant_seat_withdrawal");
    pub const SET_VALIDATOR_CLIENT_REWARDS_PROPORTION: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::set_validator_client_rewards_proportion");
    pub const INITIALIZE_CLAIM_HOLDING: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::initialize_claim_holding");
    pub const CLAIM_VALIDATOR_CLIENT_REWARDS: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::claim_validator_client_rewards");
    pub const CHECK_CLI_VERSION: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::check_cli_version");
}

impl BorshSerialize for ShredSubscriptionInstructionData {
    fn serialize<W: io::Write>(&self, writer: &mut W) -> io::Result<()> {
        match self {
            Self::InitializeClientSeat { client_ip } => {
                Self::INITIALIZE_CLIENT_SEAT.serialize(writer)?;
                client_ip.serialize(writer)
            }
            Self::InitializePaymentEscrow => Self::INITIALIZE_PAYMENT_ESCROW.serialize(writer),
            Self::ClosePaymentEscrow => Self::CLOSE_PAYMENT_ESCROW.serialize(writer),
            Self::FundPaymentEscrowUsdc(amount) => {
                Self::FUND_PAYMENT_ESCROW_USDC.serialize(writer)?;
                amount.serialize(writer)
            }
            Self::RequestInstantSeatAllocation => {
                Self::REQUEST_INSTANT_SEAT_ALLOCATION.serialize(writer)
            }
            Self::RequestInstantSeatWithdrawal => {
                Self::REQUEST_INSTANT_SEAT_WITHDRAWAL.serialize(writer)
            }
            Self::RequestProratedInstantSeatWithdrawal => {
                Self::REQUEST_PRORATED_INSTANT_SEAT_WITHDRAWAL.serialize(writer)
            }
            Self::SetValidatorClientRewardsProportion(proportion) => {
                Self::SET_VALIDATOR_CLIENT_REWARDS_PROPORTION.serialize(writer)?;
                proportion.serialize(writer)
            }
            Self::InitializeClaimHolding(subscription_epoch) => {
                Self::INITIALIZE_CLAIM_HOLDING.serialize(writer)?;
                subscription_epoch.serialize(writer)
            }
            Self::ClaimValidatorClientRewards(holdings) => {
                Self::CLAIM_VALIDATOR_CLIENT_REWARDS.serialize(writer)?;
                holdings.serialize(writer)
            }
            Self::CheckCliVersion {
                major,
                minor,
                patch,
            } => {
                Self::CHECK_CLI_VERSION.serialize(writer)?;
                major.serialize(writer)?;
                minor.serialize(writer)?;
                patch.serialize(writer)
            }
        }
    }
}

impl BorshDeserialize for ShredSubscriptionInstructionData {
    fn deserialize_reader<R: io::Read>(reader: &mut R) -> io::Result<Self> {
        match Discriminator::deserialize_reader(reader)? {
            Self::INITIALIZE_CLIENT_SEAT => {
                let client_ip = u32::deserialize_reader(reader)?;
                Ok(Self::InitializeClientSeat { client_ip })
            }
            Self::INITIALIZE_PAYMENT_ESCROW => Ok(Self::InitializePaymentEscrow),
            Self::CLOSE_PAYMENT_ESCROW => Ok(Self::ClosePaymentEscrow),
            Self::FUND_PAYMENT_ESCROW_USDC => {
                let amount = u64::deserialize_reader(reader)?;
                Ok(Self::FundPaymentEscrowUsdc(amount))
            }
            Self::REQUEST_INSTANT_SEAT_ALLOCATION => Ok(Self::RequestInstantSeatAllocation),
            Self::REQUEST_INSTANT_SEAT_WITHDRAWAL => Ok(Self::RequestInstantSeatWithdrawal),
            Self::REQUEST_PRORATED_INSTANT_SEAT_WITHDRAWAL => {
                Ok(Self::RequestProratedInstantSeatWithdrawal)
            }
            Self::SET_VALIDATOR_CLIENT_REWARDS_PROPORTION => {
                let proportion = u16::deserialize_reader(reader)?;
                Ok(Self::SetValidatorClientRewardsProportion(proportion))
            }
            Self::INITIALIZE_CLAIM_HOLDING => {
                let subscription_epoch = u64::deserialize_reader(reader)?;
                Ok(Self::InitializeClaimHolding(subscription_epoch))
            }
            Self::CLAIM_VALIDATOR_CLIENT_REWARDS => {
                let holdings = Vec::<ClaimHoldingId>::deserialize_reader(reader)?;
                Ok(Self::ClaimValidatorClientRewards(holdings))
            }
            Self::CHECK_CLI_VERSION => {
                let major = u32::deserialize_reader(reader)?;
                let minor = u32::deserialize_reader(reader)?;
                let patch = u32::deserialize_reader(reader)?;
                Ok(Self::CheckCliVersion {
                    major,
                    minor,
                    patch,
                })
            }
            _ => Err(io::Error::new(
                io::ErrorKind::InvalidData,
                "Invalid discriminator",
            )),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn claim_holding_id_round_trip() {
        let id = ClaimHoldingId {
            subscription_epoch: 0x1122_3344_5566_7788,
            bump_seed: 0xAB,
        };
        let bytes = borsh::to_vec(&id).unwrap();
        assert_eq!(bytes.len(), 9);
        let decoded: ClaimHoldingId = borsh::from_slice(&bytes).unwrap();
        assert_eq!(decoded, id);
    }

    #[test]
    fn claim_holding_id_frozen_bytes() {
        let id = ClaimHoldingId {
            subscription_epoch: 0x0807_0605_0403_0201,
            bump_seed: 0xFF,
        };
        let bytes = borsh::to_vec(&id).unwrap();
        assert_eq!(
            bytes,
            vec![0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0xFF]
        );
    }

    #[test]
    fn round_trip_initialize_claim_holding() {
        let ix = ShredSubscriptionInstructionData::InitializeClaimHolding(0xDEAD_BEEF_CAFE_BABE);
        let bytes = borsh::to_vec(&ix).unwrap();
        let decoded = ShredSubscriptionInstructionData::try_from_slice(&bytes).unwrap();
        assert_eq!(decoded, ix);
    }

    #[test]
    fn round_trip_claim_validator_client_rewards_empty() {
        let ix = ShredSubscriptionInstructionData::ClaimValidatorClientRewards(vec![]);
        let bytes = borsh::to_vec(&ix).unwrap();
        let decoded = ShredSubscriptionInstructionData::try_from_slice(&bytes).unwrap();
        assert_eq!(decoded, ix);
    }

    #[test]
    fn round_trip_claim_validator_client_rewards_multiple() {
        let ix = ShredSubscriptionInstructionData::ClaimValidatorClientRewards(vec![
            ClaimHoldingId {
                subscription_epoch: 100,
                bump_seed: 254,
            },
            ClaimHoldingId {
                subscription_epoch: 101,
                bump_seed: 253,
            },
            ClaimHoldingId {
                subscription_epoch: 102,
                bump_seed: 252,
            },
        ]);
        let bytes = borsh::to_vec(&ix).unwrap();
        let decoded = ShredSubscriptionInstructionData::try_from_slice(&bytes).unwrap();
        assert_eq!(decoded, ix);
    }

    #[test]
    fn frozen_bytes_initialize_claim_holding() {
        let ix = ShredSubscriptionInstructionData::InitializeClaimHolding(0x01);
        let mut expected =
            borsh::to_vec(&ShredSubscriptionInstructionData::INITIALIZE_CLAIM_HOLDING)
                .expect("discriminator serialization");
        expected.extend_from_slice(&1u64.to_le_bytes());
        assert_eq!(borsh::to_vec(&ix).unwrap(), expected);
    }

    #[test]
    fn frozen_bytes_claim_validator_client_rewards_one_entry() {
        let ix =
            ShredSubscriptionInstructionData::ClaimValidatorClientRewards(vec![ClaimHoldingId {
                subscription_epoch: 7,
                bump_seed: 250,
            }]);
        let mut expected =
            borsh::to_vec(&ShredSubscriptionInstructionData::CLAIM_VALIDATOR_CLIENT_REWARDS)
                .expect("discriminator serialization");
        // Borsh vec length prefix is u32 LE.
        expected.extend_from_slice(&1u32.to_le_bytes());
        expected.extend_from_slice(&7u64.to_le_bytes());
        expected.push(250);
        assert_eq!(borsh::to_vec(&ix).unwrap(), expected);
    }
}
