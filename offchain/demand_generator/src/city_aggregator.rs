use crate::{constants::CITY_CODES, types::EnrichedValidator};
use anyhow::Result;
use serde::{Deserialize, Serialize};
use std::{cmp::Ordering, collections::HashMap};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CityAggregate {
    pub city_name: String,
    pub city_code: String,
    pub country: String,
    pub total_stake_sol: f64,
    pub validator_count: u32,
    pub latitude: Option<f64>,
    pub longitude: Option<f64>,
}

/// Aggregates validators by city and generates city codes
pub fn aggregate_by_city(validators: &[EnrichedValidator]) -> Result<Vec<CityAggregate>> {
    let mut city_map: HashMap<String, CityAggregate> = HashMap::new();

    for validator in validators {
        let city_key = format!("{}-{}", validator.city, validator.country);

        let entry = city_map
            .entry(city_key.clone())
            .or_insert_with(|| CityAggregate {
                city_name: validator.city.clone(),
                city_code: generate_city_code(&validator.city, &validator.country),
                country: validator.country.clone(),
                total_stake_sol: 0.0,
                validator_count: 0,
                latitude: validator.latitude,
                longitude: validator.longitude,
            });

        entry.total_stake_sol += validator.stake_sol;
        entry.validator_count += 1;
    }

    let mut aggregates: Vec<CityAggregate> = city_map.into_values().collect();
    // Sort by stake descending
    aggregates.sort_by(|a, b| {
        b.total_stake_sol
            .partial_cmp(&a.total_stake_sol)
            .unwrap_or(Ordering::Equal)
    });

    Ok(aggregates)
}

/// Generates a 3-letter city code
fn generate_city_code(city: &str, country: &str) -> String {
    // Common city code mappings
    let known_codes = HashMap::from(CITY_CODES);

    let city_country = format!("{city}-{country}");
    if let Some(&code) = known_codes.get(city_country.as_str()) {
        return code.to_string();
    }

    // Generate code from city name if not in known list
    let clean_city = city
        .to_uppercase()
        .chars()
        .filter(|c| c.is_alphabetic())
        .collect::<String>();

    if clean_city.len() >= 3 {
        clean_city[..3].to_string()
    } else {
        // Pad with country code if city name is too short
        format!(
            "{}{}",
            clean_city,
            country
                .to_uppercase()
                .chars()
                .take(3 - clean_city.len())
                .collect::<String>()
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_city_code_generation() {
        assert_eq!(generate_city_code("New York", "US"), "NYC");
        assert_eq!(generate_city_code("Singapore", "SG"), "SIN");
        assert_eq!(generate_city_code("Unknown City", "US"), "UNK");
    }
}
