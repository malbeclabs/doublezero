//! Build script to capture dependency versions at compile time
//!
//! This script extracts the exact git revision of the network-shapley-rs dependency
//! from Cargo.lock and exposes it as an environment variable for use in the binary.

use std::{env, fs, path::Path};

fn main() {
    // Find Cargo.lock relative to the build script
    let manifest_dir = env::var("CARGO_MANIFEST_DIR").unwrap();
    let workspace_root = Path::new(&manifest_dir).parent().unwrap();
    let cargo_lock_path = workspace_root.join("Cargo.lock");

    // Read and parse Cargo.lock
    let lockfile_content = fs::read_to_string(&cargo_lock_path).expect("Failed to read Cargo.lock");
    let lockfile: toml::Value =
        toml::from_str(&lockfile_content).expect("Failed to parse Cargo.lock");

    // Find the shapley package
    let packages = lockfile["package"]
        .as_array()
        .expect("Cargo.lock should contain a 'package' array");

    let shapley_package = packages
        .iter()
        .find(|p| p["name"].as_str() == Some("shapley"))
        .expect("Could not find 'shapley' package in Cargo.lock");

    // Extract the git revision from the source field
    let source = shapley_package["source"]
        .as_str()
        .expect("shapley package should have a 'source' field");

    // The source format is: "git+https://github.com/...#<commit_hash>"
    let shapley_version = source
        .split('#')
        .next_back()
        .expect("Could not extract git revision from shapley source");

    // Expose as environment variable
    println!("cargo:rustc-env=SHAPLEY_VERSION={shapley_version}");

    // Also tell cargo to re-run this script if Cargo.lock changes
    println!("cargo:rerun-if-changed={}", cargo_lock_path.display());
}
