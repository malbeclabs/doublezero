use clap::Parser;
use std::path::PathBuf;

#[derive(Parser, Debug)]
#[command(author, version, about, long_about = None)]
pub struct Cli {
    /// Output enriched validators CSV to the specified path
    #[arg(long)]
    pub enriched_validators: Option<PathBuf>,

    /// Output demand matrix CSV to the specified path
    #[arg(long)]
    pub demand: Option<PathBuf>,
}
