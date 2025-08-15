//! Program instructions

use solana_program::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
};

use crate::ID;

/// Instructions supported by the program
#[derive(Clone, Debug, PartialEq)]
pub enum RecordInstruction<'a> {
    /// Create a new record
    ///
    /// Accounts expected by this instruction:
    ///
    /// 0. `[writable]` Record account, must be uninitialized
    /// 1. `[]` Record authority
    Initialize,

    /// Write to the provided record account
    ///
    /// Accounts expected by this instruction:
    ///
    /// 0. `[writable]` Record account, must be previously initialized
    /// 1. `[signer]` Current record authority
    Write {
        /// Offset to start writing record, expressed as `u64`.
        offset: u64,
        /// Data to replace the existing record data
        data: &'a [u8],
    },

    /// TODO: Remove this instruction. We do not want the ability for an author
    /// to change its authority over a record it created. Because we plan on
    /// using deterministic seeds, which includes the payer's public key,
    /// reading records with headers that reveal a different write authority
    /// may be confusing.
    SetAuthority,

    /// TODO: Remove this instruction. We want to prevent the ability for
    /// authors from closing their records. Even though they can overwrite
    /// their records, we want to make it difficult to completely purge them
    /// entirely from existence. An improvement would be to add an instruction
    /// to finalize records to prevent any subsequent writes.
    CloseAccount,

    /// Reallocate additional space in a record account
    ///
    /// If the record account already has enough space to hold the specified
    /// data length, then the instruction does nothing.
    ///
    /// Accounts expected by this instruction:
    ///
    /// 0. `[writable]` The record account to reallocate
    /// 1. `[signer]` The account's owner
    Reallocate(u64),
}

impl<'a> RecordInstruction<'a> {
    const INITIALIZE: u8 = 0;
    const WRITE: u8 = 1;
    const SET_AUTHORITY: u8 = 2;
    const CLOSE_ACCOUNT: u8 = 3;
    const REALLOCATE: u8 = 4;

    /// Unpacks a byte buffer into a [`RecordInstruction`].
    pub fn unpack(input: &'a [u8]) -> Option<Self> {
        const U32_BYTES: usize = size_of::<u32>();
        const U64_BYTES: usize = size_of::<u64>();

        let (&tag, rest) = input.split_first()?;

        match tag {
            Self::INITIALIZE => Some(Self::Initialize),
            Self::WRITE => {
                let offset = rest
                    .get(..U64_BYTES)
                    .and_then(|slice| slice.try_into().ok())
                    .map(u64::from_le_bytes)?;
                let (length, data) = rest[U64_BYTES..].split_at(U32_BYTES);
                let length = length.try_into().map(u32::from_le_bytes).ok()? as usize;

                Some(Self::Write {
                    offset,
                    data: &data[..length],
                })
            }
            Self::SET_AUTHORITY => Some(Self::SetAuthority),
            Self::CLOSE_ACCOUNT => Some(Self::CloseAccount),
            Self::REALLOCATE => {
                let data_length = rest
                    .get(..U64_BYTES)
                    .and_then(|slice| slice.try_into().ok())
                    .map(u64::from_le_bytes)?;

                Some(Self::Reallocate(data_length))
            }
            _ => None,
        }
    }

    /// Packs a [`RecordInstruction`] into a byte buffer.
    pub fn pack(&self) -> Vec<u8> {
        let mut buf = Vec::with_capacity(size_of::<Self>());
        match self {
            Self::Initialize => buf.push(Self::INITIALIZE),
            Self::Write { offset, data } => {
                buf.push(Self::WRITE);
                buf.extend_from_slice(&offset.to_le_bytes());
                buf.extend_from_slice(&(data.len() as u32).to_le_bytes());
                buf.extend_from_slice(data);
            }
            Self::SetAuthority => buf.push(Self::SET_AUTHORITY),
            Self::CloseAccount => buf.push(Self::CLOSE_ACCOUNT),
            Self::Reallocate(data_length) => {
                buf.push(Self::REALLOCATE);
                buf.extend_from_slice(&data_length.to_le_bytes());
            }
        };
        buf
    }
}

/// Create a `RecordInstruction::Initialize` instruction
pub fn initialize(record_account: &Pubkey, authority: &Pubkey) -> Instruction {
    Instruction {
        program_id: ID,
        accounts: vec![
            AccountMeta::new(*record_account, false),
            AccountMeta::new_readonly(*authority, false),
        ],
        data: RecordInstruction::Initialize.pack(),
    }
}

/// Create a `RecordInstruction::Write` instruction
pub fn write(record_account: &Pubkey, signer: &Pubkey, offset: u64, data: &[u8]) -> Instruction {
    Instruction {
        program_id: ID,
        accounts: vec![
            AccountMeta::new(*record_account, false),
            AccountMeta::new_readonly(*signer, true),
        ],
        data: RecordInstruction::Write { offset, data }.pack(),
    }
}

/// Create a `RecordInstruction::SetAuthority` instruction
pub fn set_authority(
    record_account: &Pubkey,
    signer: &Pubkey,
    new_authority: &Pubkey,
) -> Instruction {
    Instruction {
        program_id: ID,
        accounts: vec![
            AccountMeta::new(*record_account, false),
            AccountMeta::new_readonly(*signer, true),
            AccountMeta::new_readonly(*new_authority, false),
        ],
        data: RecordInstruction::SetAuthority.pack(),
    }
}

/// Create a `RecordInstruction::CloseAccount` instruction
pub fn close_account(record_account: &Pubkey, signer: &Pubkey, receiver: &Pubkey) -> Instruction {
    Instruction {
        program_id: ID,
        accounts: vec![
            AccountMeta::new(*record_account, false),
            AccountMeta::new_readonly(*signer, true),
            AccountMeta::new(*receiver, false),
        ],
        data: RecordInstruction::CloseAccount.pack(),
    }
}

/// Create a `RecordInstruction::Reallocate` instruction
pub fn reallocate(record_account: &Pubkey, signer: &Pubkey, data_length: u64) -> Instruction {
    Instruction {
        program_id: ID,
        accounts: vec![
            AccountMeta::new(*record_account, false),
            AccountMeta::new_readonly(*signer, true),
        ],
        data: RecordInstruction::Reallocate(data_length).pack(),
    }
}

#[cfg(test)]
mod tests {
    use crate::state::tests::TEST_BYTES;

    use super::*;

    #[test]
    fn serialize_initialize() {
        let instruction = RecordInstruction::Initialize;
        let expected = vec![0];
        assert_eq!(instruction.pack(), expected);
        assert_eq!(RecordInstruction::unpack(&expected).unwrap(), instruction);
    }

    #[test]
    fn serialize_write() {
        let data = &TEST_BYTES;
        let offset = 0u64;
        let instruction = RecordInstruction::Write { offset: 0, data };
        let mut expected = vec![1];
        expected.extend_from_slice(&offset.to_le_bytes());
        expected.extend_from_slice(&(data.len() as u32).to_le_bytes());
        expected.extend_from_slice(data);
        assert_eq!(instruction.pack(), expected);
        assert_eq!(RecordInstruction::unpack(&expected).unwrap(), instruction);
    }

    #[test]
    fn serialize_set_authority() {
        let instruction = RecordInstruction::SetAuthority;
        let expected = vec![2];
        assert_eq!(instruction.pack(), expected);
        assert_eq!(RecordInstruction::unpack(&expected).unwrap(), instruction);
    }

    #[test]
    fn serialize_close_account() {
        let instruction = RecordInstruction::CloseAccount;
        let expected = vec![3];
        assert_eq!(instruction.pack(), expected);
        assert_eq!(RecordInstruction::unpack(&expected).unwrap(), instruction);
    }

    #[test]
    fn serialize_reallocate() {
        let data_length = 16u64;
        let instruction = RecordInstruction::Reallocate(data_length);
        let mut expected = vec![4];
        expected.extend_from_slice(&data_length.to_le_bytes());
        assert_eq!(instruction.pack(), expected);
        assert_eq!(RecordInstruction::unpack(&expected).unwrap(), instruction);
    }

    #[test]
    fn deserialize_invalid_instruction() {
        let mut expected = vec![12];
        expected.extend_from_slice(&TEST_BYTES);
        assert!(RecordInstruction::unpack(&expected).is_none());
    }
}
