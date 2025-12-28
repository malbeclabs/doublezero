package sol

import (
	schematypes "github.com/malbeclabs/doublezero/lake/pkg/indexer/schema"
)

var Schema = &schematypes.Schema{
	Name: "solana",
	Description: `
Use only for Solana-specific questions:
- gossip nodes
- vote accounts (validators)
- leader schedule

SCD2 TABLE STRUCTURE (CRITICAL):
All tables in this schema use SCD2 (Slowly Changing Dimension Type 2) pattern:
- {table}_current: One row per primary key with the most recent version. Contains: primary key columns, payload columns, as_of_ts (timestamp of snapshot), row_hash (hash for change detection).
- {table}_history: Append-only historical versions with validity windows. Contains: primary key columns, payload columns, valid_from (timestamp), valid_to (timestamp, NULL for current), row_hash, op (I|U|D), run_id (optional).
- Always query {table}_current for current state. Query {table}_history only for historical analysis or point-in-time queries.
- All joins should use {table}_current tables unless explicitly doing historical analysis.

TERMINOLOGY:
- Gossip nodes are all Solana network participants.
- A staked validator is a vote account with epoch_vote_account = true and activated_stake_lamports > 0.

RULES:
- Convert lamports to SOL as: lamports / 1e9.
- Report stake in SOL, not lamports.

Be explicit whether results refer to gossip nodes or staked validators.
`,
	Tables: []schematypes.TableInfo{
		{
			Name:        "solana_gossip_nodes",
			Description: "Solana gossip participants (includes validators and non-validators) (SCD2). STRUCTURE: solana_gossip_nodes_current (one row per pubkey with latest version + as_of_ts, row_hash) and solana_gossip_nodes_history (historical versions with valid_from, valid_to, op, run_id). USAGE: Always query solana_gossip_nodes_current for current state. Join to vote accounts: solana_gossip_nodes_current.pubkey = solana_vote_accounts_current.node_pubkey. Can map to DZ users via solana_gossip_nodes_current.gossip_ip = dz_users_current.dz_ip.",
			Columns: []schematypes.ColumnInfo{
				{Name: "as_of_ts", Type: "TIMESTAMP", Description: "Timestamp of the snapshot that produced this row (SCD2, in _current table only)"},
				{Name: "row_hash", Type: "VARCHAR", Description: "Hash of payload columns for change detection (SCD2, in _current table only)"},
				{Name: "epoch", Type: "INTEGER", Description: "Solana blockchain epoch at snapshot time"},
				{Name: "pubkey", Type: "VARCHAR", Description: "Node public key (primary key)"},
				{Name: "gossip_ip", Type: "VARCHAR", Description: "Gossip IP address"},
				{Name: "gossip_port", Type: "INTEGER", Description: "Gossip port"},
				{Name: "tpuquic_ip", Type: "VARCHAR", Description: "TPU-QUIC IP address"},
				{Name: "tpuquic_port", Type: "INTEGER", Description: "TPU-QUIC port"},
				{Name: "version", Type: "VARCHAR", Description: "Solana client version"},
			},
		},
		{
			Name:        "solana_vote_accounts",
			Description: "Solana vote accounts (validators) (SCD2). STRUCTURE: solana_vote_accounts_current (one row per vote_pubkey with latest version + as_of_ts, row_hash) and solana_vote_accounts_history (historical versions with valid_from, valid_to, op, run_id). USAGE: Always query solana_vote_accounts_current for current state. Join: solana_vote_accounts_current.node_pubkey = solana_gossip_nodes_current.pubkey. Filter for staked validators using epoch_vote_account = true AND activated_stake_lamports > 0.",
			Columns: []schematypes.ColumnInfo{
				{Name: "as_of_ts", Type: "TIMESTAMP", Description: "Timestamp of the snapshot that produced this row (SCD2, in _current table only)"},
				{Name: "row_hash", Type: "VARCHAR", Description: "Hash of payload columns for change detection (SCD2, in _current table only)"},
				{Name: "epoch", Type: "INTEGER", Description: "Solana blockchain epoch at snapshot time"},
				{Name: "vote_pubkey", Type: "VARCHAR", Description: "Vote account public key (primary key)"},
				{Name: "node_pubkey", Type: "VARCHAR", Description: "Node public key"},
				{Name: "activated_stake_lamports", Type: "BIGINT", Description: "Activated stake (lamports)"},
				{Name: "epoch_vote_account", Type: "BOOLEAN", Description: "Vote account active for this epoch"},
				{Name: "commission_percentage", Type: "INTEGER", Description: "Commission percentage"},
				{Name: "last_vote_slot", Type: "BIGINT", Description: "Slot of last vote"},
				{Name: "root_slot", Type: "BIGINT", Description: "Last rooted slot"},
			},
		},
		{
			Name:        "solana_leader_schedule",
			Description: "Leader schedule for an epoch (SCD2). STRUCTURE: solana_leader_schedule_current (one row per node_pubkey with latest version + as_of_ts, row_hash) and solana_leader_schedule_history (historical versions with valid_from, valid_to, op, run_id). USAGE: Always query solana_leader_schedule_current for current state. Join: solana_leader_schedule_current.node_pubkey = solana_vote_accounts_current.node_pubkey.",
			Columns: []schematypes.ColumnInfo{
				{Name: "as_of_ts", Type: "TIMESTAMP", Description: "Timestamp of the snapshot that produced this row (SCD2, in _current table only)"},
				{Name: "row_hash", Type: "VARCHAR", Description: "Hash of payload columns for change detection (SCD2, in _current table only)"},
				{Name: "epoch", Type: "INTEGER", Description: "Solana blockchain epoch at snapshot time"},
				{Name: "node_pubkey", Type: "VARCHAR", Description: "Node public key (primary key)"},
				{Name: "slots", Type: "VARCHAR", Description: "Slots where the node is leader (stored as VARCHAR in SCD2)"},
				{Name: "slot_count", Type: "INTEGER", Description: "Number of leader slots"},
			},
		},
	},
}
