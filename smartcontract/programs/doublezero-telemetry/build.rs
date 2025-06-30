use std::{env, fs, path::Path};

fn main() {
    let out_dir = env::var("OUT_DIR").unwrap();
    let dest = Path::new(&out_dir).join("serviceability_program_id_input.rs");

    let value = env::var("SERVICEABILITY_PROGRAM_ID")
        .expect("SERVICEABILITY_PROGRAM_ID must be set to 'devnet', 'testnet', or base58 Pubkey");

    fs::write(
        dest,
        format!(r#"pub const RAW_SERVICEABILITY_PROGRAM_ID: &str = "{value}";"#),
    )
    .unwrap();
}
