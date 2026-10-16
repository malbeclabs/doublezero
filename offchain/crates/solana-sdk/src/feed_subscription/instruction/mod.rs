pub mod account;

use std::io;

use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_tools::{DISCRIMINATOR_LEN, Discriminator};
use solana_sdk::pubkey::Pubkey;

/// An authorization the validator identity signs with `solana sign-offchain-message`: the ed25519
/// signature and the last slot at which the program accepts it.
#[derive(Debug, Clone, PartialEq, Eq, BorshSerialize, BorshDeserialize)]
pub struct ValidatorOffchainAuthorization {
    pub deadline_slot: u64,
    pub signature: [u8; 64],
}

/// The feed subscription instructions this SDK builds. The onchain program has more. Add a
/// variant here, with its discriminator, when a command needs one.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum FeedSubscriptionInstructionData {
    /// Manager only (pause-gated). Sets a validator client's share, in basis points, up to 3,500.
    ConfigureValidatorClientRewardsProportion(u16),
    /// Validator identity or its offchain signature (pause-gated). Creates the publisher's
    /// `FeedPublisherRewards` when missing and sets its rewards owner and mint.
    ConfigureFeedPublisherRewards {
        rewards_token_owner_key: Pubkey,
        /// `None` when the validator identity signs the transaction.
        offchain_authorization: Option<ValidatorOffchainAuthorization>,
    },
}

impl FeedSubscriptionInstructionData {
    pub const CONFIGURE_VALIDATOR_CLIENT_REWARDS_PROPORTION: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::configure_validator_client_rewards_proportion");
    pub const CONFIGURE_FEED_PUBLISHER_REWARDS: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::configure_feed_publisher_rewards");
}

impl BorshSerialize for FeedSubscriptionInstructionData {
    fn serialize<W: io::Write>(&self, writer: &mut W) -> io::Result<()> {
        match self {
            Self::ConfigureValidatorClientRewardsProportion(proportion_bps) => {
                Self::CONFIGURE_VALIDATOR_CLIENT_REWARDS_PROPORTION.serialize(writer)?;
                proportion_bps.serialize(writer)
            }
            Self::ConfigureFeedPublisherRewards {
                rewards_token_owner_key,
                offchain_authorization,
            } => {
                Self::CONFIGURE_FEED_PUBLISHER_REWARDS.serialize(writer)?;
                rewards_token_owner_key.serialize(writer)?;
                offchain_authorization.serialize(writer)
            }
        }
    }
}

impl BorshDeserialize for FeedSubscriptionInstructionData {
    fn deserialize_reader<R: io::Read>(reader: &mut R) -> io::Result<Self> {
        match Discriminator::deserialize_reader(reader)? {
            Self::CONFIGURE_VALIDATOR_CLIENT_REWARDS_PROPORTION => {
                let proportion_bps = u16::deserialize_reader(reader)?;
                Ok(Self::ConfigureValidatorClientRewardsProportion(
                    proportion_bps,
                ))
            }
            Self::CONFIGURE_FEED_PUBLISHER_REWARDS => {
                let rewards_token_owner_key = Pubkey::deserialize_reader(reader)?;
                let offchain_authorization =
                    Option::<ValidatorOffchainAuthorization>::deserialize_reader(reader)?;
                Ok(Self::ConfigureFeedPublisherRewards {
                    rewards_token_owner_key,
                    offchain_authorization,
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
    fn test_configure_validator_client_rewards_proportion_frozen_bytes() {
        let ix = FeedSubscriptionInstructionData::ConfigureValidatorClientRewardsProportion(3_500);
        let bytes = borsh::to_vec(&ix).unwrap();

        // The discriminator is the one in the onchain IDL, then the share as a little-endian u16.
        assert_eq!(bytes, vec![230, 201, 59, 64, 173, 186, 113, 43, 0xAC, 0x0D]);
        assert_eq!(
            FeedSubscriptionInstructionData::try_from_slice(&bytes).unwrap(),
            ix
        );
    }

    #[test]
    fn test_configure_feed_publisher_rewards_frozen_bytes() {
        // The discriminator in the onchain IDL, then the owner.
        let mut direct_bytes = vec![79, 127, 26, 89, 7, 225, 23, 101];
        direct_bytes.extend_from_slice(&[3; 32]);
        let mut offchain_bytes = direct_bytes.clone();
        // No authorization.
        direct_bytes.push(0);
        // Some authorization, then its deadline slot as a little-endian u64 and its signature.
        offchain_bytes.push(1);
        offchain_bytes.extend_from_slice(&[0xEF, 0xBE, 0xAD, 0xDE, 0, 0, 0, 0]);
        offchain_bytes.extend_from_slice(&[7; 64]);

        // The program serializes these two instructions to the same bytes.
        for (offchain_authorization, expected_bytes) in [
            (None, direct_bytes),
            (
                Some(ValidatorOffchainAuthorization {
                    deadline_slot: 0xdead_beef,
                    signature: [7; 64],
                }),
                offchain_bytes,
            ),
        ] {
            let ix = FeedSubscriptionInstructionData::ConfigureFeedPublisherRewards {
                rewards_token_owner_key: Pubkey::new_from_array([3; 32]),
                offchain_authorization,
            };
            let bytes = borsh::to_vec(&ix).unwrap();
            assert_eq!(bytes, expected_bytes);
            assert_eq!(
                FeedSubscriptionInstructionData::try_from_slice(&bytes).unwrap(),
                ix
            );
        }
    }

    #[test]
    fn test_unknown_discriminator_is_refused() {
        let error = FeedSubscriptionInstructionData::try_from_slice(&[0; 10]).unwrap_err();
        assert_eq!(error.kind(), io::ErrorKind::InvalidData);
    }
}
