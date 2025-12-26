package sol

import (
	"context"

	sqltools "github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/tools/sql"
)

func (v *View) SchemaTool(ctx context.Context) (*sqltools.SchemaTool, error) {
	return sqltools.NewSchemaTool(ctx, sqltools.SchemaToolConfig{
		Logger: v.log,
		DB:     v.cfg.DB,
		Schema: SCHEMA,
	})
}

var SCHEMA = &sqltools.Schema{
	Name: "solana",
	Description: `
Use only for Solana-specific questions:
- gossip nodes
- vote accounts (validators)
- leader schedule

Terminology:
- Gossip nodes are all Solana network participants.
- A staked validator is a vote account with epoch_vote_account = true and activated_stake_lamports > 0.
Be explicit whether results refer to gossip nodes or staked validators.
`,
	Tables: []sqltools.TableInfo{
		{
			Name:        "solana_gossip_nodes",
			Description: "Solana gossip participants (includes validators and non-validators). Join to vote accounts: solana_gossip_nodes.pubkey = solana_vote_accounts.node_pubkey. Can map to DZ users via solana_gossip_nodes.gossip_ip = dz_users.dz_ip.",
			Columns: []sqltools.ColumnInfo{
				{Name: "snapshot_timestamp", Type: "TIMESTAMP", Description: "Snapshot timestamp"},
				{Name: "current_epoch", Type: "INTEGER", Description: "Epoch at snapshot time"},
				{Name: "pubkey", Type: "VARCHAR", Description: "Node public key"},
				{Name: "gossip_ip", Type: "VARCHAR", Description: "Gossip IP address"},
				{Name: "gossip_port", Type: "INTEGER", Description: "Gossip port"},
				{Name: "tpuquic_ip", Type: "VARCHAR", Description: "TPU-QUIC IP address"},
				{Name: "tpuquic_port", Type: "INTEGER", Description: "TPU-QUIC port"},
				{Name: "version", Type: "VARCHAR", Description: "Solana client version"},
			},
		},
		{
			Name:        "solana_vote_accounts",
			Description: "Solana vote accounts (validators). Join: solana_vote_accounts.node_pubkey = solana_gossip_nodes.pubkey. Filter for staked validators using epoch_vote_account = true AND activated_stake_lamports > 0.",
			Columns: []sqltools.ColumnInfo{
				{Name: "snapshot_timestamp", Type: "TIMESTAMP", Description: "Snapshot timestamp"},
				{Name: "current_epoch", Type: "INTEGER", Description: "Epoch at snapshot time"},
				{Name: "vote_pubkey", Type: "VARCHAR", Description: "Vote account public key"},
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
			Description: "Leader schedule for an epoch. Join: solana_leader_schedule.node_pubkey = solana_vote_accounts.node_pubkey.",
			Columns: []sqltools.ColumnInfo{
				{Name: "snapshot_timestamp", Type: "TIMESTAMP", Description: "Snapshot timestamp"},
				{Name: "current_epoch", Type: "INTEGER", Description: "Epoch at snapshot time"},
				{Name: "node_pubkey", Type: "VARCHAR", Description: "Node public key"},
				{Name: "slots", Type: "INTEGER[]", Description: "Slots where the node is leader"},
				{Name: "slot_count", Type: "INTEGER", Description: "Number of leader slots"},
			},
		},
	},
}
