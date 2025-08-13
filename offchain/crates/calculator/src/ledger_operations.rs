use borsh::BorshSerialize;
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_sdk::signature::Keypair;
use std::fmt;
use tracing::{info, warn};

/// Result of a write operation
#[derive(Debug)]
pub enum WriteResult {
    Success(String),
    Failed(String, String), // (description, error)
}

/// Summary of all ledger writes
#[derive(Debug, Default)]
pub struct WriteSummary {
    pub results: Vec<WriteResult>,
}

impl WriteSummary {
    pub fn add_success(&mut self, description: String) {
        self.results.push(WriteResult::Success(description));
    }

    pub fn add_failure(&mut self, description: String, error: String) {
        self.results.push(WriteResult::Failed(description, error));
    }

    pub fn successful_count(&self) -> usize {
        self.results
            .iter()
            .filter(|r| matches!(r, WriteResult::Success(_)))
            .count()
    }

    pub fn failed_count(&self) -> usize {
        self.results
            .iter()
            .filter(|r| matches!(r, WriteResult::Failed(_, _)))
            .count()
    }

    pub fn total_count(&self) -> usize {
        self.results.len()
    }

    pub fn all_successful(&self) -> bool {
        self.failed_count() == 0
    }
}

impl fmt::Display for WriteSummary {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        writeln!(
            f,
            "
========================================="
        )?;
        writeln!(f, "Ledger Write Summary")?;
        writeln!(f, "=========================================")?;
        writeln!(
            f,
            "Total: {}/{} successful",
            self.successful_count(),
            self.total_count()
        )?;

        if !self.all_successful() {
            writeln!(
                f,
                "
Failed writes:"
            )?;
            for result in &self.results {
                if let WriteResult::Failed(desc, error) = result {
                    writeln!(f, "  ❌ {desc}: {error}")?;
                }
            }
        }

        writeln!(
            f,
            "
All writes:"
        )?;
        for result in &self.results {
            match result {
                WriteResult::Success(desc) => writeln!(f, "  ✅ {desc}")?,
                WriteResult::Failed(desc, _) => writeln!(f, "  ❌ {desc}")?,
            }
        }

        writeln!(f, "=========================================")?;
        Ok(())
    }
}

/// Simple helper to write and track results
pub async fn write_and_track<T: BorshSerialize>(
    rpc_client: &RpcClient,
    payer_signer: &Keypair,
    seeds: &[&[u8]],
    data: &T,
    description: &str,
    summary: &mut WriteSummary,
) {
    match crate::recorder::write_to_ledger(rpc_client, payer_signer, seeds, data, description).await
    {
        Ok(_) => {
            info!("✅ Successfully wrote {}", description);
            summary.add_success(description.to_string());
        }
        Err(e) => {
            warn!("❌ Failed to write {}: {}", description, e);
            summary.add_failure(description.to_string(), e.to_string());
        }
    }
}
