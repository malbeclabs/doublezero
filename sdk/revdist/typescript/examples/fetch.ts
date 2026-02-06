#!/usr/bin/env bun
/** Example CLI that fetches and displays revenue distribution data. */

import { Client } from "../revdist/client.js";

async function main() {
  const args = process.argv.slice(2);
  let env = "mainnet-beta";
  let epoch: bigint | null = null;

  for (let i = 0; i < args.length; i++) {
    if (args[i] === "--env" && args[i + 1]) {
      env = args[i + 1];
      i++;
    } else if (args[i] === "--epoch" && args[i + 1]) {
      epoch = BigInt(args[i + 1]);
      i++;
    }
  }

  const validEnvs = ["mainnet-beta", "testnet", "devnet", "localnet"];
  if (!validEnvs.includes(env)) {
    console.error(`Invalid environment: ${env}`);
    process.exit(1);
  }

  console.log(`Fetching revenue distribution data from ${env}...\n`);

  const client = Client.forEnv(env);

  // Fetch program config
  let config;
  try {
    config = await client.fetchConfig();
  } catch (err) {
    console.error(`Error fetching config: ${err}`);
    process.exit(1);
  }

  console.log("=== Program Config ===");
  console.log(`Admin:                  ${config.adminKey.toBase58()}`);
  console.log(`Debt Accountant:        ${config.debtAccountantKey.toBase58()}`);
  console.log(`Rewards Accountant:     ${config.rewardsAccountantKey.toBase58()}`);
  console.log(`Contributor Manager:    ${config.contributorManagerKey.toBase58()}`);
  console.log(`Next Completed Epoch:   ${config.nextCompletedDzEpoch}`);
  console.log();

  console.log("=== Distribution Parameters ===");
  console.log(
    `Calculation Grace Period:   ${config.distributionParameters.calculationGracePeriodMinutes} minutes`,
  );
  console.log(
    `Initialization Grace:       ${config.distributionParameters.initializationGracePeriodMinutes} minutes`,
  );
  console.log(
    `Min Epoch Duration:         ${config.distributionParameters.minimumEpochDurationToFinalizeRewards}`,
  );
  console.log();

  const vfee = config.distributionParameters.solanaValidatorFeeParameters;
  console.log("=== Validator Fee Parameters ===");
  console.log(`Base Block Rewards:     ${(vfee.baseBlockRewardsPct / 100).toFixed(2)}%`);
  console.log(`Priority Block Rewards: ${(vfee.priorityBlockRewardsPct / 100).toFixed(2)}%`);
  console.log(`Inflation Rewards:      ${(vfee.inflationRewardsPct / 100).toFixed(2)}%`);
  console.log(`Jito Tips:              ${(vfee.jitoTipsPct / 100).toFixed(2)}%`);
  console.log();

  // Fetch distribution for a specific epoch
  let targetEpoch = epoch;
  if (targetEpoch === null && config.nextCompletedDzEpoch > 0n) {
    targetEpoch = config.nextCompletedDzEpoch - 1n;
  }

  if (targetEpoch !== null && targetEpoch > 0n) {
    try {
      const dist = await client.fetchDistribution(targetEpoch);
      console.log(`=== Distribution (epoch ${dist.dzEpoch}) ===`);
      console.log(
        `Community Burn Rate:            ${dist.communityBurnRate} (${((dist.communityBurnRate / 1_000_000_000) * 100).toFixed(2)}%)`,
      );
      console.log(`Total Solana Validators:        ${dist.totalSolanaValidators}`);
      console.log(`Validator Payments Count:       ${dist.solanaValidatorPaymentsCount}`);
      console.log(`Total Validator Debt:           ${dist.totalSolanaValidatorDebt} lamports`);
      console.log(`Collected Validator Payments:   ${dist.collectedSolanaValidatorPayments} lamports`);
      console.log(`Total Contributors:             ${dist.totalContributors}`);
      console.log(`Distributed Rewards Count:      ${dist.distributedRewardsCount}`);
      console.log(`Collected Prepaid 2Z:           ${dist.collectedPrepaid2zPayments}`);
      console.log(`2Z Converted from SOL:          ${dist.collected2zConvertedFromSol}`);
      console.log(`Distributed 2Z Amount:          ${dist.distributed2zAmount}`);
    } catch (err) {
      console.log(`=== Distribution (epoch ${targetEpoch}) ===`);
      console.log(`  Not found or error: ${err}`);
    }
    console.log();
  }

  // Fetch journal
  try {
    const journal = await client.fetchJournal();
    console.log("=== Journal ===");
    console.log(`Total SOL Balance:          ${journal.totalSolBalance} lamports`);
    console.log(`Total 2Z Balance:           ${journal.total2zBalance}`);
    console.log(`Swapped SOL Amount:         ${journal.swappedSolAmount} lamports`);
    console.log(`Next Epoch to Sweep:        ${journal.nextDzEpochToSweepTokens}`);
  } catch (err) {
    console.log("=== Journal ===");
    console.log(`  Not found or error: ${err}`);
  }
  console.log();

  // Fetch all validator deposits
  try {
    const deposits = await client.fetchAllValidatorDeposits();
    console.log(`=== Validator Deposits (${deposits.length}) ===`);
    for (let i = 0; i < Math.min(10, deposits.length); i++) {
      const dep = deposits[i];
      console.log(
        `  ${dep.nodeId.toBase58().slice(0, 16)}...: written off debt ${dep.writtenOffSolDebt}`,
      );
    }
    if (deposits.length > 10) {
      console.log(`  ... and ${deposits.length - 10} more`);
    }
  } catch (err) {
    console.log("=== Validator Deposits ===");
    console.log(`  Error: ${err}`);
  }
  console.log();

  // Fetch all contributor rewards
  try {
    const rewards = await client.fetchAllContributorRewards();
    console.log(`=== Contributor Rewards (${rewards.length}) ===`);
    for (let i = 0; i < Math.min(10, rewards.length); i++) {
      const r = rewards[i];
      console.log(
        `  ${r.serviceKey.toBase58().slice(0, 16)}...: rewards manager ${r.rewardsManagerKey.toBase58().slice(0, 16)}...`,
      );
    }
    if (rewards.length > 10) {
      console.log(`  ... and ${rewards.length - 10} more`);
    }
  } catch (err) {
    console.log("=== Contributor Rewards ===");
    console.log(`  Error: ${err}`);
  }
  console.log();

  console.log("Done.");
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
