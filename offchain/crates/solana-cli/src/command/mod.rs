mod passport;
mod revenue_distribution;

//

use anyhow::Result;
use clap::Subcommand;

#[derive(Debug, Subcommand)]
pub enum DoubleZeroSolanaCommand {
    /// Passport program commands.
    Passport(passport::PassportCommand),

    /// Revenue distribution program commands.
    RevenueDistribution(revenue_distribution::RevenueDistributionCommand),
}

impl DoubleZeroSolanaCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self {
            Self::Passport(passport) => passport.command.try_into_execute().await,
            Self::RevenueDistribution(revenue_distribution) => {
                revenue_distribution.command.try_into_execute().await
            }
        }
    }
}

fn try_prompt_proceed_confirmation(prompt_message: String, abort_message: String) -> Result<()> {
    loop {
        println!("⚠️  {prompt_message}. Proceed? [y/N]");

        let mut input = String::new();
        std::io::stdin().read_line(&mut input).unwrap();

        let first_char = input
            .trim()
            .chars()
            .next()
            .map(|c| c.to_lowercase().next().unwrap());

        match first_char {
            Some('y') => return Ok(()),
            Some('n') | None => anyhow::bail!("{abort_message}"),
            _ => {
                println!("Invalid input. Please enter 'y' for yes or 'n' for no.");
                continue;
            }
        }
    }
}
