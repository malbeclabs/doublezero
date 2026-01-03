package sol

import (
	schematypes "github.com/malbeclabs/doublezero/lake/pkg/indexer/schema"
)

var Datasets = []schematypes.Dataset{
	{
		Name:        "solana_gossip_nodes",
		DatasetType: schematypes.DatasetTypeSCD2,
		Purpose: `
			Solana gossip participants (includes validators and non-validators).
			Contains node public keys, IP addresses (gossip and TPU-QUIC), ports, client version, and epoch information.
			Use for Solana network participant analysis and joining to vote accounts.
		`,
		Tables: []string{"solana_gossip_nodes_current", "solana_gossip_nodes_history"},
		Description: `
		USAGE:
		- Always query solana_gossip_nodes_current for current state.
		- Joins:
			- solana_gossip_nodes_current.pubkey = solana_vote_accounts_current.node_pubkey
			- solana_gossip_nodes_current.gossip_ip = geoip_records_current.ip
			- solana_gossip_nodes_current.gossip_ip = dz_users_current.client_ip (via geoip_records_current.ip)

		TERMINOLOGY:
		- Gossip nodes are all Solana network participants.

		COLUMNS:
		- epoch (INTEGER): Solana blockchain epoch at snapshot time
		- pubkey (VARCHAR): Node public key (primary key)
		- gossip_ip (VARCHAR): Gossip IP address
		- gossip_port (INTEGER): Gossip port
		- tpuquic_ip (VARCHAR): TPU-QUIC IP address
		- tpuquic_port (INTEGER): TPU-QUIC port
		- version (VARCHAR): Solana client version

		SCD2 DATA STRUCTURE:
		- {table}_current: Current state of the dataset. One row per dataset entity.
		- {table}_history: Append-only historical versions with validity windows.
		- Always query {table}_current for current state. Query {table}_history only for historical analysis or point-in-time queries.
		- All joins should use {table}_current tables unless explicitly doing historical analysis.
		- {table}_current.as_of_ts is the timestamp of the snapshot that produced this row.
		- {table}_current.row_hash is the hash of the payload columns for change detection.
		- {table}_history.valid_from is the timestamp of the start of the validity window.
		- {table}_history.valid_to is the timestamp of the end of the validity window.
		- {table}_history.op is the operation that produced this row (I|U|D).
		- {table}_history.run_id is the identifier of the ingestion run that produced this row.
		`,
	},
	{
		Name:        "solana_vote_accounts",
		DatasetType: schematypes.DatasetTypeSCD2,
		Purpose: `
			Solana vote accounts (validators).
			Contains activated stake (lamports), commission, vote activity, and epoch status.
			Use for validator analysis, stake analysis, and joining to gossip nodes.
		`,
		Tables: []string{"solana_vote_accounts_current", "solana_vote_accounts_history"},
		Description: `
		USAGE:
		- Always query solana_vote_accounts_current for current state.
		- Joins:
			- solana_vote_accounts_current.node_pubkey = solana_gossip_nodes_current.pubkey
		- Filter for staked validators using epoch_vote_account = true AND activated_stake_lamports > 0.

		TERMINOLOGY:
		- A staked validator is a vote account with epoch_vote_account = true and activated_stake_lamports > 0.

		RULES:
		- Convert lamports to SOL as: lamports / 1e9.
		- Report stake in SOL, not lamports.
		- Be explicit whether results refer to gossip nodes or staked validators.

		COLUMNS:
		- epoch (INTEGER): Solana blockchain epoch at snapshot time
		- vote_pubkey (VARCHAR): Vote account public key (primary key)
		- node_pubkey (VARCHAR): Node public key
		- activated_stake_lamports (BIGINT): Activated stake (lamports)
		- epoch_vote_account (BOOLEAN): Vote account active for this epoch
		- commission_percentage (INTEGER): Commission percentage

		SCD2 DATA STRUCTURE:
		- {table}_current: Current state of the dataset. One row per dataset entity.
		- {table}_history: Append-only historical versions with validity windows.
		- Always query {table}_current for current state. Query {table}_history only for historical analysis or point-in-time queries.
		- All joins should use {table}_current tables unless explicitly doing historical analysis.
		- {table}_current.as_of_ts is the timestamp of the snapshot that produced this row.
		- {table}_current.row_hash is the hash of the payload columns for change detection.
		- {table}_history.valid_from is the timestamp of the start of the validity window.
		- {table}_history.valid_to is the timestamp of the end of the validity window.
		- {table}_history.op is the operation that produced this row (I|U|D).
		- {table}_history.run_id is the identifier of the ingestion run that produced this row.
		`,
	},
	{
		Name:        "solana_leader_schedule",
		DatasetType: schematypes.DatasetTypeSCD2,
		Purpose: `
			Leader schedule for an epoch.
			Contains node public keys and their assigned leader slots for each epoch.
			Use for analyzing validator leadership and slot assignments.
		`,
		Tables: []string{"solana_leader_schedule_current", "solana_leader_schedule_history"},
		Description: `
		USAGE:
		- Always query using time filter.
		- Joins:
			- solana_leader_schedule_current.node_pubkey = solana_vote_accounts_current.node_pubkey

		COLUMNS:
		- epoch (INTEGER): Solana blockchain epoch at snapshot time
		- node_pubkey (VARCHAR): Node public key (primary key)
		- slots (VARCHAR): Slots where the node is leader (stored as VARCHAR in SCD2)
		- slot_count (INTEGER): Number of leader slots

		SCD2 DATA STRUCTURE:
		- {table}_current: Current state of the dataset. One row per dataset entity.
		- {table}_history: Append-only historical versions with validity windows.
		- Always query {table}_current for current state. Query {table}_history only for historical analysis or point-in-time queries.
		- All joins should use {table}_current tables unless explicitly doing historical analysis.
		- {table}_current.as_of_ts is the timestamp of the snapshot that produced this row.
		- {table}_current.row_hash is the hash of the payload columns for change detection.
		- {table}_history.valid_from is the timestamp of the start of the validity window.
		- {table}_history.valid_to is the timestamp of the end of the validity window.
		- {table}_history.op is the operation that produced this row (I|U|D).
		- {table}_history.run_id is the identifier of the ingestion run that produced this row.
		`,
	},
	{
		Name:        "solana_vote_account_activity",
		DatasetType: schematypes.DatasetTypeFact,
		Purpose: `
			Append-only time series of Solana vote account activity sampled every minute from getVoteAccounts, with a network reference slot from getSlot. Used to measure voting health, finality lag, and credit accrual.
		`,
		Tables: []string{"solana_vote_account_activity_raw"},
		Description: `
		USAGE:
		- Append-only fact table with one row per (time, vote_account_pubkey) combination.
		- Sampled every minute from getVoteAccounts and getSlot RPC calls.
		- Partitioned by date(time), ordered by (vote_account_pubkey, time) within partitions.
		- Joins:
			- solana_vote_account_activity_raw.vote_account_pubkey = solana_vote_accounts_current.vote_pubkey
			- solana_vote_account_activity_raw.node_identity_pubkey = solana_gossip_nodes_current.pubkey

		TERMINOLOGY:
		- Vote account activity: Time-series measurements of vote account state including voting slots, credits, and health metrics.

		DERIVED METRICS (query-time):
		- vote_lag_slots = cluster_slot - last_vote_slot
		- root_lag_slots = cluster_slot - root_slot
		- vote_root_gap = last_vote_slot - root_slot

		CONSTRAINTS:
		- root_slot <= last_vote_slot <= cluster_slot
		- credits_epoch_credits >= 0
		- commission in [0, 100] (if present)
		- activated_stake_lamports >= 0 (if present)

		COLUMNS:
		- time (TIMESTAMP): Collection time (UTC, minute cadence)
		- vote_account_pubkey (VARCHAR): Vote account address
		- node_identity_pubkey (VARCHAR): Validator identity stamped at collection time
		- root_slot (BIGINT): Latest finalized slot for the vote account
		- last_vote_slot (BIGINT): Most recent (highest) voted slot
		- cluster_slot (BIGINT): Network reference slot from getSlot
		- is_delinquent (BOOLEAN): From getVoteAccounts delinquent list
		- epoch_credits_json (VARCHAR): Raw epochCredits array from RPC (JSON string)
		- credits_epoch (INTEGER): Epoch of the latest epochCredits entry
		- credits_epoch_credits (BIGINT): Credits accrued so far in credits_epoch
		- credits_delta (BIGINT, nullable): Minute-over-minute credits delta (epoch-aware)
			* First observation: NULL
			* Same epoch (E == E_prev): max(C - C_prev, 0)
			* Epoch rollover (E == E_prev + 1): NULL (cannot calculate meaningful delta across epochs)
			* Any other jump/gap: NULL
		- activated_stake_lamports (BIGINT, nullable): Activated stake in lamports
		- activated_stake_sol (DOUBLE, nullable): Activated stake in SOL (lamports / 1e9)
		- commission (INTEGER, nullable): Commission percentage [0, 100]
		- collector_run_id (VARCHAR, nullable): Identifier for the data collection run

		FACT TABLE STRUCTURE:
		- Append-only: No updates or deletes, only inserts
		- Partitioned by: date(time) (year, month, day)
		- Ordered within partitions: (vote_account_pubkey, time)
		- Grain: One row per (time, vote_account_pubkey)
		`,
	},
	{
		Name:        "solana_block_production",
		DatasetType: schematypes.DatasetTypeFact,
		Purpose: `
			Hourly snapshots of cumulative leader slot assignment vs blocks produced from getBlockProduction, so you can compute skip rates during an epoch and also recover final per-epoch totals.
		`,
		Tables: []string{"solana_block_production_raw"},
		Description: `
		USAGE:
		- Append-only fact table with one row per (epoch, time, leader_identity_pubkey) combination.
		- Sampled hourly from getBlockProduction RPC calls.
		- Partitioned by date(time), ordered by (leader_identity_pubkey, time) within partitions.
		- Joins:
			- solana_block_production_raw.leader_identity_pubkey = solana_gossip_nodes_current.pubkey
			- solana_block_production_raw.leader_identity_pubkey = solana_vote_accounts_current.node_pubkey

		TERMINOLOGY:
		- Block production: Time-series measurements of leader slot assignments and blocks produced per validator.

		DERIVED METRICS (query-time):
		- slots_skipped_cum = leader_slots_assigned_cum - blocks_produced_cum
		- produce_rate_cum = blocks_produced_cum / NULLIF(leader_slots_assigned_cum, 0)
		- Hourly deltas (window functions, per (epoch, leader_identity_pubkey)):
			* leader_slots_assigned_delta = leader_slots_assigned_cum - LAG(leader_slots_assigned_cum) OVER (PARTITION BY epoch, leader_identity_pubkey ORDER BY time)
			* blocks_produced_delta = blocks_produced_cum - LAG(blocks_produced_cum) OVER (PARTITION BY epoch, leader_identity_pubkey ORDER BY time)
			* slots_skipped_delta = slots_skipped_cum - LAG(slots_skipped_cum) OVER (PARTITION BY epoch, leader_identity_pubkey ORDER BY time)

		CONSTRAINTS:
		- blocks_produced_cum <= leader_slots_assigned_cum
		- leader_slots_assigned_cum >= 0
		- blocks_produced_cum >= 0

		COLUMNS:
		- epoch (INTEGER): Solana blockchain epoch
		- time (TIMESTAMP): Collection time (UTC, hourly cadence)
		- leader_identity_pubkey (VARCHAR): Validator identity (leader) public key
		- leader_slots_assigned_cum (BIGINT): Cumulative leader slots assigned within epoch
		- blocks_produced_cum (BIGINT): Cumulative blocks produced within epoch

		FACT TABLE STRUCTURE:
		- Append-only: No updates or deletes, only inserts
		- Partitioned by: date(time) (year, month, day)
		- Ordered within partitions: (leader_identity_pubkey, time)
		- Grain: One row per (epoch, time, leader_identity_pubkey)

		USE CASES:
		- Live-ish skip monitoring during the current epoch
		- Identifying leaders with spiking skips hour-over-hour
		- Producing final epoch totals by taking the latest row per (epoch, leader_identity_pubkey)
		- Correlating production drops with vote lag/delinquency (join to vote activity) and gossip presence/version (join to gossip SCD)
		`,
	},
}
