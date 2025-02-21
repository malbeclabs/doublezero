use std::env;
use std::fs;
use std::path::Path;

fn main() {
    // Obtén la versión desde una variable de entorno (o usa un valor predeterminado)
    let version = env::var("VERSION").unwrap_or_else(|_| "unknown".to_string());
    let commit = env::var("COMMIT").unwrap_or_else(|_| "unknown".to_string());
    let date = env::var("DATE").unwrap_or_else(|_| "unknown".to_string());
    let os = env::var("OS").unwrap_or_else(|_| "unknown".to_string());
    let arch = env::var("ARCH").unwrap_or_else(|_| "unknown".to_string());

    // Genera un archivo con una constante para la versión
    let out_dir = env::var("OUT_DIR").unwrap();
    let dest_path = Path::new(&out_dir).join("version.rs");
    fs::write(
        dest_path,
        format!(r#"pub const APP_VERSION: &str = "{}";
pub const APP_LONG_VERSION: &str = "version: {}\ncommit: {}\ndate: {}\nos: {}\narch: {}";"#, version, version, commit, date, os, arch),
    )
    .unwrap();
}
