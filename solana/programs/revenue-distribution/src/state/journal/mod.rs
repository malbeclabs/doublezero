mod prepaid_connection;

pub use prepaid_connection::*;

//

use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{types::StorageGap, Discriminator, PrecomputedDiscriminator};
use solana_pubkey::Pubkey;

use crate::types::DoubleZeroEpoch;

pub const JOURNAL_ENTRIES_ABSOLUTE_MAX_LENGTH: u16 = 256;

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct Journal {
    /// This seed will be used to sign for token transfers.
    pub bump_seed: u8,

    /// Cache this seed to validate token PDA address.
    pub token_2z_pda_bump_seed: u8,
    _padding_0: [u8; 6],

    pub prepaid_connection_parameters: PrepaidConnectionParameters,

    pub total_sol_balance: u64,

    /// Based on interactions with the program to deposit 2Z, this is our
    /// expected balance. This balance may deviate from the actual balance in
    /// the 2Z Token account because folks may transfer tokens directly to
    /// that account (not intended). So if we wanted any recourse to do
    /// something with the excess amount in this token account, we can simply
    /// compute the difference between the token account balance and this.
    pub total_2z_balance: u64,

    pub swap_2z_destination_balance: u64,

    pub swapped_sol_amount: u64,

    pub next_dz_epoch_to_sweep_tokens: DoubleZeroEpoch,

    _padding: [u64; 1],

    pub prepayment_entries: PrepaymentEntries,

    /// 8 * 32 bytes of a storage gap in case we need more fields.
    _storage_gap: StorageGap<7>,
}

impl PrecomputedDiscriminator for Journal {
    const DISCRIMINATOR: Discriminator<8> = Discriminator::new_sha2(b"dz::account::journal");
}

impl Journal {
    pub const SEED_PREFIX: &'static [u8] = b"journal";

    /// Max allowable entries. Due to the CPI constraint of 10kb when creating
    /// the Journal account (and to avoid performing a realloc for this PDA), we
    /// do not allow the maximum entries field to be configured beyond this
    /// maximum value.
    ///
    /// Each entry represents one DZ epoch's payment. If the maximum entries
    /// value is configured to be 32 DZ epochs for example, this value equates
    /// to roughly 64 days worth of payments. It is unlikely that the
    /// configured maximum entries value will ever be this large.
    pub const MAX_CONFIGURABLE_ENTRIES: u16 = 32;

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

    pub fn checked_cost_per_dz_epoch(&self, decimals: u8) -> Option<u64> {
        let cost_per_dz_epoch = self.prepaid_connection_parameters.cost_per_dz_epoch;

        if cost_per_dz_epoch == 0 {
            None
        } else {
            checked_pow_10(decimals)?.checked_mul(cost_per_dz_epoch.into())
        }
    }

    pub fn checked_cost_per_dz_epoch_amount(&self, num_entries: u16, decimals: u8) -> Option<u64> {
        self.checked_cost_per_dz_epoch(decimals)?
            .checked_mul(num_entries.into())
    }
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct PrepaymentEntry {
    pub dz_epoch: DoubleZeroEpoch,
    pub amount: u64,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct PrepaymentEntries {
    pub head: u16,
    pub length: u16,
    _padding: [u8; 4],
    pub entries: [PrepaymentEntry; JOURNAL_ENTRIES_ABSOLUTE_MAX_LENGTH as usize],
}

impl Default for PrepaymentEntries {
    fn default() -> Self {
        Self {
            head: 0,
            length: 0,
            _padding: [0; 4],
            entries: [Default::default(); JOURNAL_ENTRIES_ABSOLUTE_MAX_LENGTH as usize],
        }
    }
}

impl PrepaymentEntries {
    pub fn update(
        &mut self,
        next_dz_epoch: DoubleZeroEpoch,
        valid_through_dz_epoch: DoubleZeroEpoch,
        cost_per_epoch: u64,
    ) -> Option<u16> {
        // If we want to add service between the next DZ epoch and the DZ epoch
        // where service is valid through, we take the difference and add one
        // because service should be active at the next DZ epoch.
        let num_epochs = valid_through_dz_epoch
            .value()
            .checked_sub(next_dz_epoch.value())?
            .saturating_add(1);

        // Do nothing if the difference between epochs is too large. The maximum
        // entries parameter in the journal is configured as `u16`.
        if num_epochs > JOURNAL_ENTRIES_ABSOLUTE_MAX_LENGTH as u64 {
            return None;
        }

        // Calculate how many new entries we need to add.
        let last_dz_epoch = if self.length == 0 {
            next_dz_epoch
        } else {
            let last_index = (self.head + self.length - 1) % JOURNAL_ENTRIES_ABSOLUTE_MAX_LENGTH;
            self.entries[last_index as usize]
                .dz_epoch
                .saturating_add_duration(1)
        };

        let new_entries_needed = if last_dz_epoch <= valid_through_dz_epoch {
            valid_through_dz_epoch
                .value()
                .saturating_sub(last_dz_epoch.value())
                .saturating_add(1)
        } else {
            0
        };

        // Check if projected length would exceed maximum.
        let projected_length = self.length as u64 + new_entries_needed;
        if projected_length > JOURNAL_ENTRIES_ABSOLUTE_MAX_LENGTH as u64 {
            return None;
        }

        // Update existing entries in the range.
        for i in 0..self.length {
            let index = (self.head + i) % JOURNAL_ENTRIES_ABSOLUTE_MAX_LENGTH;
            let entry = &mut self.entries[index as usize];

            if entry.dz_epoch >= next_dz_epoch && entry.dz_epoch <= valid_through_dz_epoch {
                entry.amount = entry.amount.saturating_add(cost_per_epoch);
            }
        }

        // Add new entries.
        if last_dz_epoch <= valid_through_dz_epoch {
            for epoch_value in last_dz_epoch.value()..=valid_through_dz_epoch.value() {
                let new_index = (self.head + self.length) % JOURNAL_ENTRIES_ABSOLUTE_MAX_LENGTH;
                self.entries[new_index as usize] = PrepaymentEntry {
                    dz_epoch: DoubleZeroEpoch::new(epoch_value),
                    amount: cost_per_epoch,
                };
                self.length += 1;
            }
        }

        Some(num_epochs as u16)
    }

    pub fn front_entry(&self) -> Option<&PrepaymentEntry> {
        if self.length == 0 {
            None
        } else {
            Some(&self.entries[self.head as usize])
        }
    }

    pub fn pop_front_entry(&mut self) -> Option<PrepaymentEntry> {
        if self.length == 0 {
            None
        } else {
            let entry = std::mem::take(&mut self.entries[self.head as usize]);
            self.head = (self.head + 1) % JOURNAL_ENTRIES_ABSOLUTE_MAX_LENGTH;
            self.length -= 1;
            Some(entry)
        }
    }
}

#[inline(always)]
fn checked_pow_10(decimals: u8) -> Option<u64> {
    u64::checked_pow(10, decimals.into())
}

//

const _: () = assert!(size_of::<Journal>() == 4_656, "`Journal` size changed");

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_journal_entries_update_full_overlap() {
        let next_dz_epoch = DoubleZeroEpoch::new(0);
        let valid_through_dz_epoch = DoubleZeroEpoch::new(5);

        let cost_per_epoch = 69;

        let mut journal_entries = PrepaymentEntries {
            length: 2,
            ..Default::default()
        };
        journal_entries.entries[0] = PrepaymentEntry {
            dz_epoch: DoubleZeroEpoch::new(0),
            amount: 100,
        };
        journal_entries.entries[1] = PrepaymentEntry {
            dz_epoch: DoubleZeroEpoch::new(1),
            amount: 200,
        };

        journal_entries.update(next_dz_epoch, valid_through_dz_epoch, cost_per_epoch);

        let mut expected_journal_entries = PrepaymentEntries {
            length: 6,
            ..Default::default()
        };
        expected_journal_entries.entries[0] = PrepaymentEntry {
            dz_epoch: DoubleZeroEpoch::new(0),
            amount: 169,
        };
        expected_journal_entries.entries[1] = PrepaymentEntry {
            dz_epoch: DoubleZeroEpoch::new(1),
            amount: 269,
        };
        expected_journal_entries.entries[2] = PrepaymentEntry {
            dz_epoch: DoubleZeroEpoch::new(2),
            amount: 69,
        };
        expected_journal_entries.entries[3] = PrepaymentEntry {
            dz_epoch: DoubleZeroEpoch::new(3),
            amount: 69,
        };
        expected_journal_entries.entries[4] = PrepaymentEntry {
            dz_epoch: DoubleZeroEpoch::new(4),
            amount: 69,
        };
        expected_journal_entries.entries[5] = PrepaymentEntry {
            dz_epoch: DoubleZeroEpoch::new(5),
            amount: 69,
        };

        assert_eq!(journal_entries, expected_journal_entries);
    }
}
