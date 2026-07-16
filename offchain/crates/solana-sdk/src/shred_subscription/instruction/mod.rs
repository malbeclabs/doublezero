pub mod account;

use std::io;

use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_tools::{DISCRIMINATOR_LEN, Discriminator};
use solana_sdk::pubkey::Pubkey;
use svm_hash::merkle::MerkleProof;

/// Envelope for an offchain authorization produced by a validator operator
/// via `solana sign-offchain-message`. Carries the ed25519 signature plus the
/// cluster slot after which the authorization is no longer valid. Mirrors the
/// on-chain `ValidatorOffchainAuthorization` Borsh layout.
#[derive(Debug, Clone, PartialEq, Eq, BorshSerialize, BorshDeserialize)]
pub struct ValidatorOffchainAuthorization {
    pub deadline_slot: u64,
    pub signature: [u8; 64],
}

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
    /// Initialize a payment escrow for a (seat, withdraw_authority) pair. The
    /// argument is the operator key (Pubkey::default() = no separate operator).
    InitializePaymentEscrow(Pubkey),
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
    /// Anyone can initialize validator publisher rewards for a given node.
    /// The `node_id` must not be `Pubkey::default()`.
    InitializeValidatorPublisherRewards(Pubkey),
    /// Set the reward token owner and mint on a previously initialized
    /// validator publisher rewards account. Two auth paths:
    /// - `offchain_authorization = Some(_)`: ed25519 signature produced by the
    ///   `node_id` keypair via `solana sign-offchain-message` over
    ///   `ConfigureValidatorPublisherRewardsAuthMessage::to_hex_encoded()`.
    /// - `offchain_authorization = None`: the `validator_node` account must be
    ///   a Solana signer on the transaction.
    ConfigureValidatorPublisherRewards {
        rewards_token_owner_key: Pubkey,
        offchain_authorization: Option<ValidatorOffchainAuthorization>,
    },
    /// Permissionless. Distribute a single validator's accumulated rewards
    /// for one (subscription_epoch, journal) pair: transfers the publisher
    /// share to the validator's destination ATA and the client share into
    /// the per-epoch claim-holding account. Authenticates the leaf via the
    /// merkle proof against the journal's root.
    DistributeValidatorRewards {
        leader_slots: u32,
        proof: MerkleProof,
    },
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
    pub const INITIALIZE_VALIDATOR_PUBLISHER_REWARDS: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::initialize_validator_publisher_rewards");
    pub const CONFIGURE_VALIDATOR_PUBLISHER_REWARDS: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::configure_validator_publisher_rewards");
    pub const DISTRIBUTE_VALIDATOR_REWARDS: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::distribute_validator_rewards");
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
            Self::InitializePaymentEscrow(operator_key) => {
                Self::INITIALIZE_PAYMENT_ESCROW.serialize(writer)?;
                operator_key.serialize(writer)
            }
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
            Self::InitializeValidatorPublisherRewards(node_id) => {
                Self::INITIALIZE_VALIDATOR_PUBLISHER_REWARDS.serialize(writer)?;
                node_id.serialize(writer)
            }
            Self::ConfigureValidatorPublisherRewards {
                rewards_token_owner_key,
                offchain_authorization,
            } => {
                Self::CONFIGURE_VALIDATOR_PUBLISHER_REWARDS.serialize(writer)?;
                rewards_token_owner_key.serialize(writer)?;
                offchain_authorization.serialize(writer)
            }
            Self::DistributeValidatorRewards {
                leader_slots,
                proof,
            } => {
                Self::DISTRIBUTE_VALIDATOR_REWARDS.serialize(writer)?;
                leader_slots.serialize(writer)?;
                proof.serialize(writer)
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
            Self::INITIALIZE_PAYMENT_ESCROW => {
                // Optional trailing argument: absent (legacy tx) -> default key;
                // exactly 32 bytes -> that key. Any other length is malformed.
                // We read to end and check the length explicitly rather than
                // relying on Pubkey::deserialize_reader + borsh's leftover-bytes
                // guard: a slice reader's read_exact consumes the remaining
                // bytes before erroring, so 1..=31 trailing bytes would
                // otherwise decode to the default key — bytes the onchain
                // program (strict on length) rejects.
                let mut rest = Vec::new();
                reader.read_to_end(&mut rest)?;
                let operator_key = match rest.len() {
                    0 => Pubkey::default(),
                    32 => Pubkey::try_from(rest.as_slice()).map_err(|_| {
                        io::Error::new(io::ErrorKind::InvalidData, "invalid operator key")
                    })?,
                    _ => {
                        return Err(io::Error::new(
                            io::ErrorKind::InvalidData,
                            "InitializePaymentEscrow operator key must be absent or 32 bytes",
                        ));
                    }
                };
                Ok(Self::InitializePaymentEscrow(operator_key))
            }
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
            Self::INITIALIZE_VALIDATOR_PUBLISHER_REWARDS => {
                let node_id = Pubkey::deserialize_reader(reader)?;
                Ok(Self::InitializeValidatorPublisherRewards(node_id))
            }
            Self::CONFIGURE_VALIDATOR_PUBLISHER_REWARDS => {
                let rewards_token_owner_key = Pubkey::deserialize_reader(reader)?;
                let offchain_authorization =
                    Option::<ValidatorOffchainAuthorization>::deserialize_reader(reader)?;
                Ok(Self::ConfigureValidatorPublisherRewards {
                    rewards_token_owner_key,
                    offchain_authorization,
                })
            }
            Self::DISTRIBUTE_VALIDATOR_REWARDS => {
                let leader_slots = u32::deserialize_reader(reader)?;
                let proof = MerkleProof::deserialize_reader(reader)?;
                Ok(Self::DistributeValidatorRewards {
                    leader_slots,
                    proof,
                })
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

    fn round_trip(ix: &ShredSubscriptionInstructionData) {
        let bytes = borsh::to_vec(ix).unwrap();
        let parsed = ShredSubscriptionInstructionData::try_from_slice(&bytes).unwrap();
        assert_eq!(*ix, parsed);
    }

    #[test]
    fn test_round_trip_initialize_payment_escrow() {
        round_trip(&ShredSubscriptionInstructionData::InitializePaymentEscrow(
            Pubkey::new_unique(),
        ));
    }

    #[test]
    fn test_frozen_bytes_initialize_payment_escrow() {
        let key = Pubkey::new_unique();
        let ix = ShredSubscriptionInstructionData::InitializePaymentEscrow(key);
        let mut expected =
            borsh::to_vec(&ShredSubscriptionInstructionData::INITIALIZE_PAYMENT_ESCROW)
                .expect("discriminator serialization");
        expected.extend_from_slice(&key.to_bytes());
        assert_eq!(borsh::to_vec(&ix).unwrap(), expected);
    }

    // An old client that sent only the discriminator (the pre-operator-key
    // wire form) must still decode — payments.rs decodes historical
    // transactions. The absent key decodes to the default.
    #[test]
    fn test_initialize_payment_escrow_decodes_legacy_wire_form() {
        let bytes = borsh::to_vec(&ShredSubscriptionInstructionData::INITIALIZE_PAYMENT_ESCROW)
            .expect("discriminator serialization");
        let parsed = ShredSubscriptionInstructionData::try_from_slice(&bytes).unwrap();
        assert_eq!(
            parsed,
            ShredSubscriptionInstructionData::InitializePaymentEscrow(Pubkey::default()),
        );
    }

    // A truncated operator key (1..=31 trailing bytes) is malformed and must
    // error, matching the strict onchain program. Without the explicit length
    // check, a slice reader's read_exact consumes the partial bytes and the
    // payload decodes to the default key instead of failing.
    #[test]
    fn test_initialize_payment_escrow_rejects_truncated_operator_key() {
        let mut bytes = borsh::to_vec(&ShredSubscriptionInstructionData::INITIALIZE_PAYMENT_ESCROW)
            .expect("discriminator serialization");
        bytes.extend_from_slice(&[0xAB; 31]);
        assert!(ShredSubscriptionInstructionData::try_from_slice(&bytes).is_err());
    }

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

    #[test]
    fn round_trip_initialize_validator_publisher_rewards() {
        round_trip(
            &ShredSubscriptionInstructionData::InitializeValidatorPublisherRewards(
                Pubkey::new_unique(),
            ),
        );
    }

    #[test]
    fn round_trip_configure_validator_publisher_rewards_direct() {
        round_trip(
            &ShredSubscriptionInstructionData::ConfigureValidatorPublisherRewards {
                rewards_token_owner_key: Pubkey::new_unique(),
                offchain_authorization: None,
            },
        );
    }

    #[test]
    fn round_trip_configure_validator_publisher_rewards_offchain() {
        round_trip(
            &ShredSubscriptionInstructionData::ConfigureValidatorPublisherRewards {
                rewards_token_owner_key: Pubkey::new_unique(),
                offchain_authorization: Some(ValidatorOffchainAuthorization {
                    deadline_slot: 999_888,
                    signature: [7u8; 64],
                }),
            },
        );
    }

    #[test]
    fn round_trip_distribute_validator_rewards() {
        let leaves: [&[u8]; 2] = [b"leaf_a", b"leaf_b"];
        let proof = MerkleProof::from_leaves(&leaves, 0, None).expect("two-leaf proof at index 0");
        round_trip(
            &ShredSubscriptionInstructionData::DistributeValidatorRewards {
                leader_slots: 1_234,
                proof,
            },
        );
    }
}
