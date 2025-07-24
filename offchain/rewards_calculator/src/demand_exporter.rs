use anyhow::Result;
use csv::Writer;
use demand_generator::{
    generator::DemandGenerator, settings::Settings as DemandSettings, types::EnrichedValidator,
};
use network_shapley::types::Demand;
use std::path::Path;
use tracing::info;

pub async fn export_demand_data(
    demand_path: &Path,
    enriched_validators_path: Option<&Path>,
) -> Result<()> {
    // Create DemandGenerator with settings
    let demand_settings = DemandSettings::from_env()?;
    let generator = DemandGenerator::new(demand_settings);

    // Generate demands and enriched validators
    let (enriched_validators, demands) = generator.generate_with_validators().await?;

    // Write demand CSV
    write_demand_csv(demand_path, &demands)?;
    info!(
        "Wrote {} demand entries to: {}",
        demands.len(),
        demand_path.display()
    );

    // Write enriched validators CSV if path provided
    if let Some(validators_path) = enriched_validators_path {
        write_enriched_validators_csv(validators_path, &enriched_validators)?;
        info!(
            "Wrote {} enriched validators to: {}",
            enriched_validators.len(),
            validators_path.display()
        );
    }

    Ok(())
}

fn write_demand_csv(path: &Path, demands: &[Demand]) -> Result<()> {
    let mut wtr = Writer::from_path(path)?;
    for demand in demands {
        wtr.serialize(demand)?;
    }
    wtr.flush()?;
    Ok(())
}

fn write_enriched_validators_csv(path: &Path, validators: &[EnrichedValidator]) -> Result<()> {
    let mut wtr = Writer::from_path(path)?;
    for validator in validators {
        wtr.serialize(validator)?;
    }
    wtr.flush()?;
    Ok(())
}
