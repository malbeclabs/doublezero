#!/usr/bin/env bun
/**
 * Example CLI that fetches and displays telemetry data.
 *
 * For a full example with device discovery, use the serviceability SDK
 * to fetch devices and links, then use their public keys here.
 *
 * Usage:
 *   bun run examples/fetch.ts --env testnet --origin <pubkey> --target <pubkey> --link <pubkey> --epoch 12345
 *
 * Or run the serviceability example first to discover devices and links.
 */

import { PublicKey } from "@solana/web3.js";
import { Client } from "../telemetry/client.js";

async function main() {
  const args = process.argv.slice(2);
  let env = "mainnet-beta";
  let originPK: string | null = null;
  let targetPK: string | null = null;
  let linkPK: string | null = null;
  let epoch: number | null = null;

  for (let i = 0; i < args.length; i++) {
    switch (args[i]) {
      case "--env":
        env = args[++i];
        break;
      case "--origin":
        originPK = args[++i];
        break;
      case "--target":
        targetPK = args[++i];
        break;
      case "--link":
        linkPK = args[++i];
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

  if (!originPK || !targetPK || !linkPK || epoch === null) {
    console.log("Usage: fetch.ts --env <env> --origin <pubkey> --target <pubkey> --link <pubkey> --epoch <num>");
    console.log();
    console.log("This example requires specific device and link public keys.");
    console.log("Run the serviceability SDK example first to discover available devices and links.");
    console.log();
    console.log("Example (with placeholder values - replace with real pubkeys):");
    console.log("  bun run examples/fetch.ts --env testnet \\");
    console.log("    --origin 11111111111111111111111111111111 \\");
    console.log("    --target 22222222222222222222222222222222 \\");
    console.log("    --link 33333333333333333333333333333333 \\");
    console.log("    --epoch 12345");
    process.exit(0);
  }

  console.log(`Fetching telemetry data from ${env}...\n`);

  const client = Client.forEnv(env);

  try {
    const samples = await client.getDeviceLatencySamples(
      new PublicKey(originPK),
      new PublicKey(targetPK),
      new PublicKey(linkPK),
      epoch,
    );

    console.log("=== Device Latency Samples ===");
    console.log(`Epoch: ${samples.epoch}`);
    console.log(`Origin Device: ${new PublicKey(samples.originDevicePK).toBase58()}`);
    console.log(`Target Device: ${new PublicKey(samples.targetDevicePK).toBase58()}`);
    console.log(`Link: ${new PublicKey(samples.linkPK).toBase58()}`);
    console.log(`Sampling Interval: ${samples.samplingIntervalMicroseconds}us`);
    console.log(`Sample Count: ${samples.samples.length}`);

    if (samples.samples.length > 0) {
      const total = samples.samples.reduce((a, b) => a + b, 0);
      const min = Math.min(...samples.samples);
      const max = Math.max(...samples.samples);
      const avg = total / samples.samples.length;

      console.log();
      console.log("Statistics:");
      console.log(`  Average: ${(avg / 1000).toFixed(2)}ms`);
      console.log(`  Min: ${(min / 1000).toFixed(2)}ms`);
      console.log(`  Max: ${(max / 1000).toFixed(2)}ms`);
    }

  } catch (err) {
    if ((err as Error).message === "Account not found") {
      console.log("No samples found for the specified parameters.");
      console.log("The account may not exist for this epoch or device pair.");
    } else {
      throw err;
    }
  }

  console.log();
  console.log("Done.");
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
