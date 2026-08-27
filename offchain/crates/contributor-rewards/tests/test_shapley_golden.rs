mod common;

use std::{
    collections::{BTreeMap, HashMap},
    fs,
    path::{Path, PathBuf},
};

use anyhow::{Context, Result};
use common::create_test_settings;
use doublezero_contributor_rewards::{
    calculator::{data_prep::PreparedData, shapley::evaluator::compute_shapley_values},
    cli::snapshot::CompleteSnapshot,
    settings::network::Network,
};
use serde::{Deserialize, Serialize};

// Exact float equality is avoided on purpose. The computation is deterministic on
// one machine, but bit-identical floating point across architectures is not
// guaranteed, so a golden generated on arm64 could differ in the last bit from
// x86_64 CI. A dependency change that alters reward math moves values far more
// than this tolerance. A last-bit difference does not.
const RELATIVE_TOLERANCE: f64 = 1e-12;

// Values below this carry no reward information. They are cancellation noise from
// the Shapley computation, and pinning them at a 1e-12 absolute gate would make the
// test fail on floating point differences that mean nothing. A real regression moves
// these entries to the hundreds, not to 1e-12.
const NEGLIGIBLE: f64 = 1e-6;

const FIXTURE: &str = "tests/goldens/mainnet-beta-epoch-129-trimmed.json";

fn assert_close(label: &str, actual: f64, expected: f64) {
    if actual.abs() < NEGLIGIBLE && expected.abs() < NEGLIGIBLE {
        return;
    }
    let difference = (actual - expected).abs();
    let scale = expected.abs().max(actual.abs()).max(1.0);
    assert!(
        difference / scale <= RELATIVE_TOLERANCE,
        "{label}: {actual} differs from golden {expected} by {difference} (relative {})",
        difference / scale
    );
}

fn golden_path(name: &str) -> PathBuf {
    Path::new("tests/goldens").join(name)
}

// Set UPDATE_GOLDEN=1 to rewrite the golden files instead of comparing against them.
fn regenerating() -> bool {
    std::env::var("UPDATE_GOLDEN").as_deref() == Ok("1")
}

/// Run the committed fixture through the production snapshot path.
///
/// `PreparedData::from_snapshot` is what `Orchestrator::calculate_rewards` calls for
/// the snapshot case, so this exercises the real assembly rather than a copy of it.
/// Everything past `compute_shapley_values` is async and reads from RPC, so this is
/// the deepest point a test can reach offline.
fn compute_from_fixture()
-> Result<doublezero_contributor_rewards::calculator::shapley::evaluator::ShapleyComputeResult> {
    // create_test_settings hardcodes Testnet. The fixture is mainnet-beta, and
    // build_devices derives city codes on a different branch per network, so
    // leaving this as Testnet mis-maps every city.
    let mut settings = create_test_settings(0.7, 1000.0, false);
    settings.network = Network::MainnetBeta;

    let snapshot = CompleteSnapshot::load_from_file(Path::new(FIXTURE))
        .with_context(|| format!("loading {FIXTURE}"))?;
    let prepared = PreparedData::from_snapshot(&snapshot, &settings, true)?;
    let inputs = prepared
        .shapley_inputs
        .context("from_snapshot returned no shapley inputs despite require_shapley")?;
    compute_shapley_values(&inputs, &settings.shapley, &HashMap::new())
}

#[derive(Debug, Serialize, Deserialize)]
struct GoldenOperator {
    operator: String,
    value: f64,
    proportion: f64,
}

#[derive(Debug, Serialize, Deserialize)]
struct AggregatedGolden {
    operator_count: usize,
    // Ordered as `aggregated_output` iterates, which is BTreeMap order.
    operators: Vec<GoldenOperator>,
}

#[test]
fn test_aggregated_shapley_output_matches_golden() -> Result<()> {
    let result = compute_from_fixture()?;

    let operators = result
        .aggregated_output
        .iter()
        .map(|(operator, aggregated)| GoldenOperator {
            operator: operator.clone(),
            value: aggregated.value,
            proportion: aggregated.proportion,
        })
        .collect::<Vec<_>>();
    let actual = AggregatedGolden {
        operator_count: operators.len(),
        operators,
    };

    let path = golden_path("shapley-mainnet-beta-epoch-129.json");

    if regenerating() {
        fs::write(
            &path,
            format!("{}\n", serde_json::to_string_pretty(&actual)?),
        )?;
        eprintln!("wrote golden {}", path.display());
        return Ok(());
    }

    let golden_json = fs::read_to_string(&path).with_context(|| {
        format!(
            "reading {}. Generate it with UPDATE_GOLDEN=1",
            path.display()
        )
    })?;
    let expected = serde_json::from_str::<AggregatedGolden>(&golden_json)?;

    // Structure is asserted exactly. Only the numbers get a tolerance.
    assert_eq!(
        actual.operator_count, expected.operator_count,
        "operator count changed"
    );
    let actual_operators = actual
        .operators
        .iter()
        .map(|entry| entry.operator.as_str())
        .collect::<Vec<_>>();
    let expected_operators = expected
        .operators
        .iter()
        .map(|entry| entry.operator.as_str())
        .collect::<Vec<_>>();
    assert_eq!(
        actual_operators, expected_operators,
        "operator set or ordering changed"
    );

    for (actual_entry, expected_entry) in actual.operators.iter().zip(expected.operators.iter()) {
        assert_close(
            &format!("{} value", actual_entry.operator),
            actual_entry.value,
            expected_entry.value,
        );
        assert_close(
            &format!("{} proportion", actual_entry.operator),
            actual_entry.proportion,
            expected_entry.proportion,
        );
    }
    Ok(())
}

#[derive(Debug, Serialize, Deserialize)]
struct PerCityGolden {
    city_count: usize,
    // city -> [(operator, value)], in BTreeMap and Vec order as produced.
    cities: BTreeMap<String, Vec<(String, f64)>>,
}

#[test]
fn test_per_city_shapley_output_matches_golden() -> Result<()> {
    let result = compute_from_fixture()?;

    let actual = PerCityGolden {
        city_count: result.per_city_outputs.len(),
        cities: result.per_city_outputs.clone(),
    };

    let path = golden_path("shapley-per-city-mainnet-beta-epoch-129.json");

    if regenerating() {
        fs::write(
            &path,
            format!("{}\n", serde_json::to_string_pretty(&actual)?),
        )?;
        eprintln!("wrote golden {}", path.display());
        return Ok(());
    }

    let golden_json = fs::read_to_string(&path).with_context(|| {
        format!(
            "reading {}. Generate it with UPDATE_GOLDEN=1",
            path.display()
        )
    })?;
    let expected = serde_json::from_str::<PerCityGolden>(&golden_json)?;

    assert_eq!(actual.city_count, expected.city_count, "city count changed");

    let actual_cities = actual.cities.keys().collect::<Vec<_>>();
    let expected_cities = expected.cities.keys().collect::<Vec<_>>();
    assert_eq!(
        actual_cities, expected_cities,
        "city set or ordering changed"
    );

    for (city, actual_values) in actual.cities.iter() {
        let expected_values = expected
            .cities
            .get(city)
            .with_context(|| format!("city {city} missing from golden"))?;
        let actual_operators = actual_values
            .iter()
            .map(|(operator, _)| operator)
            .collect::<Vec<_>>();
        let expected_operators = expected_values
            .iter()
            .map(|(operator, _)| operator)
            .collect::<Vec<_>>();
        assert_eq!(
            actual_operators, expected_operators,
            "{city}: operator set or ordering changed"
        );
        for ((operator, actual_value), (_, expected_value)) in
            actual_values.iter().zip(expected_values.iter())
        {
            assert_close(
                &format!("{city}/{operator}"),
                *actual_value,
                *expected_value,
            );
        }
    }
    Ok(())
}
