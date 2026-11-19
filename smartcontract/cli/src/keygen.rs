use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_sdk::{create_new_pubkey_user, keypair::parse_keypair_token, write_keypair_file};
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
    /// Import a DZ_ token instead of generating a keypair. Reads it from a hidden prompt, or from stdin when piped
    #[arg(long)]
    import: bool,
}

/// Read a `DZ_` token from a hidden prompt, or from stdin when it is not a terminal.
pub fn read_token_from_stdin() -> io::Result<String> {
    let stdin = io::stdin();
    if !stdin.is_terminal() {
        return io::read_to_string(stdin);
    }
    let term = console::Term::stderr();
    term.write_str("Paste your DZ_ token (input is hidden): ")?;
    term.read_secure_line()
}

impl KeyGenCliCommand {
    pub async fn execute<W: Write>(
        self,
        _ctx: &CliContext,
        out: &mut W,
        read_token: impl FnOnce() -> io::Result<String>,
    ) -> eyre::Result<()> {
        if self.import {
            let token = read_token()?;
            if token.trim().is_empty() {
                eyre::bail!("no DZ_ token given");
            }
            let keypair = parse_keypair_token(&token)?;
            write_keypair_file(&keypair, self.force, self.outfile)?;
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
        };
        let mut out = Vec::new();
        let token = token.to_string();
        let res =
            futures::executor::block_on(
                cmd.execute(&create_test_context(), &mut out, || Ok(token)),
            );
        (res, String::from_utf8(out).unwrap())
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

    #[test]
    fn test_keygen_import_rejects_bad_input_without_writing() {
        let tmp = tempfile::TempDir::new().unwrap();
        let path = tmp.path().join("id.json");

        for bad in ["", "\n", &TOKEN[..TOKEN.len() - 4]] {
            let (res, _) = import(bad, &path);
            assert!(res.is_err(), "{bad:?}");
            assert!(!path.exists(), "{bad:?}");
        }
    }
}
