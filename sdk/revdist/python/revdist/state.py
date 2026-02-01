"""On-chain account data structures for the revenue distribution program.

Binary layout matches Rust #[repr(C)] structs. Deserialization uses
struct.unpack_from with little-endian byte order and tolerates extra
trailing bytes for forward compatibility.
"""

from __future__ import annotations

import struct
from dataclasses import dataclass

from borsh_incremental import IncrementalReader
from solders.pubkey import Pubkey  # type: ignore[import-untyped]

from revdist.discriminator import DISCRIMINATOR_SIZE, validate_discriminator


def _pubkey(data: bytes, offset: int) -> Pubkey:
    return Pubkey.from_bytes(data[offset : offset + 32])


# ---------------------------------------------------------------------------
# Nested structs
# ---------------------------------------------------------------------------


@dataclass
class CommunityBurnRateParameters:
    limit: int  # u32
    dz_epochs_to_increasing: int  # u32
    dz_epochs_to_limit: int  # u32
    cached_slope_numerator: int  # u32
    cached_slope_denominator: int  # u32
    cached_next_burn_rate: int  # u32

    STRUCT_SIZE = 24

    @classmethod
    def from_bytes(cls, data: bytes, offset: int = 0) -> CommunityBurnRateParameters:
        fields = struct.unpack_from("<6I", data, offset)
        return cls(*fields)


@dataclass
class SolanaValidatorFeeParameters:
    base_block_rewards_pct: int  # u16
    priority_block_rewards_pct: int  # u16
    inflation_rewards_pct: int  # u16
    jito_tips_pct: int  # u16
    fixed_sol_amount: int  # u32

    STRUCT_SIZE = 40  # 4*u16 + u32 + 7*u32 reserved

    @classmethod
    def from_bytes(
        cls, data: bytes, offset: int = 0
    ) -> SolanaValidatorFeeParameters:
        vals = struct.unpack_from("<4HI", data, offset)
        return cls(*vals)


@dataclass
class DistributionParameters:
    calculation_grace_period_minutes: int  # u16
    initialization_grace_period_minutes: int  # u16
    minimum_epoch_duration_to_finalize_rewards: int  # u8
    community_burn_rate_parameters: CommunityBurnRateParameters
    solana_validator_fee_parameters: SolanaValidatorFeeParameters

    STRUCT_SIZE = 328  # 2+2+1+3pad+24+40+256reserved

    @classmethod
    def from_bytes(cls, data: bytes, offset: int = 0) -> DistributionParameters:
        calc_gp, init_gp = struct.unpack_from("<2H", data, offset)
        min_epoch = struct.unpack_from("<B", data, offset + 4)[0]
        # 3 bytes padding at offset+5
        burn = CommunityBurnRateParameters.from_bytes(data, offset + 8)
        vfee = SolanaValidatorFeeParameters.from_bytes(data, offset + 8 + 24)
        return cls(calc_gp, init_gp, min_epoch, burn, vfee)


@dataclass
class RelayParameters:
    placeholder_lamports: int  # u32
    distribute_rewards_lamports: int  # u32

    STRUCT_SIZE = 40  # 4+4+32reserved

    @classmethod
    def from_bytes(cls, data: bytes, offset: int = 0) -> RelayParameters:
        vals = struct.unpack_from("<2I", data, offset)
        return cls(*vals)


@dataclass
class RecipientShare:
    recipient_key: Pubkey  # 32 bytes
    share: int  # u16

    STRUCT_SIZE = 34

    @classmethod
    def from_bytes(cls, data: bytes, offset: int = 0) -> RecipientShare:
        key = _pubkey(data, offset)
        share = struct.unpack_from("<H", data, offset + 32)[0]
        return cls(key, share)


# ---------------------------------------------------------------------------
# Top-level account types
# ---------------------------------------------------------------------------


def _deserialize(data: bytes, discriminator: bytes, min_size: int) -> bytes:
    """Validate discriminator and return the body bytes.

    Tolerates extra trailing bytes for forward compatibility.
    """
    validate_discriminator(data, discriminator)
    body = data[DISCRIMINATOR_SIZE:]
    if len(body) < min_size:
        raise ValueError(
            f"account data too short: have {len(body)} bytes, need at least {min_size}"
        )
    return body


@dataclass
class ProgramConfig:
    flags: int  # u64
    next_completed_dz_epoch: int  # u64
    bump_seed: int  # u8
    reserve_2z_bump_seed: int  # u8
    swap_authority_bump_seed: int  # u8
    swap_destination_2z_bump_seed: int  # u8
    withdraw_sol_authority_bump_seed: int  # u8
    admin_key: Pubkey
    debt_accountant_key: Pubkey
    rewards_accountant_key: Pubkey
    contributor_manager_key: Pubkey
    placeholder_key: Pubkey
    sol_2z_swap_program_id: Pubkey
    distribution_parameters: DistributionParameters
    relay_parameters: RelayParameters
    last_initialized_distribution_timestamp: int  # u32
    debt_write_off_feature_activation_epoch: int  # u64

    STRUCT_SIZE = 600

    @classmethod
    def from_bytes(
        cls, data: bytes, discriminator: bytes
    ) -> ProgramConfig:
        b = _deserialize(data, discriminator, cls.STRUCT_SIZE)
        flags, next_epoch = struct.unpack_from("<2Q", b, 0)
        bump, r2z, swap_auth, swap_dest, withdraw = struct.unpack_from(
            "<5B", b, 16
        )
        # 3 bytes padding at 21
        off = 24
        admin = _pubkey(b, off); off += 32
        debt = _pubkey(b, off); off += 32
        rewards = _pubkey(b, off); off += 32
        contrib_mgr = _pubkey(b, off); off += 32
        placeholder = _pubkey(b, off); off += 32
        swap_prog = _pubkey(b, off); off += 32
        dist_params = DistributionParameters.from_bytes(b, off)
        off += DistributionParameters.STRUCT_SIZE
        relay = RelayParameters.from_bytes(b, off)
        off += RelayParameters.STRUCT_SIZE
        last_ts = struct.unpack_from("<I", b, off)[0]
        off += 4
        # 4 bytes padding
        off += 4
        debt_wo_epoch = struct.unpack_from("<Q", b, off)[0]
        return cls(
            flags=flags,
            next_completed_dz_epoch=next_epoch,
            bump_seed=bump,
            reserve_2z_bump_seed=r2z,
            swap_authority_bump_seed=swap_auth,
            swap_destination_2z_bump_seed=swap_dest,
            withdraw_sol_authority_bump_seed=withdraw,
            admin_key=admin,
            debt_accountant_key=debt,
            rewards_accountant_key=rewards,
            contributor_manager_key=contrib_mgr,
            placeholder_key=placeholder,
            sol_2z_swap_program_id=swap_prog,
            distribution_parameters=dist_params,
            relay_parameters=relay,
            last_initialized_distribution_timestamp=last_ts,
            debt_write_off_feature_activation_epoch=debt_wo_epoch,
        )


@dataclass
class Distribution:
    dz_epoch: int  # u64
    flags: int  # u64
    community_burn_rate: int  # u32
    bump_seed: int  # u8
    token_2z_pda_bump_seed: int  # u8
    solana_validator_fee_parameters: SolanaValidatorFeeParameters
    solana_validator_debt_merkle_root: bytes  # 32 bytes
    total_solana_validators: int  # u32
    solana_validator_payments_count: int  # u32
    total_solana_validator_debt: int  # u64
    collected_solana_validator_payments: int  # u64
    rewards_merkle_root: bytes  # 32 bytes
    total_contributors: int  # u32
    distributed_rewards_count: int  # u32
    collected_prepaid_2z_payments: int  # u64
    collected_2z_converted_from_sol: int  # u64
    uncollectible_sol_debt: int  # u64
    processed_sv_debt_start_index: int  # u32
    processed_sv_debt_end_index: int  # u32
    processed_rewards_start_index: int  # u32
    processed_rewards_end_index: int  # u32
    distribute_rewards_relay_lamports: int  # u32
    calculation_allowed_timestamp: int  # u32
    distributed_2z_amount: int  # u64
    burned_2z_amount: int  # u64
    processed_sv_debt_wo_start_index: int  # u32
    processed_sv_debt_wo_end_index: int  # u32
    solana_validator_write_off_count: int  # u32

    STRUCT_SIZE = 448

    @classmethod
    def from_bytes(cls, data: bytes, discriminator: bytes) -> Distribution:
        b = _deserialize(data, discriminator, cls.STRUCT_SIZE)
        dz_epoch, flags = struct.unpack_from("<2Q", b, 0)
        burn_rate = struct.unpack_from("<I", b, 16)[0]
        bump, t2z_bump = struct.unpack_from("<2B", b, 20)
        # 2 bytes padding at 22
        vfee = SolanaValidatorFeeParameters.from_bytes(b, 24)
        off = 24 + SolanaValidatorFeeParameters.STRUCT_SIZE  # 64
        sv_debt_root = b[off : off + 32]; off += 32  # 96
        total_sv, sv_pay_count = struct.unpack_from("<2I", b, off); off += 8  # 104
        total_sv_debt, collected_sv_pay = struct.unpack_from("<2Q", b, off); off += 16  # 120
        rewards_root = b[off : off + 32]; off += 32  # 152
        total_contrib, dist_rew_count = struct.unpack_from("<2I", b, off); off += 8  # 160
        coll_2z, coll_sol, uncoll = struct.unpack_from("<3Q", b, off); off += 24  # 184
        (
            ps_start, ps_end, pr_start, pr_end,
            dr_relay, calc_ts,
        ) = struct.unpack_from("<6I", b, off); off += 24  # 208
        dist_2z, burned_2z = struct.unpack_from("<2Q", b, off); off += 16  # 224
        wo_start, wo_end, wo_count = struct.unpack_from("<3I", b, off)
        return cls(
            dz_epoch=dz_epoch,
            flags=flags,
            community_burn_rate=burn_rate,
            bump_seed=bump,
            token_2z_pda_bump_seed=t2z_bump,
            solana_validator_fee_parameters=vfee,
            solana_validator_debt_merkle_root=sv_debt_root,
            total_solana_validators=total_sv,
            solana_validator_payments_count=sv_pay_count,
            total_solana_validator_debt=total_sv_debt,
            collected_solana_validator_payments=collected_sv_pay,
            rewards_merkle_root=rewards_root,
            total_contributors=total_contrib,
            distributed_rewards_count=dist_rew_count,
            collected_prepaid_2z_payments=coll_2z,
            collected_2z_converted_from_sol=coll_sol,
            uncollectible_sol_debt=uncoll,
            processed_sv_debt_start_index=ps_start,
            processed_sv_debt_end_index=ps_end,
            processed_rewards_start_index=pr_start,
            processed_rewards_end_index=pr_end,
            distribute_rewards_relay_lamports=dr_relay,
            calculation_allowed_timestamp=calc_ts,
            distributed_2z_amount=dist_2z,
            burned_2z_amount=burned_2z,
            processed_sv_debt_wo_start_index=wo_start,
            processed_sv_debt_wo_end_index=wo_end,
            solana_validator_write_off_count=wo_count,
        )


@dataclass
class SolanaValidatorDeposit:
    node_id: Pubkey  # 32 bytes
    written_off_sol_debt: int  # u64

    STRUCT_SIZE = 96

    @classmethod
    def from_bytes(
        cls, data: bytes, discriminator: bytes
    ) -> SolanaValidatorDeposit:
        b = _deserialize(data, discriminator, cls.STRUCT_SIZE)
        node_id = _pubkey(b, 0)
        debt = struct.unpack_from("<Q", b, 32)[0]
        return cls(node_id=node_id, written_off_sol_debt=debt)


@dataclass
class ContributorRewards:
    rewards_manager_key: Pubkey  # 32 bytes
    service_key: Pubkey  # 32 bytes
    flags: int  # u64
    recipient_shares: list[RecipientShare]  # 8 entries

    STRUCT_SIZE = 600

    @classmethod
    def from_bytes(
        cls, data: bytes, discriminator: bytes
    ) -> ContributorRewards:
        b = _deserialize(data, discriminator, cls.STRUCT_SIZE)
        mgr = _pubkey(b, 0)
        svc = _pubkey(b, 32)
        flags = struct.unpack_from("<Q", b, 64)[0]
        shares = []
        off = 72
        for _ in range(8):
            shares.append(RecipientShare.from_bytes(b, off))
            off += RecipientShare.STRUCT_SIZE
        return cls(
            rewards_manager_key=mgr,
            service_key=svc,
            flags=flags,
            recipient_shares=shares,
        )


@dataclass
class Journal:
    bump_seed: int  # u8
    token_2z_pda_bump_seed: int  # u8
    total_sol_balance: int  # u64
    total_2z_balance: int  # u64
    swap_2z_destination_balance: int  # u64
    swapped_sol_amount: int  # u64
    next_dz_epoch_to_sweep_tokens: int  # u64
    lifetime_swapped_2z_amount: bytes  # u128 LE, 16 bytes

    STRUCT_SIZE = 64

    @classmethod
    def from_bytes(cls, data: bytes, discriminator: bytes) -> Journal:
        b = _deserialize(data, discriminator, cls.STRUCT_SIZE)
        bump, t2z_bump = struct.unpack_from("<2B", b, 0)
        # 6 bytes padding at 2
        (
            total_sol, total_2z, swap_dest, swapped, next_epoch,
        ) = struct.unpack_from("<5Q", b, 8)
        lifetime = b[48:64]
        return cls(
            bump_seed=bump,
            token_2z_pda_bump_seed=t2z_bump,
            total_sol_balance=total_sol,
            total_2z_balance=total_2z,
            swap_2z_destination_balance=swap_dest,
            swapped_sol_amount=swapped,
            next_dz_epoch_to_sweep_tokens=next_epoch,
            lifetime_swapped_2z_amount=lifetime,
        )


# ---------------------------------------------------------------------------
# DZ Ledger record types (Borsh-serialized)
# ---------------------------------------------------------------------------


@dataclass
class ComputedSolanaValidatorDebt:
    node_id: Pubkey  # 32 bytes
    amount: int  # u64


@dataclass
class ComputedSolanaValidatorDebts:
    blockhash: bytes  # 32 bytes
    first_solana_epoch: int  # u64
    last_solana_epoch: int  # u64
    debts: list[ComputedSolanaValidatorDebt]

    @classmethod
    def from_bytes(cls, data: bytes) -> ComputedSolanaValidatorDebts:
        r = IncrementalReader(data)
        blockhash = r.read_bytes(32)
        first_epoch = r.read_u64()
        last_epoch = r.read_u64()
        count = r.read_u32()
        debts = []
        for _ in range(count):
            node_id = Pubkey.from_bytes(r.read_pubkey_raw())
            amount = r.read_u64()
            debts.append(ComputedSolanaValidatorDebt(node_id=node_id, amount=amount))
        return cls(
            blockhash=blockhash,
            first_solana_epoch=first_epoch,
            last_solana_epoch=last_epoch,
            debts=debts,
        )


@dataclass
class RewardShare:
    contributor_key: Pubkey  # 32 bytes
    unit_share: int  # u32
    remaining_bytes: bytes  # 4 bytes

    @property
    def is_blocked(self) -> bool:
        val = struct.unpack_from("<I", self.remaining_bytes, 0)[0]
        return bool(val & (1 << 31))

    @property
    def economic_burn_rate(self) -> int:
        val = struct.unpack_from("<I", self.remaining_bytes, 0)[0]
        return val & 0x3FFFFFFF


@dataclass
class ShapleyOutputStorage:
    epoch: int  # u64
    rewards: list[RewardShare]
    total_unit_shares: int  # u32

    @classmethod
    def from_bytes(cls, data: bytes) -> ShapleyOutputStorage:
        r = IncrementalReader(data)
        epoch = r.read_u64()
        count = r.read_u32()
        rewards = []
        for _ in range(count):
            key = Pubkey.from_bytes(r.read_pubkey_raw())
            unit_share = r.read_u32()
            remaining = r.read_bytes(4)
            rewards.append(RewardShare(
                contributor_key=key,
                unit_share=unit_share,
                remaining_bytes=remaining,
            ))
        total_unit_shares = r.read_u32()
        return cls(epoch=epoch, rewards=rewards, total_unit_shares=total_unit_shares)
