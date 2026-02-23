#![allow(unexpected_cfgs)]

#[cfg(any(not(feature = "no-entrypoint"), test))]
pub mod entrypoint;

pub mod error;
pub mod instructions;
pub mod pda;
pub mod processors;
pub mod seeds;
mod serializer;
pub mod state;
