use std::path::Path;

use doublezero_config::Environment;

fn main() {
    let out_dir = std::env::var("OUT_DIR").unwrap();
    let dest = Path::new(&out_dir).join("build_constants.rs");

    // Determine the network environment config with serviceability program ID based on the
    // features through cargo environment variables during build, so that we can validate and print
    // the values being used at build-time.
    // The CARGO_FEATURE_* env variables are set by cargo for build scripts based on the features
    // defined in the Cargo.toml file.
    let env = if std::env::var("CARGO_FEATURE_MAINNET_BETA").is_ok() {
        Environment::MainnetBeta
    } else if std::env::var("CARGO_FEATURE_TESTNET").is_ok() {
        Environment::Testnet
    } else if std::env::var("CARGO_FEATURE_DEVNET").is_ok() {
        Environment::Devnet
    } else {
        Environment::Localnet
    };

    let serviceability_program_id = env.config().serviceability_program_id.to_string();

    println!("cargo:warning=Environment: {}", env.moniker());
    println!(
        "cargo:warning=Serviceability Program ID: {}",
        serviceability_program_id
    );

    std::fs::write(
        dest,
        format!(r#"pub const SERVICEABILITY_PROGRAM_ID: &str = "{serviceability_program_id}";"#),
    )
    .unwrap();
}
