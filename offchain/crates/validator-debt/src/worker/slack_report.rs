use crate::transaction::DebtCollectionResults;

/// Summary struct used in tests to verify summary calculations.
#[allow(dead_code)]
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct DebtCollectionSummary {
    pub total_paid: u64,
    pub total_debt: u64,
    pub insufficient_funds_count: usize,
    pub visible_epoch_count: usize,
}

#[allow(dead_code)]
impl DebtCollectionSummary {
    /// Outstanding debt = total_debt - total_paid
    pub fn total_outstanding(&self) -> u64 {
        self.total_debt.saturating_sub(self.total_paid)
    }

    pub fn percentage_paid(&self) -> f64 {
        if self.total_debt == 0 {
            0.0
        } else {
            self.total_paid as f64 / self.total_debt as f64
        }
    }
}

/// Determines if a debt collection result should be displayed as a row in Slack.
///
/// A row is visible if:
/// - `total_validators > 0`
/// - `successful_transactions_count > 0`
///
/// Epochs that don't meet these criteria are skipped in the table display.
#[inline]
pub fn is_row_visible(dcr: &DebtCollectionResults) -> bool {
    dcr.total_validators > 0 && dcr.successful_transactions_count > 0
}

/// Filters debt collection results to only those that will be displayed in Slack.
pub fn visible_rows(results: &[DebtCollectionResults]) -> Vec<&DebtCollectionResults> {
    results.iter().filter(|dcr| is_row_visible(dcr)).collect()
}

#[allow(dead_code)]
pub fn compute_summary(results: &[&DebtCollectionResults]) -> DebtCollectionSummary {
    let mut summary = DebtCollectionSummary::default();

    for dcr in results {
        summary.total_paid += dcr.total_paid;
        summary.total_debt += dcr.total_debt;
        summary.insufficient_funds_count += dcr.insufficient_funds_count;
    }
    summary.visible_epoch_count = results.len();

    summary
}

/// filter to visible rows and compute summary in one step.
#[allow(dead_code)]
pub fn compute_visible_summary(results: &[DebtCollectionResults]) -> DebtCollectionSummary {
    let visible = visible_rows(results);
    compute_summary(&visible)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_result(
        dz_epoch: u64,
        total_validators: usize,
        successful_transactions_count: usize,
        total_debt: u64,
        total_paid: u64,
        insufficient_funds_count: usize,
    ) -> DebtCollectionResults {
        DebtCollectionResults {
            collection_results: vec![],
            dz_epoch,
            successful_transactions_count,
            insufficient_funds_count,
            already_paid_count: 0,
            total_debt,
            total_paid,
            already_paid: 0,
            total_validators,
        }
    }

    #[test]
    fn test_is_row_visible_with_activity() {
        let dcr = make_result(1, 10, 5, 1000, 500, 0);
        assert!(is_row_visible(&dcr));
    }

    #[test]
    fn test_is_row_visible_no_validators() {
        let dcr = make_result(1, 0, 0, 0, 0, 0);
        assert!(!is_row_visible(&dcr));
    }

    #[test]
    fn test_is_row_visible_no_successful_transactions() {
        let dcr = make_result(1, 10, 0, 1000, 0, 5);
        assert!(!is_row_visible(&dcr));
    }

    #[test]
    fn test_visible_rows_filters_correctly() {
        let results = vec![
            make_result(1, 10, 5, 1000, 500, 0), // visible
            make_result(2, 0, 0, 0, 0, 0),       // hidden: no validators
            make_result(3, 5, 0, 500, 0, 2),     // hidden: no successful tx
            make_result(4, 8, 3, 800, 300, 1),   // visible
        ];

        let visible = visible_rows(&results);
        assert_eq!(visible.len(), 2);
        assert_eq!(visible[0].dz_epoch, 1);
        assert_eq!(visible[1].dz_epoch, 4);
    }

    #[test]
    fn test_compute_visible_summary_filters_correctly() {
        let results = vec![
            make_result(1, 10, 5, 1_000_000_000, 500_000_000, 1),
            make_result(2, 0, 0, 2_000_000_000, 0, 0), // hidden
            make_result(3, 5, 0, 3_000_000_000, 0, 10), // hidden
            make_result(4, 8, 3, 800_000_000, 300_000_000, 2),
        ];

        let summary = compute_visible_summary(&results);

        // compute_visible_summary only counts visible epochs (1 and 4)
        assert_eq!(summary.visible_epoch_count, 2);
        assert_eq!(summary.total_debt, 1_000_000_000 + 800_000_000);
        assert_eq!(summary.total_paid, 500_000_000 + 300_000_000);
        assert_eq!(summary.insufficient_funds_count, 1 + 2);
    }

    #[test]
    fn test_compute_summary_sums_all_passed_rows() {
        // compute_summary does NO filtering - it sums whatever is passed in
        let results = [
            make_result(1, 10, 5, 1_000_000_000, 500_000_000, 1),
            make_result(2, 0, 0, 2_000_000_000, 0, 0),
            make_result(3, 5, 0, 3_000_000_000, 0, 10),
            make_result(4, 8, 3, 800_000_000, 300_000_000, 2),
        ];

        let refs: Vec<&DebtCollectionResults> = results.iter().collect();
        let summary = compute_summary(&refs);

        // All 4 epochs should be summed
        assert_eq!(summary.visible_epoch_count, 4);
        assert_eq!(
            summary.total_debt,
            1_000_000_000 + 2_000_000_000 + 3_000_000_000 + 800_000_000
        );
        assert_eq!(summary.total_paid, 500_000_000 + 300_000_000);
        assert_eq!(summary.insufficient_funds_count, 13); // 1 + 0 + 10 + 2
    }

    #[test]
    fn test_summary_outstanding_calculation() {
        let results = vec![make_result(1, 10, 5, 1000, 400, 0)];
        let summary = compute_visible_summary(&results);

        assert_eq!(summary.total_outstanding(), 600);
    }

    #[test]
    fn test_summary_percentage_paid() {
        let results = vec![make_result(1, 10, 5, 1000, 250, 0)];
        let summary = compute_visible_summary(&results);

        assert!((summary.percentage_paid() - 0.25).abs() < 0.0001);
    }

    #[test]
    fn test_summary_percentage_paid_zero_debt() {
        let results = vec![make_result(1, 10, 5, 0, 0, 0)];
        let summary = compute_visible_summary(&results);

        assert_eq!(summary.percentage_paid(), 0.0);
    }

    #[test]
    fn test_empty_results() {
        let results: Vec<DebtCollectionResults> = vec![];
        let summary = compute_visible_summary(&results);

        assert_eq!(summary.visible_epoch_count, 0);
        assert_eq!(summary.total_debt, 0);
        assert_eq!(summary.total_paid, 0);
    }
}
