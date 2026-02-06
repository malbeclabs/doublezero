/**
 * PDA derivation tests.
 */

import { describe, expect, test } from "bun:test";
import { PublicKey } from "@solana/web3.js";
import {
  deriveDeviceLatencySamplesPda,
  deriveInternetLatencySamplesPda,
} from "../pda.js";

const PROGRAM_ID = new PublicKey(
  "tE1exJ5VMyoC9ByZeSmgtNzJCFF74G9JAv338sJiqkC",
);

describe("deriveDeviceLatencySamplesPda", () => {
  test("deterministic", () => {
    const origin = new PublicKey("11111111111111111111111111111112");
    const target = new PublicKey("11111111111111111111111111111113");
    const link = new PublicKey("11111111111111111111111111111114");

    const [addr1, bump1] = deriveDeviceLatencySamplesPda(
      PROGRAM_ID,
      origin,
      target,
      link,
      42,
    );
    const [addr2, bump2] = deriveDeviceLatencySamplesPda(
      PROGRAM_ID,
      origin,
      target,
      link,
      42,
    );

    expect(addr1.equals(addr2)).toBe(true);
    expect(bump1).toBe(bump2);
  });
});

describe("deriveInternetLatencySamplesPda", () => {
  test("non-zero", () => {
    const oracle = new PublicKey("11111111111111111111111111111112");
    const origin = new PublicKey("11111111111111111111111111111113");
    const target = new PublicKey("11111111111111111111111111111114");

    const [addr] = deriveInternetLatencySamplesPda(
      PROGRAM_ID,
      oracle,
      "RIPE Atlas",
      origin,
      target,
      42,
    );
    expect(addr.equals(PublicKey.default)).toBe(false);
  });
});
