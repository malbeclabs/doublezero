//! Program entrypoint

#![allow(unexpected_cfgs)]
#![cfg(not(feature = "no-entrypoint"))]

use crate::processor::process_instruction;

solana_program::entrypoint!(process_instruction);
