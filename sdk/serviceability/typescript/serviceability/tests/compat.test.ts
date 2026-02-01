/**
 * Mainnet compatibility tests.
 *
 * These tests fetch live mainnet-beta data and verify that our struct
 * deserialization works against real on-chain accounts.
 *
 * Run with:
 *   SERVICEABILITY_COMPAT_TEST=1 cd sdk/serviceability/typescript && bun test --grep compat
 *
 * Requires network access to Solana mainnet RPC.
 */

import { describe, expect, test, setDefaultTimeout } from "bun:test";

// Compat tests hit public RPC endpoints which may be slow or rate-limited.
setDefaultTimeout(30_000);
import { Connection, PublicKey } from "@solana/web3.js";
import { PROGRAM_IDS, LEDGER_RPC_URLS } from "../config.js";
import { newConnection } from "../rpc.js";
import {
  deriveGlobalConfigPda,
  deriveGlobalStatePda,
  deriveProgramConfigPda,
} from "../pda.js";
import {
  deserializeGlobalConfig,
  deserializeGlobalState,
  deserializeProgramConfig,
} from "../state.js";

function skipUnlessCompat(): void {
  if (!process.env.SERVICEABILITY_COMPAT_TEST) {
    throw new Error("SKIP");
  }
}

function rpcUrl(): string {
  return process.env.SOLANA_RPC_URL || LEDGER_RPC_URLS["mainnet-beta"];
}

function programId(): PublicKey {
  return new PublicKey(PROGRAM_IDS["mainnet-beta"]);
}

async function fetchRawAccount(addr: PublicKey): Promise<Buffer> {
  const conn = newConnection(rpcUrl());
  const info = await conn.getAccountInfo(addr);
  if (info === null) throw new Error(`account not found: ${addr.toBase58()}`);
  return info.data;
}

function readU8(buf: Buffer, offset: number): number {
  return buf.readUInt8(offset);
}

function readU16LE(buf: Buffer, offset: number): number {
  return buf.readUInt16LE(offset);
}

function readU32LE(buf: Buffer, offset: number): number {
  return buf.readUInt32LE(offset);
}

function readPubkey(buf: Buffer, offset: number): PublicKey {
  return new PublicKey(buf.subarray(offset, offset + 32));
}

describe("compat: ProgramConfig", () => {
  test("deserialize matches raw bytes", async () => {
    try {
      skipUnlessCompat();
    } catch {
      return;
    }

    const pid = programId();
    const [addr] = deriveProgramConfigPda(pid);
    const raw = await fetchRawAccount(addr);

    const pc = deserializeProgramConfig(new Uint8Array(raw));

    // ProgramConfig layout (all fixed-size):
    // offset 0: AccountType (u8)
    // offset 1: BumpSeed (u8)
    // offset 2: Version.Major (u32)
    // offset 6: Version.Minor (u32)
    // offset 10: Version.Patch (u32)
    // offset 14: MinCompatVersion.Major (u32)
    // offset 18: MinCompatVersion.Minor (u32)
    // offset 22: MinCompatVersion.Patch (u32)
    expect(pc.accountType).toBe(readU8(raw, 0));
    expect(pc.bumpSeed).toBe(readU8(raw, 1));
    expect(pc.version.major).toBe(readU32LE(raw, 2));
    expect(pc.version.minor).toBe(readU32LE(raw, 6));
    expect(pc.version.patch).toBe(readU32LE(raw, 10));
    expect(pc.minCompatVersion.major).toBe(readU32LE(raw, 14));
    expect(pc.minCompatVersion.minor).toBe(readU32LE(raw, 18));
    expect(pc.minCompatVersion.patch).toBe(readU32LE(raw, 22));

    expect(pc.accountType).toBe(9);
  });
});

describe("compat: GlobalConfig", () => {
  test("deserialize matches raw bytes", async () => {
    try {
      skipUnlessCompat();
    } catch {
      return;
    }

    const pid = programId();
    const [addr] = deriveGlobalConfigPda(pid);
    const raw = await fetchRawAccount(addr);

    const gc = deserializeGlobalConfig(new Uint8Array(raw));

    // GlobalConfig layout (all fixed-size):
    // offset 0: AccountType (u8)
    // offset 1: Owner (32 bytes)
    // offset 33: BumpSeed (u8)
    // offset 34: LocalASN (u32)
    // offset 38: RemoteASN (u32)
    // offset 57: NextBGPCommunity (u16)
    expect(gc.accountType).toBe(readU8(raw, 0));
    expect(gc.owner.toBase58()).toBe(readPubkey(raw, 1).toBase58());
    expect(gc.bumpSeed).toBe(readU8(raw, 33));
    expect(gc.localAsn).toBe(readU32LE(raw, 34));
    expect(gc.remoteAsn).toBe(readU32LE(raw, 38));
    expect(gc.nextBgpCommunity).toBe(readU16LE(raw, 57));

    expect(gc.accountType).toBe(2);
    expect(gc.localAsn).toBeGreaterThan(0);
  });
});

describe("compat: GlobalState", () => {
  test("deserialize and sanity check", async () => {
    try {
      skipUnlessCompat();
    } catch {
      return;
    }

    const pid = programId();
    const [addr] = deriveGlobalStatePda(pid);
    const raw = await fetchRawAccount(addr);

    const gs = deserializeGlobalState(new Uint8Array(raw));

    // GlobalState fixed layout (first 2 bytes before variable-length vecs):
    expect(gs.accountType).toBe(readU8(raw, 0));
    expect(gs.bumpSeed).toBe(readU8(raw, 1));

    expect(gs.accountType).toBe(1);

    // Sanity checks.
    expect(gs.activatorAuthorityPk.equals(PublicKey.default)).toBe(false);
    expect(gs.sentinelAuthorityPk.equals(PublicKey.default)).toBe(false);
    // healthOraclePk may be zero on mainnet
  });
});
