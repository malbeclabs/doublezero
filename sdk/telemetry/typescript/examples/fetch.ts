#!/usr/bin/env bun
/**
 * Example CLI that fetches and displays telemetry data.
 *
 * Uses the serviceability SDK to discover devices and links,
 * then fetches telemetry samples for each link.
 */

import { PublicKey } from "@solana/web3.js";
import { Client as ServiceabilityClient } from "@doublezero/serviceability";
import { Client as TelemetryClient } from "../telemetry/client.js";
import { LEDGER_RPC_URLS } from "../telemetry/config.js";
import { newConnection } from "../telemetry/rpc.js";

async function main() {
  const args = process.argv.slice(2);
  let env = "mainnet-beta";
  let epoch = 0;

  for (let i = 0; i < args.length; i++) {
    switch (args[i]) {
      case "--env":
        env = args[++i];
        break;
      case "--epoch":
        epoch = parseInt(args[++i], 10);
        break;
    }
  }

  const validEnvs = ["mainnet-beta", "testnet", "devnet", "localnet"];
  if (!validEnvs.includes(env)) {
    console.error(`Invalid environment: ${env}`);
    process.exit(1);
  }

  console.log(`Fetching telemetry data from ${env}...\n`);

  // First, get serviceability data to discover devices and links
  const svcClient = ServiceabilityClient.forEnv(env);
  const svcData = await svcClient.getProgramData();

  console.log("=== Network Overview ===");
  console.log(`Devices: ${svcData.devices.length}`);
  console.log(`Links:   ${svcData.links.length}`);
  console.log();

  if (svcData.links.length === 0) {
    console.log("No links found - no telemetry data to fetch.");
    return;
  }

  // Create telemetry client
  const telClient = TelemetryClient.forEnv(env);

  // Determine which epoch to use
  let targetEpoch = epoch;
  if (targetEpoch === 0) {
    // Get current epoch from DZ Ledger RPC
    const conn = newConnection(LEDGER_RPC_URLS[env]);
    const epochInfo = await conn.getEpochInfo();
    targetEpoch = epochInfo.epoch;
  }

  console.log(`=== Device Latency Samples (epoch ${targetEpoch}) ===`);

  let samplesFound = 0;
  for (const link of svcData.links) {
    const sideAPK = link.sideAPubKey;
    const sideZPK = link.sideZPubKey;
    const linkPK = link.owner; // Using owner as link pubkey proxy

    // Find device codes
    let sideACode = "unknown";
    let sideZCode = "unknown";
    for (const dev of svcData.devices) {
      if (dev.owner.equals(sideAPK)) {
        sideACode = dev.code;
      }
      if (dev.owner.equals(sideZPK)) {
        sideZCode = dev.code;
      }
    }

    // Try both directions
    const directions: [PublicKey, PublicKey, string, string][] = [
      [sideAPK, sideZPK, sideACode, sideZCode],
      [sideZPK, sideAPK, sideZCode, sideACode],
    ];

    for (const [originPK, targetPK, oCode, tCode] of directions) {
      try {
        const samples = await telClient.getDeviceLatencySamples(
          originPK,
          targetPK,
          linkPK,
          targetEpoch,
        );

        samplesFound++;
        const sampleCount = samples.samples.length;

        if (sampleCount === 0) {
          console.log(
            `  ${oCode} -> ${tCode} (${link.code}): initialized, no samples yet`,
          );
          continue;
        }

        // Calculate stats
        const total = samples.samples.reduce((a, b) => a + b, 0);
        const minVal = Math.min(...samples.samples);
        const maxVal = Math.max(...samples.samples);
        const avgUs = total / sampleCount;
        const avgMs = avgUs / 1000;
        const minMs = minVal / 1000;
        const maxMs = maxVal / 1000;

        console.log(
          `  ${oCode} -> ${tCode} (${link.code}): ${sampleCount} samples, ` +
            `avg ${avgMs.toFixed(2)}ms, min ${minMs.toFixed(2)}ms, max ${maxMs.toFixed(2)}ms`,
        );
      } catch {
        // Account likely doesn't exist for this epoch
        continue;
      }
    }
  }

  if (samplesFound === 0) {
    console.log(
      `  No samples found for epoch ${targetEpoch}. Try a different epoch with --epoch flag.`,
    );
  }

  console.log();
  console.log("Done.");
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
