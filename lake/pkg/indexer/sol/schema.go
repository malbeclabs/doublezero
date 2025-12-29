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
		- last_vote_slot (BIGINT): Slot of last vote
		- root_slot (BIGINT): Last rooted slot

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
}
