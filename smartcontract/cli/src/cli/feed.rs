use clap::{Args, Subcommand};

use crate::feed::{create::*, delete::*, list::*, update::*};

#[derive(Args, Debug)]
pub struct FeedCliCommand {
    #[command(subcommand)]
    pub command: FeedCommands,
}

#[derive(Debug, Subcommand)]
pub enum FeedCommands {
    /// Create a new feed (a metro's multicast group set)
    #[clap()]
    Create(CreateFeedCliCommand),
    /// Update a feed's name or group set
    #[clap()]
    Update(UpdateFeedCliCommand),
    /// List feeds, optionally narrowed to one code or one metro
    #[clap()]
    List(ListFeedCliCommand),
    /// Delete a feed (must have no references)
    #[clap()]
    Delete(DeleteFeedCliCommand),
}
