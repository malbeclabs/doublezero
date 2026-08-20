//! The RFC-27 IP ownership verification service.
//!
//! A stateless HTTP signer: it observes the source address of a request and returns an
//! `IpOwnershipProof` binding that address to the requesting payer for the current DoubleZero
//! epoch. The serviceability program validates the proof before it will bind a `client_ip` to a
//! user, so this service is the only party that can attest an address — which makes its answer to
//! "which address did I actually see?" the entire security property. That question lives in
//! [`client_ip`].

pub mod client_ip;
pub mod epoch;
pub mod rate_limit;
pub mod server;
pub mod settings;
