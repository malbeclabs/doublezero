import { PublicKey } from "@solana/web3.js";

const SEED_GLOBAL_STATE = Buffer.from("global_state");
const SEED_GLOBAL_CONFIG = Buffer.from("global_config");
const SEED_PROGRAM_CONFIG = Buffer.from("program_config");

export function deriveGlobalStatePda(
  programId: PublicKey,
): [PublicKey, number] {
  return PublicKey.findProgramAddressSync([SEED_GLOBAL_STATE], programId);
}

export function deriveGlobalConfigPda(
  programId: PublicKey,
): [PublicKey, number] {
  return PublicKey.findProgramAddressSync([SEED_GLOBAL_CONFIG], programId);
}

export function deriveProgramConfigPda(
  programId: PublicKey,
): [PublicKey, number] {
  return PublicKey.findProgramAddressSync([SEED_PROGRAM_CONFIG], programId);
}
