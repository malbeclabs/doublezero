mod prepaid_connection;

pub use prepaid_connection::*;

//

use std::collections::VecDeque;

use borsh::{BorshDeserialize, BorshSerialize};
use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{types::StorageGap, Discriminator, PrecomputedDiscriminator};
use solana_account_info::MAX_PERMITTED_DATA_INCREASE;
use solana_pubkey::Pubkey;

use crate::types::DoubleZeroEpoch;

pub const fn absolute_max_journal_entries() -> usize {
    let remaining_size = MAX_PERMITTED_DATA_INCREASE - size_of::<Journal>() - 4;
    remaining_size / size_of::<JournalEntry>()
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct Journal {
    /// This seed will be used to sign for token transfers.
    pub bump_seed: u8,

    /// Cache this seed to validate token PDA address.
    pub token_2z_pda_bump_seed: u8,
    _padding: [u8; 6],

    pub prepaid_connection_parameters: PrepaidConnectionParameters,

    pub total_sol_balance: u64,

    /// Based on interactions with the program to deposit 2Z, this is our expected balance. This
    /// balance may deviate from the actual balance in the 2Z Token account because folks may
    /// transfer tokens directly to that account (not intended). So if we wanted any recourse to
    /// do something with the excess amount in this token account, we can simply compute the
    /// difference between the token account balance and this.
    pub total_2z_balance: u64,

    /// 8 * 32 bytes of a storage gap in case we need more fields.
    _storage_gap: StorageGap<8>,
}

impl PrecomputedDiscriminator for Journal {
    const DISCRIMINATOR: Discriminator<8> = Discriminator::new_sha2(b"dz::account::journal");
}

impl Journal {
    pub const SEED_PREFIX: &'static [u8] = b"journal";

    /// Max allowable entries. Due to the CPI constraint of 10kb when creating the Journal account
    /// (and to avoid performing a realloc for this PDA), we do not allow the maximum entries field
    /// to be configured beyond this maximum value.
    ///
    /// Each entry represents one DZ epoch's payment. If the maximum entries value is configured to
    /// be 200 DZ epochs for example, this value equates to roughly 400 days worth of payments. It
    /// is unlikely that the configured maximum entries value will ever be this large.
    pub const MAX_CONFIGURABLE_ENTRIES: u16 = 200;

    pub fn find_address() -> (Pubkey, u8) {
        Pubkey::find_program_address(&[Self::SEED_PREFIX], &crate::ID)
    }

    pub fn checked_maximum_entries(&self) -> Option<u16> {
        let maximum_entries = self.prepaid_connection_parameters.maximum_entries;

        if maximum_entries == 0 {
            None
        } else {
            Some(maximum_entries)
        }
    }

    pub fn checked_minimum_allowed_dz_epochs(&self) -> Option<u16> {
        let minimum_allowed_dz_epochs =
            self.prepaid_connection_parameters.minimum_allowed_dz_epochs;

        if minimum_allowed_dz_epochs == 0 {
            None
        } else {
            Some(minimum_allowed_dz_epochs)
        }
    }

    pub fn checked_journal_entries(mut data: &[u8]) -> Option<JournalEntries> {
        BorshDeserialize::deserialize(&mut data).ok()
    }

    pub fn checked_activation_cost(&self) -> Option<u32> {
        let activation_cost = self.prepaid_connection_parameters.activation_cost;

        if activation_cost == 0 {
            None
        } else {
            Some(activation_cost)
        }
    }

    pub fn checked_activation_cost_amount(&self, decimals: u8) -> Option<u64> {
        let activation_cost = self.checked_activation_cost()?;

        checked_pow_10(decimals)?.checked_mul(activation_cost.into())
    }

    pub fn checked_cost_per_dz_epoch(&self) -> Option<u32> {
        let cost_per_dz_epoch = self.prepaid_connection_parameters.cost_per_dz_epoch;

        if cost_per_dz_epoch == 0 {
            None
        } else {
            Some(cost_per_dz_epoch)
        }
    }

    pub fn checked_cost_per_dz_epoch_amount(&self, num_entries: u16, decimals: u8) -> Option<u64> {
        let cost_per_epoch = self.checked_cost_per_dz_epoch()?;

        checked_pow_10(decimals)?
            .checked_mul(cost_per_epoch.into())?
            .checked_mul(num_entries.into())
    }
}

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, Copy, PartialEq, Eq)]
pub struct JournalEntry {
    pub dz_epoch: DoubleZeroEpoch,
    pub amount: u32,
}

impl JournalEntry {
    pub fn checked_amount(&self, decimals: u8) -> Option<u64> {
        checked_pow_10(decimals)?.checked_mul(self.amount.into())
    }
}

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, Default, PartialEq, Eq)]
pub struct JournalEntries(pub VecDeque<JournalEntry>);

impl JournalEntries {
    pub fn last_dz_epoch(&self) -> Option<DoubleZeroEpoch> {
        self.0.back().map(|entry| entry.dz_epoch)
    }

    pub fn update(
        &mut self,
        next_dz_epoch: DoubleZeroEpoch,
        valid_through_dz_epoch: DoubleZeroEpoch,
        cost_per_epoch: u32,
    ) -> Option<u16> {
        // If we want to add service between the next DZ epoch and the DZ epoch where service is
        // valid through, we take the difference and add one because service should be active
        // starting at the next DZ epoch.
        let num_epochs = valid_through_dz_epoch
            .value()
            .checked_sub(next_dz_epoch.value())?
            .saturating_add(1);

        // Do nothing if the difference between epochs is too large. The maximum entries parameter
        // in the journal is configured as u16.
        if num_epochs > u16::MAX as u64 {
            return None;
        }

        let entries = &mut self.0;

        // First, add amounts to existing entries where we need to allocate 2Z to specific DZ epochs.
        entries
            .iter_mut()
            .filter(|entry| {
                entry.dz_epoch >= next_dz_epoch && entry.dz_epoch <= valid_through_dz_epoch
            })
            .for_each(|entry| entry.amount = entry.amount.saturating_add(cost_per_epoch));

        // Find the last epoch so we can push the cost-per-epoch as new entries.
        let last_dz_epoch = entries
            .back()
            .map(|entry| entry.dz_epoch.saturating_add_duration(1))
            .unwrap_or(next_dz_epoch);

        if last_dz_epoch <= valid_through_dz_epoch {
            for epoch_value in last_dz_epoch.value()..=valid_through_dz_epoch.value() {
                entries.push_back(JournalEntry {
                    dz_epoch: DoubleZeroEpoch::new(epoch_value),
                    amount: cost_per_epoch,
                });
            }
        }

        Some(num_epochs as u16)
    }

    pub fn front_entry(&self) -> Option<&JournalEntry> {
        self.0.front()
    }

    pub fn pop_front_entry(&mut self) -> Option<JournalEntry> {
        self.0.pop_front()
    }
}

#[inline(always)]
fn checked_pow_10(decimals: u8) -> Option<u64> {
    u64::checked_pow(10, decimals.into())
}

//

const _: () = assert!(size_of::<Journal>() == 552, "`Journal` size changed");

const _: () = assert!(
    absolute_max_journal_entries() == 605,
    "Absolute max journal entries changed"
);

const _: () = assert!(
    (Journal::MAX_CONFIGURABLE_ENTRIES as usize) <= absolute_max_journal_entries(),
    "Journal entries size is too large"
);

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_journal_entries_update_full_overlap() {
        let next_dz_epoch = DoubleZeroEpoch::new(0);
        let valid_through_dz_epoch = DoubleZeroEpoch::new(5);

        let cost_per_epoch = 69;

        let mut journal_entries = JournalEntries(
            vec![
                JournalEntry {
                    dz_epoch: DoubleZeroEpoch::new(0),
                    amount: 100,
                },
                JournalEntry {
                    dz_epoch: DoubleZeroEpoch::new(1),
                    amount: 200,
                },
            ]
            .into(),
        );

        journal_entries.update(next_dz_epoch, valid_through_dz_epoch, cost_per_epoch);

        let expected_journal_entries = JournalEntries(
            vec![
                JournalEntry {
                    dz_epoch: DoubleZeroEpoch::new(0),
                    amount: 169,
                },
                JournalEntry {
                    dz_epoch: DoubleZeroEpoch::new(1),
                    amount: 269,
                },
                JournalEntry {
                    dz_epoch: DoubleZeroEpoch::new(2),
                    amount: 69,
                },
                JournalEntry {
                    dz_epoch: DoubleZeroEpoch::new(3),
                    amount: 69,
                },
                JournalEntry {
                    dz_epoch: DoubleZeroEpoch::new(4),
                    amount: 69,
                },
                JournalEntry {
                    dz_epoch: DoubleZeroEpoch::new(5),
                    amount: 69,
                },
            ]
            .into(),
        );
        assert_eq!(journal_entries, expected_journal_entries);
    }
}
