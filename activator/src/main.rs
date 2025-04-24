use clap::Parser;

mod states;
mod idallocator;
mod ipblockallocator;
mod activator;
include!(concat!(env!("OUT_DIR"), "/version.rs"));

#[derive(Parser, Debug)]
#[command(term_width = 0)]
#[command(name = "Doublezero activator")]
#[command(version = "1.0")]
#[command(about = "Double Zero ", long_about = None)]
struct AppArgs {
    #[arg(long)]
    rpc: Option<String>,

    #[arg(long)]
    ws: Option<String>,

    #[arg(long)]
    program_id: Option<String>,

    #[arg(long)]
    keypair: Option<String>,
}



#[tokio::main]
async fn main() -> eyre::Result<()> {

    let args = AppArgs::parse();

    println!("DoubleZero Activator {}", APP_VERSION);
    
    let mut activator = activator::Activator::new(
        args.rpc,
        args.ws,
        args.program_id,
        args.keypair,
    ).await.unwrap();
    
    println!("Activator started");

    activator.init().await?;

    println!("Initialized");

    activator.run()?;

    Ok(())
}
