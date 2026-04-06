pub mod account;

use std::io;

use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_tools::{DISCRIMINATOR_LEN, Discriminator};

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
    /// Set the rewards proportion for a validator client.
    SetValidatorClientRewardsProportion(u16),
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
    pub const SET_VALIDATOR_CLIENT_REWARDS_PROPORTION: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::set_validator_client_rewards_proportion");
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
            Self::SetValidatorClientRewardsProportion(proportion) => {
                Self::SET_VALIDATOR_CLIENT_REWARDS_PROPORTION.serialize(writer)?;
                proportion.serialize(writer)
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
            Self::SET_VALIDATOR_CLIENT_REWARDS_PROPORTION => {
                let proportion = u16::deserialize_reader(reader)?;
                Ok(Self::SetValidatorClientRewardsProportion(proportion))
            }
            _ => Err(io::Error::new(
                io::ErrorKind::InvalidData,
                "Invalid discriminator",
            )),
        }
    }
}
