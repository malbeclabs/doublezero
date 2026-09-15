use clap::{Args, Subcommand};

use crate::feed::{
    activate::*, create::*, delete::*, finalize_retirement::*, halt::*, list::*, resume::*,
    retire::*, update::*,
};

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
    /// Admit a feed that was waiting on a conformance verdict
    Activate(ActivateFeedCliCommand),
    /// Stop a feed publishing
    Halt(HaltFeedCliCommand),
    /// Put a halted feed back to publishing
    Resume(ResumeFeedCliCommand),
    /// Start a feed's retirement notice, thirty days once it has published
    Retire(RetireFeedCliCommand),
    /// End a retirement once its notice has elapsed
    FinalizeRetirement(FinalizeFeedRetirementCliCommand),
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
