import { PublicKey } from "@solana/web3.js";

const SEED_PREFIX = Buffer.from("doublezero");
const SEED_GLOBAL_STATE = Buffer.from("globalstate");
const SEED_GLOBAL_CONFIG = Buffer.from("config");
const SEED_PROGRAM_CONFIG = Buffer.from("programconfig");
const SEED_TENANT = Buffer.from("tenant");

export function deriveGlobalStatePda(
  programId: PublicKey,
): [PublicKey, number] {
  return PublicKey.findProgramAddressSync([SEED_PREFIX, SEED_GLOBAL_STATE], programId);
}

export function deriveGlobalConfigPda(
  programId: PublicKey,
): [PublicKey, number] {
  return PublicKey.findProgramAddressSync([SEED_PREFIX, SEED_GLOBAL_CONFIG], programId);
}

export function deriveProgramConfigPda(
  programId: PublicKey,
): [PublicKey, number] {
  return PublicKey.findProgramAddressSync([SEED_PREFIX, SEED_PROGRAM_CONFIG], programId);
}

export function deriveTenantPda(
  programId: PublicKey,
  code: string,
): [PublicKey, number] {
  return PublicKey.findProgramAddressSync(
    [SEED_PREFIX, SEED_TENANT, Buffer.from(code)],
    programId,
  );
}
