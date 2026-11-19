use clap::{Args, Subcommand};

use crate::migrate::{
    feed_count::FeedCountMigrateCliCommand, flex_algo::FlexAlgoMigrateCliCommand,
    user_pda::MigrateUserPdaCliCommand,
};

#[derive(Args, Debug)]
pub struct MigrateCliCommand {
    #[command(subcommand)]
    pub command: MigrateCommands,
}

#[derive(Subcommand, Debug)]
pub enum MigrateCommands {
    /// Migrate user accounts to the new PDA scheme
    UserPda(MigrateUserPdaCliCommand),
    /// Backfill link topologies and Vpnv4 loopback FlexAlgoNodeSegments (RFC-18)
    FlexAlgo(FlexAlgoMigrateCliCommand),
    /// Set each multicast group's feed count to the number of feeds that list it
    FeedCount(FeedCountMigrateCliCommand),
}
