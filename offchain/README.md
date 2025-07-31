# DoubleZero Rewarder

An off-chain rewards calculation system for the DoubleZero network that uses Shapley values to ensure fair distribution of rewards based on network contributions.

## Overview

The DoubleZero Rewarder is a Rust-based system that:

- Fetches on-chain serviceability and telemetry data from Solana
- Processes network performance metrics (latency, jitter, packet loss)
- Calculates fair reward distributions using Shapley values
- Generates a Merkle root for on-chain verification

This ensures that network participants are rewarded proportionally to their actual contribution to network performance and reliability.
