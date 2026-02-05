#!/usr/bin/env bun
/** Example CLI that fetches and displays serviceability program data. */

import { Client } from "../serviceability/client.js";
import {
  locationStatusString,
  exchangeStatusString,
  deviceStatusString,
  deviceHealthString,
  linkLinkTypeString,
  linkStatusString,
  userUserTypeString,
  userStatusString,
} from "../serviceability/state.js";

function formatIp(ip: Uint8Array): string {
  return `${ip[0]}.${ip[1]}.${ip[2]}.${ip[3]}`;
}

async function main() {
  const args = process.argv.slice(2);
  let env = "mainnet-beta";

  for (let i = 0; i < args.length; i++) {
    if (args[i] === "--env" && args[i + 1]) {
      env = args[i + 1];
      i++;
    }
  }

  const validEnvs = ["mainnet-beta", "testnet", "devnet", "localnet"];
  if (!validEnvs.includes(env)) {
    console.error(`Invalid environment: ${env}`);
    process.exit(1);
  }

  console.log(`Fetching serviceability data from ${env}...\n`);

  const client = Client.forEnv(env);
  const data = await client.getProgramData();

  // Summary
  console.log("=== Summary ===");
  console.log(`Locations:        ${data.locations.length}`);
  console.log(`Exchanges:        ${data.exchanges.length}`);
  console.log(`Contributors:     ${data.contributors.length}`);
  console.log(`Devices:          ${data.devices.length}`);
  console.log(`Links:            ${data.links.length}`);
  console.log(`Users:            ${data.users.length}`);
  console.log(`Multicast Groups: ${data.multicastGroups.length}`);
  console.log(`Access Passes:    ${data.accessPasses.length}`);
  console.log();

  // Global Config
  if (data.globalConfig) {
    console.log("=== Global Config ===");
    console.log(`Local ASN:        ${data.globalConfig.localAsn}`);
    console.log(`Remote ASN:       ${data.globalConfig.remoteAsn}`);
    console.log();
  }

  // Locations
  if (data.locations.length > 0) {
    console.log("=== Locations ===");
    for (const loc of data.locations) {
      console.log(
        `  ${loc.code} (${loc.name}) - ${loc.country} [${locationStatusString(loc.status)}]`,
      );
    }
    console.log();
  }

  // Exchanges
  if (data.exchanges.length > 0) {
    console.log("=== Exchanges ===");
    for (const ex of data.exchanges) {
      console.log(
        `  ${ex.code} (${ex.name}) [${exchangeStatusString(ex.status)}]`,
      );
    }
    console.log();
  }

  // Devices
  if (data.devices.length > 0) {
    console.log("=== Devices ===");
    for (const dev of data.devices) {
      const publicIp = formatIp(dev.publicIp);
      console.log(
        `  ${dev.code} - ${publicIp} [status=${deviceStatusString(dev.status)}, health=${deviceHealthString(dev.deviceHealth)}]`,
      );
    }
    console.log();
  }

  // Links
  if (data.links.length > 0) {
    console.log("=== Links ===");
    for (const link of data.links) {
      const delayMs = Number(link.delayNs) / 1_000_000;
      console.log(
        `  ${link.code} - ${linkLinkTypeString(link.linkType)}, ${link.bandwidth} bps, ${delayMs}ms delay [${linkStatusString(link.status)}]`,
      );
    }
    console.log();
  }

  // Users
  if (data.users.length > 0) {
    console.log("=== Users ===");
    for (const user of data.users) {
      const ownerShort = user.owner.toBase58().slice(0, 12);
      console.log(
        `  ${ownerShort}... - ${userUserTypeString(user.userType)} [${userStatusString(user.status)}]`,
      );
    }
    console.log();
  }

  console.log("Done.");
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
