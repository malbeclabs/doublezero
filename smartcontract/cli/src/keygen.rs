use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_sdk::{
    create_new_pubkey_user, default_keypair_path,
    keypair::{
        encode_keypair_token, is_keypair_json_content, load_keypair, parse_keypair_json,
        parse_keypair_token,
    },
    write_keypair_file,
};
use eyre::WrapErr;
use solana_sdk::signer::Signer;
use std::{
    io::{self, IsTerminal, Write},
    path::PathBuf,
};

#[derive(Args, Debug)]
pub struct KeyGenCliCommand {
    /// Force keypair generation
    #[arg(short, default_value = "false")]
    force: bool,
    /// Path to generated file
    #[arg(short, long)]
    outfile: Option<PathBuf>,
    /// Import a DZ_ token or keypair JSON array instead of generating a keypair. Reads it from a hidden prompt, or from stdin when piped
    #[arg(long)]
    import: bool,
    /// Print the keypair that the CLI signs with as a DZ_ token
    #[arg(long, conflicts_with_all = ["import", "outfile", "force"])]
    export: bool,
}

/// Read a `DZ_` token or keypair JSON array from a hidden prompt, or from stdin when it is not a terminal.
pub fn read_token_from_stdin() -> io::Result<String> {
    let stdin = io::stdin();
    if !stdin.is_terminal() {
        return io::read_to_string(stdin);
    }
    let term = console::Term::stderr();
    term.write_str("Paste your DZ_ token or keypair JSON (input is hidden): ")?;
    term.read_secure_line()
}

impl KeyGenCliCommand {
    /// `keypair_flag` is the raw `--keypair` flag, which `ctx` does not keep apart from the config path.
    pub async fn execute<W: Write>(
        self,
        ctx: &CliContext,
        keypair_flag: Option<PathBuf>,
        out: &mut W,
        read_token: impl FnOnce() -> io::Result<String>,
    ) -> eyre::Result<()> {
        if self.export {
            let default_path = ctx
                .keypair_path
                .clone()
                .unwrap_or_else(default_keypair_path);
            let keypair = load_keypair(keypair_flag, None, default_path)?.keypair;
            writeln!(out, "{}", encode_keypair_token(&keypair))?;
            return Ok(());
        }

        if self.import {
            let input = read_token()?;
            if input.trim().is_empty() {
                eyre::bail!("no DZ_ token or keypair JSON given");
            }
            let keypair = if is_keypair_json_content(&input) {
                parse_keypair_json(input.trim(), "--import input")
            } else {
                parse_keypair_token(&input)
            }
            .wrap_err("expected a DZ_ token or a JSON array of 64 bytes")?;
            write_keypair_file(&keypair, self.force, self.outfile.or(keypair_flag))?;
            writeln!(out, "Pubkey: {}", keypair.pubkey())?;
            return Ok(());
        }

        match create_new_pubkey_user(self.force, self.outfile) {
            Ok(keypair) => {
                writeln!(out, "Pubkey: {}", keypair.pubkey())?;
            }
            Err(e) => {
                writeln!(out, "Error: {e}")?;
            }
        };

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::tests::utils::create_test_context;

    // Throwaway key; Edge Connect's installer decodes this token to the same bytes.
    const TOKEN: &str =
        "DZ_QLPKFhPTFZs1f012TQsJSxt_qg8coXXsRbt9c_QVj-PLE0Oymc4pe_ExKBHNv5wHw6lRoBKhbFu1gPgM-p-_VA";
    const PUBKEY: &str = "EfioQw2a4kh4FYVKCHnbj8MTPu6VHEDu2LY2Ad1YhCh9";

    fn import(token: &str, outfile: &std::path::Path) -> (eyre::Result<()>, String) {
        let cmd = KeyGenCliCommand {
            force: false,
            outfile: Some(outfile.to_path_buf()),
            import: true,
            export: false,
        };
        let mut out = Vec::new();
        let token = token.to_string();
        let res = futures::executor::block_on(cmd.execute(
            &create_test_context(),
            None,
            &mut out,
            || Ok(token),
        ));
        (res, String::from_utf8(out).unwrap())
    }

    #[test]
    #[serial_test::serial]
    fn test_keygen_import_writes_to_keypair_flag_over_config() {
        let tmp = tempfile::TempDir::new().unwrap();
        let config_path = tmp.path().join("config.yml");
        let configured = tmp.path().join("configured.json");
        let flagged = tmp.path().join("flagged.json");

        temp_env::with_var("DOUBLEZERO_CONFIG_FILE", Some(&config_path), || {
            doublezero_sdk::write_doublezero_config(&doublezero_sdk::ClientConfig {
                keypair_path: configured.clone(),
                ..Default::default()
            })
            .unwrap();
            let cmd = KeyGenCliCommand {
                force: false,
                outfile: None,
                import: true,
                export: false,
            };
            futures::executor::block_on(cmd.execute(
                &create_test_context(),
                Some(flagged.clone()),
                &mut Vec::new(),
                || Ok(TOKEN.to_string()),
            ))
            .unwrap();
        });

        assert!(flagged.exists());
        assert!(!configured.exists());
    }

    #[test]
    fn test_keygen_export_prints_token_of_keypair_flag() {
        let tmp = tempfile::TempDir::new().unwrap();
        let path = tmp.path().join("id.json");
        import(TOKEN, &path).0.unwrap();
        let cmd = KeyGenCliCommand {
            force: false,
            outfile: None,
            import: false,
            export: true,
        };

        let mut out = Vec::new();
        futures::executor::block_on(cmd.execute(
            &create_test_context(),
            Some(path),
            &mut out,
            || unreachable!(),
        ))
        .unwrap();

        assert_eq!(String::from_utf8(out).unwrap(), format!("{TOKEN}\n"));
    }

    #[test]
    fn test_keygen_import_writes_token_keypair() {
        let tmp = tempfile::TempDir::new().unwrap();
        let path = tmp.path().join("id.json");

        let (res, out) = import(&format!("{TOKEN}\n"), &path);

        res.unwrap();
        assert_eq!(out, format!("Pubkey: {PUBKEY}\n"));
        let bytes: Vec<u8> =
            serde_json::from_str(&std::fs::read_to_string(&path).unwrap()).unwrap();
        let keypair = solana_sdk::signature::Keypair::try_from(bytes.as_slice()).unwrap();
        assert_eq!(keypair.pubkey().to_string(), PUBKEY);
    }

    fn token_bytes() -> Vec<u16> {
        let keypair = parse_keypair_token(TOKEN).unwrap();
        keypair.to_bytes().iter().map(|&b| b.into()).collect()
    }

    fn json(bytes: &[u16]) -> String {
        serde_json::to_string(bytes).unwrap()
    }

    #[test]
    fn test_keygen_import_writes_json_keypair() {
        let json = json(&token_bytes());
        for input in [json.clone(), format!("{json}\n"), format!("{json} \t\n")] {
            let tmp = tempfile::TempDir::new().unwrap();
            let path = tmp.path().join("id.json");

            let (res, out) = import(&input, &path);

            res.unwrap();
            assert_eq!(out, format!("Pubkey: {PUBKEY}\n"), "{input:?}");
            assert!(path.exists(), "{input:?}");
        }
    }

    #[test]
    fn test_keygen_import_rejects_bad_input_without_writing() {
        let tmp = tempfile::TempDir::new().unwrap();
        let path = tmp.path().join("id.json");
        let short = json(&token_bytes()[..63]);
        let mut over_255 = token_bytes();
        over_255[0] = 256;
        let over_255 = json(&over_255);

        for bad in ["", "\n", &TOKEN[..TOKEN.len() - 4], &short, &over_255] {
            let (res, _) = import(bad, &path);
            assert!(res.is_err(), "{bad:?}");
            assert!(!path.exists(), "{bad:?}");
        }
    }
}
