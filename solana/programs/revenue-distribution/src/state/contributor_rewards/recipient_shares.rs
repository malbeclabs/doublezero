use bytemuck::{Pod, Zeroable};
use solana_pubkey::Pubkey;

use crate::types::UnitShare16;

pub const MAX_RECIPIENTS: usize = 8;

#[derive(Debug, Clone, Copy, Default, PartialEq, Pod, Zeroable)]
#[repr(C, align(2))]
pub struct RecipientShare {
    pub recipient_key: Pubkey,
    pub share: UnitShare16,
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct RecipientShares([RecipientShare; MAX_RECIPIENTS]);

impl RecipientShares {
    pub fn new(recipients: &[(Pubkey, u16)]) -> Option<Self> {
        if recipients.len() > MAX_RECIPIENTS {
            return None;
        }

        let mut out = [RecipientShare::default(); MAX_RECIPIENTS];

        let mut total_share = UnitShare16::MIN;

        for (i, (recipient_key, share)) in recipients.iter().enumerate() {
            if recipient_key == &Pubkey::default() {
                return None;
            }

            let share = UnitShare16::new(*share)?;

            // Cannot have a zero share.
            if share == UnitShare16::MIN {
                return None;
            }

            // Keep track of the running sum of shares to make sure it does not
            // exceed 100%.
            total_share = total_share.checked_add(share)?;

            out[i] = RecipientShare {
                recipient_key: *recipient_key,
                share,
            };
        }

        if total_share != UnitShare16::MAX {
            return None;
        }

        Some(Self(out))
    }

    /// Returns an iterator over all recipient shares (including default
    /// entries).
    pub fn iter(&self) -> impl Iterator<Item = &RecipientShare> {
        self.0.iter()
    }

    /// Returns an iterator over only the active (non-default) recipient shares.
    pub fn active_iter(&self) -> impl Iterator<Item = &RecipientShare> {
        self.0
            .iter()
            .filter(|share| share.recipient_key != Pubkey::default())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_recipient_shares() {
        let recipients = vec![
            (Pubkey::new_unique(), 1_000),
            (Pubkey::new_unique(), 2_000),
            (Pubkey::new_unique(), 3_000),
            (Pubkey::new_unique(), 4_000),
        ];

        let shares = RecipientShares::new(&recipients).unwrap();

        assert_eq!(shares.0[0].recipient_key, recipients[0].0);
        assert_eq!(shares.0[0].share, UnitShare16::new(1_000).unwrap());

        assert_eq!(shares.0[1].recipient_key, recipients[1].0);
        assert_eq!(shares.0[1].share, UnitShare16::new(2_000).unwrap());

        assert_eq!(shares.0[2].recipient_key, recipients[2].0);
        assert_eq!(shares.0[2].share, UnitShare16::new(3_000).unwrap());

        assert_eq!(shares.0[3].recipient_key, recipients[3].0);
        assert_eq!(shares.0[3].share, UnitShare16::new(4_000).unwrap());

        let recipients = [
            (Pubkey::new_unique(), 1_000),
            (Pubkey::new_unique(), 0_000),
            (Pubkey::new_unique(), 5_000),
            (Pubkey::new_unique(), 4_000),
        ];

        assert!(RecipientShares::new(&recipients).is_none());
    }

    #[test]
    fn test_recipient_shares_overflow() {
        let recipients = vec![
            (Pubkey::new_unique(), 1_000),
            (Pubkey::new_unique(), 2_000),
            (Pubkey::new_unique(), 3_000),
            (Pubkey::new_unique(), 4_000),
            (Pubkey::new_unique(), 5_000),
        ];

        let shares = RecipientShares::new(&recipients);

        assert!(shares.is_none());
    }

    #[test]
    fn test_recipient_shares_zero_key() {
        let recipients = vec![
            (Pubkey::new_unique(), 1_000),
            (Pubkey::default(), 2_000),
            (Pubkey::new_unique(), 3_000),
        ];

        let shares = RecipientShares::new(&recipients);

        assert!(shares.is_none());
    }

    #[test]
    fn test_iterator() {
        let recipients = vec![(Pubkey::new_unique(), 3_000), (Pubkey::new_unique(), 7_000)];

        let shares = RecipientShares::new(&recipients).unwrap();

        assert_eq!(shares.iter().count(), MAX_RECIPIENTS);
        assert_eq!(shares.active_iter().count(), 2);
    }

    #[test]
    fn test_iterator_single_recipient() {
        let recipients = vec![(Pubkey::new_unique(), 10_000)];

        let shares = RecipientShares::new(&recipients).unwrap();

        let active = shares.active_iter().collect::<Vec<_>>();
        assert_eq!(active.len(), 1);
        assert_eq!(active[0].share, UnitShare16::MAX);
    }

    #[test]
    fn test_iterator_max_recipients() {
        let recipients = vec![
            (Pubkey::new_unique(), 1_250),
            (Pubkey::new_unique(), 1_250),
            (Pubkey::new_unique(), 1_250),
            (Pubkey::new_unique(), 1_250),
            (Pubkey::new_unique(), 1_250),
            (Pubkey::new_unique(), 1_250),
            (Pubkey::new_unique(), 1_250),
            (Pubkey::new_unique(), 1_250),
        ];

        let shares = RecipientShares::new(&recipients).unwrap();

        assert_eq!(shares.iter().count(), shares.active_iter().count());
    }
}
