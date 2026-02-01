import { describe, expect, test } from "bun:test";
import { PublicKey } from "@solana/web3.js";
import {
  deriveConfigPda,
  deriveDistributionPda,
  deriveJournalPda,
  deriveValidatorDepositPda,
  deriveContributorRewardsPda,
} from "../pda.js";

const PROGRAM_ID = new PublicKey(
  "dzrevZC94tBLwuHw1dyynZxaXTWyp7yocsinyEVPtt4",
);

describe("PDA derivation", () => {
  test("config PDA is deterministic", () => {
    const [addr1, bump1] = deriveConfigPda(PROGRAM_ID);
    const [addr2, bump2] = deriveConfigPda(PROGRAM_ID);
    expect(addr1.equals(addr2)).toBe(true);
    expect(bump1).toBe(bump2);
    expect(addr1.equals(PublicKey.default)).toBe(false);
  });

  test("distribution PDA differs by epoch", () => {
    const [addr1] = deriveDistributionPda(PROGRAM_ID, 1n);
    const [addr2] = deriveDistributionPda(PROGRAM_ID, 2n);
    expect(addr1.equals(addr2)).toBe(false);
  });

  test("journal PDA", () => {
    const [addr] = deriveJournalPda(PROGRAM_ID);
    expect(addr.equals(PublicKey.default)).toBe(false);
  });

  test("validator deposit PDA", () => {
    const nodeId = new PublicKey(
      "4uQeVj5tqViQh7yWWGStvkEG1Zmhx6uasJtWCJziofM",
    );
    const [addr] = deriveValidatorDepositPda(PROGRAM_ID, nodeId);
    expect(addr.equals(PublicKey.default)).toBe(false);
  });

  test("contributor rewards PDA", () => {
    const serviceKey = new PublicKey(
      "4uQeVj5tqViQh7yWWGStvkEG1Zmhx6uasJtWCJziofM",
    );
    const [addr] = deriveContributorRewardsPda(PROGRAM_ID, serviceKey);
    expect(addr.equals(PublicKey.default)).toBe(false);
  });
});
