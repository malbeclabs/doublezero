use std::{env, fs, path::Path};

use doublezero_config::Environment;

fn main() {
    let out_dir = env::var("OUT_DIR").unwrap();
    let dest = Path::new(&out_dir).join("build_constants.rs");

    // Determine the network environment config with serviceability program ID based on the
    // features through cargo environment variables during build, so that we can validate and print
    // the values being used at build-time.
    // The CARGO_FEATURE_* env variables are set by cargo for build scripts based on the features
    // defined in the Cargo.toml file.
    let environment: Option<Environment> = if std::env::var("CARGO_FEATURE_MAINNET_BETA").is_ok() {
        Some(Environment::MainnetBeta)
    } else if std::env::var("CARGO_FEATURE_TESTNET").is_ok() {
        Some(Environment::Testnet)
    } else if std::env::var("CARGO_FEATURE_DEVNET").is_ok() {
        Some(Environment::Devnet)
    } else {
        None
    };

    let (env_code, serviceability_program_id) = match environment {
        Some(environment) => (
            environment.to_string(),
            environment
                .config()
                .unwrap()
                .serviceability_program_id
                .to_string(),
        ),
        None => (
            "localnet".to_string(),
            "7CTniUa88iJKUHTrCkB4TjAoG6TD7AMivhQeuqN2LPtX".to_string(),
        ),
    };

    println!("cargo:warning=Environment: {env_code}");
    println!("cargo:warning=Serviceability Program ID: {serviceability_program_id}");

    fs::write(
        dest,
        format!(r#"pub const SERVICEABILITY_PROGRAM_ID: &str = "{serviceability_program_id}";"#),
    )
    .unwrap();
}
