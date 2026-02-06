import { describe, expect, test } from "bun:test";
import { PublicKey } from "@solana/web3.js";
import {
  deriveGlobalStatePda,
  deriveGlobalConfigPda,
  deriveProgramConfigPda,
} from "../pda.js";

const PROGRAM_ID = new PublicKey(
  "ser2VaTMAcYTaauMrTSfSrxBaUDq7BLNs2xfUugTAGv",
);

describe("PDA derivation", () => {
  test("global state PDA is deterministic", () => {
    const [addr1, bump1] = deriveGlobalStatePda(PROGRAM_ID);
    const [addr2, bump2] = deriveGlobalStatePda(PROGRAM_ID);
    expect(addr1.equals(addr2)).toBe(true);
    expect(bump1).toBe(bump2);
  });

  test("global config PDA", () => {
    const [addr] = deriveGlobalConfigPda(PROGRAM_ID);
    expect(addr.equals(PublicKey.default)).toBe(false);
  });

  test("program config PDA", () => {
    const [addr] = deriveProgramConfigPda(PROGRAM_ID);
    expect(addr.equals(PublicKey.default)).toBe(false);
  });

  test("PDAs are different", () => {
    const [gs] = deriveGlobalStatePda(PROGRAM_ID);
    const [gc] = deriveGlobalConfigPda(PROGRAM_ID);
    const [pc] = deriveProgramConfigPda(PROGRAM_ID);
    expect(gs.equals(gc)).toBe(false);
    expect(gs.equals(pc)).toBe(false);
    expect(gc.equals(pc)).toBe(false);
  });
});
