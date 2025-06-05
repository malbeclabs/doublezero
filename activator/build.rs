use std::{env, fs, path::Path};

fn main() {
    // Get the version from an environment variable (or use a default value)
    let version = env::var("VERSION").unwrap_or_else(|_| "unknown".to_string());

    // Genera un archivo con una constante para la versión
    let out_dir = env::var("OUT_DIR").unwrap();
    let dest_path = Path::new(&out_dir).join("version.rs");
    fs::write(
        dest_path,
        format!(
            r#"pub const APP_VERSION: &str = "{}";
pub const APP_LONG_VERSION: &str = "version: {}\n";"#,
            version, version
        ),
    )
    .unwrap();
}
