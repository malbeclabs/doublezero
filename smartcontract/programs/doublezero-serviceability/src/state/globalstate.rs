use crate::{bytereader::ByteReader, state::accounttype::AccountType};
use borsh::BorshSerialize;
use core::fmt;
use solana_program::{account_info::AccountInfo, program_error::ProgramError, pubkey::Pubkey};

#[derive(BorshSerialize, Debug, PartialEq, Clone)]
pub struct GlobalState {
    pub account_type: AccountType,         // 1
    pub bump_seed: u8,                     // 1
    pub account_index: u128,               // 16
    pub foundation_allowlist: Vec<Pubkey>, // 4 + 32 * len
    pub device_allowlist: Vec<Pubkey>,     // 4 + 32 * len
    pub user_allowlist: Vec<Pubkey>,       // 4 + 32 * len
}

impl fmt::Display for GlobalState {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, account_index: {}, foundation_allowlist: {:?}, device_allowlist: {:?}, user_allowlist: {:?}",
            self.account_type, self.account_index, self.foundation_allowlist, self.device_allowlist, self.user_allowlist
        )
    }
}

impl GlobalState {
    pub fn size(&self) -> usize {
        1 + 1
            + 16
            + 4
            + (self.foundation_allowlist.len() * 32)
            + 4
            + (self.device_allowlist.len() * 32)
            + 4
            + (self.user_allowlist.len() * 32)
    }
}

impl From<&[u8]> for GlobalState {
    fn from(data: &[u8]) -> Self {
        let mut parser = ByteReader::new(data);

        Self {
            account_type: parser.read_enum(),
            bump_seed: parser.read_u8(),
            account_index: parser.read_u128(),
            foundation_allowlist: parser.read_pubkey_vec(),
            device_allowlist: parser.read_pubkey_vec(),
            user_allowlist: parser.read_pubkey_vec(),
        }
    }
}

impl TryFrom<&AccountInfo<'_>> for GlobalState {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        Ok(Self::from(&data[..]))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_location_serialization() {
        let val = GlobalState {
            account_type: AccountType::GlobalState,
            bump_seed: 1,
            account_index: 123,
            foundation_allowlist: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            device_allowlist: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            user_allowlist: vec![Pubkey::new_unique(), Pubkey::new_unique()],
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = GlobalState::from(&data[..]);

        assert_eq!(val.size(), val2.size());
        assert_eq!(val.account_index, val2.account_index);
        assert_eq!(val.foundation_allowlist, val2.foundation_allowlist);
        assert_eq!(val.device_allowlist, val2.device_allowlist);
        assert_eq!(val.user_allowlist, val2.user_allowlist);
        assert_eq!(data.len(), val.size(), "Invalid Size");
    }
}
