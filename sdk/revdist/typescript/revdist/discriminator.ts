import { createHash } from "crypto";

export const DISCRIMINATOR_SIZE = 8;

function sha256First8(s: string): Uint8Array {
  const hash = createHash("sha256").update(s).digest();
  return new Uint8Array(hash.buffer, hash.byteOffset, 8);
}

export const DISCRIMINATOR_PROGRAM_CONFIG = sha256First8(
  "dz::account::program_config",
);
export const DISCRIMINATOR_DISTRIBUTION = sha256First8(
  "dz::account::distribution",
);
export const DISCRIMINATOR_SOLANA_VALIDATOR_DEPOSIT = sha256First8(
  "dz::account::solana_validator_deposit",
);
export const DISCRIMINATOR_CONTRIBUTOR_REWARDS = sha256First8(
  "dz::account::contributor_rewards",
);
export const DISCRIMINATOR_JOURNAL = sha256First8("dz::account::journal");

export function validateDiscriminator(
  data: Uint8Array,
  expected: Uint8Array,
): void {
  if (data.length < DISCRIMINATOR_SIZE) {
    throw new Error(
      `data too short: ${data.length} bytes, need at least ${DISCRIMINATOR_SIZE}`,
    );
  }
  for (let i = 0; i < DISCRIMINATOR_SIZE; i++) {
    if (data[i] !== expected[i]) {
      const gotHex = Buffer.from(data.slice(0, 8)).toString("hex");
      const wantHex = Buffer.from(expected).toString("hex");
      throw new Error(
        `invalid discriminator: got ${gotHex}, want ${wantHex}`,
      );
    }
  }
}
