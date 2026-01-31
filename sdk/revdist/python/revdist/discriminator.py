import hashlib

DISCRIMINATOR_SIZE = 8


def _sha256_first8(s: str) -> bytes:
    return hashlib.sha256(s.encode()).digest()[:8]


DISCRIMINATOR_PROGRAM_CONFIG = _sha256_first8("dz::account::program_config")
DISCRIMINATOR_DISTRIBUTION = _sha256_first8("dz::account::distribution")
DISCRIMINATOR_SOLANA_VALIDATOR_DEPOSIT = _sha256_first8(
    "dz::account::solana_validator_deposit"
)
DISCRIMINATOR_CONTRIBUTOR_REWARDS = _sha256_first8(
    "dz::account::contributor_rewards"
)
DISCRIMINATOR_JOURNAL = _sha256_first8("dz::account::journal")


def validate_discriminator(data: bytes, expected: bytes) -> None:
    """Validate the 8-byte discriminator prefix. Raises ValueError on mismatch."""
    if len(data) < DISCRIMINATOR_SIZE:
        raise ValueError(
            f"data too short: {len(data)} bytes, need at least {DISCRIMINATOR_SIZE}"
        )
    got = data[:DISCRIMINATOR_SIZE]
    if got != expected:
        raise ValueError(
            f"invalid discriminator: got {got.hex()}, want {expected.hex()}"
        )
