#![allow(unexpected_cfgs)]

pub mod addresses;
pub mod authorize;
pub mod entrypoint;
pub mod error;
// Public so off-chain consumers can apply the program's own address predicates rather than a
// second copy of them: the RFC-27 IP verification service must refuse to sign any address
// `helper::is_global` would make the program reject.
pub mod helper;
pub mod id_allocator;
pub mod instructions;
pub mod ip_allocator;
pub mod ip_proof;
mod min_version;
pub mod pda;
pub mod processors;
pub mod programversion;
pub mod resource;
pub mod seeds;
mod serializer;
pub mod state;
