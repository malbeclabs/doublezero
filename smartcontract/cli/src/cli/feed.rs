use clap::{Args, Subcommand};

use crate::feed::{create::*, delete::*, get::*, list::*, update::*};

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
    /// List all feeds
    #[clap()]
    List(ListFeedCliCommand),
    /// Get details for a specific feed
    #[clap()]
    Get(GetFeedCliCommand),
    /// Delete a feed (must have no references)
    #[clap()]
    Delete(DeleteFeedCliCommand),
}
