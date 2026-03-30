use crate::validator_metadata_reader::ValidatorRecord;

/// Filter parameters for the find command.
pub struct FindFilters {
    pub min_stake: Option<f64>,
    pub max_stake: Option<f64>,
    pub client: Option<String>,
    pub is_publisher: bool,
    pub not_publisher: bool,
}

/// Apply filters to a validator record.
pub fn apply_filters(filters: &FindFilters, val: &ValidatorRecord, is_pub: bool) -> bool {
    if let Some(min) = filters.min_stake {
        if val.activated_stake_sol < min {
            return false;
        }
    }
    if let Some(max) = filters.max_stake {
        if val.activated_stake_sol > max {
            return false;
        }
    }
    if let Some(ref client_filter) = filters.client {
        if !val
            .software_client
            .to_lowercase()
            .contains(&client_filter.to_lowercase())
        {
            return false;
        }
    }
    if filters.is_publisher && !is_pub {
        return false;
    }
    if filters.not_publisher && is_pub {
        return false;
    }
    true
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use std::net::Ipv4Addr;

    use super::*;

    fn make_validator(ip: Ipv4Addr, stake: f64, client: &str) -> ValidatorRecord {
        ValidatorRecord {
            vote_account: String::new(),
            software_client: client.to_string(),
            software_version: String::new(),
            activated_stake_sol: stake,
            gossip_ip: ip,
        }
    }

    fn base_filters() -> FindFilters {
        FindFilters {
            min_stake: None,
            max_stake: None,
            client: None,
            is_publisher: false,
            not_publisher: false,
        }
    }

    #[test]
    fn filter_min_stake() {
        let val = make_validator(Ipv4Addr::new(1, 2, 3, 4), 500.0, "agave");
        let filters = FindFilters {
            min_stake: Some(1000.0),
            ..base_filters()
        };
        assert!(!apply_filters(&filters, &val, false));

        let filters = FindFilters {
            min_stake: Some(100.0),
            ..base_filters()
        };
        assert!(apply_filters(&filters, &val, false));
    }

    #[test]
    fn filter_max_stake() {
        let val = make_validator(Ipv4Addr::new(1, 2, 3, 4), 1500.0, "agave");
        let filters = FindFilters {
            max_stake: Some(1000.0),
            ..base_filters()
        };
        assert!(!apply_filters(&filters, &val, false));

        let filters = FindFilters {
            max_stake: Some(2000.0),
            ..base_filters()
        };
        assert!(apply_filters(&filters, &val, false));
    }

    #[test]
    fn filter_client_case_insensitive() {
        let val = make_validator(Ipv4Addr::new(1, 2, 3, 4), 1000.0, "Jito-Solana");

        let filters = FindFilters {
            client: Some("jito".to_string()),
            ..base_filters()
        };
        assert!(apply_filters(&filters, &val, false));

        let filters = FindFilters {
            client: Some("agave".to_string()),
            ..base_filters()
        };
        assert!(!apply_filters(&filters, &val, false));
    }

    #[test]
    fn filter_is_publisher() {
        let val = make_validator(Ipv4Addr::new(1, 2, 3, 4), 1000.0, "agave");

        let filters = FindFilters {
            is_publisher: true,
            ..base_filters()
        };
        assert!(!apply_filters(&filters, &val, false));
        assert!(apply_filters(&filters, &val, true));
    }

    #[test]
    fn filter_not_publisher() {
        let val = make_validator(Ipv4Addr::new(1, 2, 3, 4), 1000.0, "agave");

        let filters = FindFilters {
            not_publisher: true,
            ..base_filters()
        };
        assert!(apply_filters(&filters, &val, false));
        assert!(!apply_filters(&filters, &val, true));
    }

    #[test]
    fn combined_filters() {
        let val = make_validator(Ipv4Addr::new(1, 2, 3, 4), 1500.0, "Jito-Solana");

        // Passes all: stake in range, client matches, is publisher
        let filters = FindFilters {
            min_stake: Some(1000.0),
            max_stake: Some(2000.0),
            client: Some("jito".to_string()),
            is_publisher: true,
            not_publisher: false,
        };
        assert!(apply_filters(&filters, &val, true));

        // Fails: not a publisher but is_publisher required
        assert!(!apply_filters(&filters, &val, false));
    }
}
