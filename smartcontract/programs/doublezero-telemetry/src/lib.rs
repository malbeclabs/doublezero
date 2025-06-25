#![allow(unexpected_cfgs)]

#[cfg(any(not(feature = "no-entrypoint"), test))]
mod entrypoint;
mod helper;

pub mod error;
pub mod instructions;
pub mod pda;
pub mod processors;
pub mod seeds;
pub mod state;
pub mod tests;
