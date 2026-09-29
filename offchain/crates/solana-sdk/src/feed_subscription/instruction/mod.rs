pub mod account;

use std::io;

use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_tools::{DISCRIMINATOR_LEN, Discriminator};

/// The feed subscription instructions this SDK builds. The onchain program has more. Add a
/// variant here, with its discriminator, when a command needs one.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum FeedSubscriptionInstructionData {
    /// Manager only (pause-gated). Sets a validator client's share, in basis points, up to 3,500.
    ConfigureValidatorClientRewardsProportion(u16),
}

impl FeedSubscriptionInstructionData {
    pub const CONFIGURE_VALIDATOR_CLIENT_REWARDS_PROPORTION: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::configure_validator_client_rewards_proportion");
}

impl BorshSerialize for FeedSubscriptionInstructionData {
    fn serialize<W: io::Write>(&self, writer: &mut W) -> io::Result<()> {
        match self {
            Self::ConfigureValidatorClientRewardsProportion(proportion_bps) => {
                Self::CONFIGURE_VALIDATOR_CLIENT_REWARDS_PROPORTION.serialize(writer)?;
                proportion_bps.serialize(writer)
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
    fn test_unknown_discriminator_is_refused() {
        let error = FeedSubscriptionInstructionData::try_from_slice(&[0; 10]).unwrap_err();
        assert_eq!(error.kind(), io::ErrorKind::InvalidData);
    }
}
