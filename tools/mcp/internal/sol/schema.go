package sol

import sqltools "github.com/malbeclabs/doublezero/tools/mcp/internal/tools/sql"

func (v *View) SchemaTool() (*sqltools.SchemaTool, error) {
	return sqltools.NewSchemaTool(sqltools.SchemaToolConfig{
		Logger: v.log,
		DB:     v.cfg.DB,
		Schema: SCHEMA,
	})
}

var SCHEMA = &sqltools.Schema{
	Name: "solana",
	Description: `
		Use this dataset **only for Solana-specific questions**:
		- Gossip nodes
		- Vote accounts
		- Leader schedules
		- Validator participation

		TERMINOLOGY:
		- Gossip nodes include all Solana network participants.
		- Validators are nodes with 'activated_stake > 0' and active vote accounts.
		- To count actual validators, join gossip nodes to vote accounts and filter accordingly.

		Be explicit about whether results refer to gossip nodes or staked validators.
	`,
	Tables: []sqltools.TableInfo{
		{
			Name:        "solana_gossip_nodes",
			Description: "Solana gossip nodes. These are not validators, but are part of the solana gossip network. Join to solana_vote_accounts.node_pubkey to get the vote account associated with the gossip node.",
			Columns: []sqltools.ColumnInfo{
				{Name: "snapshot_timestamp", Type: "TIMESTAMP", Description: "Snapshot timestamp"},
				{Name: "current_epoch", Type: "INTEGER", Description: "Current epoch, associated with the snapshot timestamp"},
				{Name: "pubkey", Type: "VARCHAR", Description: "Public key of the gossip node. join to solana_vote_accounts.node_pubkey."},
				{Name: "gossip_ip", Type: "VARCHAR", Description: "Gossip IP address. Join to dz_users.dz_ip to get the user associated with the solana gossip node."},
				{Name: "gossip_port", Type: "INTEGER", Description: "Gossip port"},
				{Name: "tpuquic_ip", Type: "VARCHAR", Description: "TPU-QUIC IP address"},
				{Name: "tpuquic_port", Type: "INTEGER", Description: "TPU-QUIC port"},
				{Name: "version", Type: "VARCHAR", Description: "The Solana client version running on the gossip node."},
			},
		},
		{
			Name:        "solana_vote_accounts",
			Description: "Solana vote accounts. These are solana validators. Join to solana_gossip_nodes.pubkey to get the gossip node associated with the vote account.",
			Columns: []sqltools.ColumnInfo{
				{Name: "snapshot_timestamp", Type: "TIMESTAMP", Description: "Snapshot timestamp"},
				{Name: "current_epoch", Type: "INTEGER", Description: "Current epoch, associated with the snapshot timestamp"},
				{Name: "vote_pubkey", Type: "VARCHAR", Description: "Vote public key The unique identifier for a validator (vote account)."},
				{Name: "node_pubkey", Type: "VARCHAR", Description: "Node public key. Join to solana_gossip_nodes.pubkey to get the gossip node associated with the vote account."},
				{Name: "activated_stake_lamports", Type: "BIGINT", Description: "Activated stake (lamports). There are 1_000_000_000 lamports in 1 SOL."},
				{Name: "epoch_vote_account", Type: "BOOLEAN", Description: "Whether the vote account is staked for this epoch."},
				{Name: "commission_percentage", Type: "INTEGER", Description: "Commission percentage. The percentage of rewards payout owed to the vote account."},
				{Name: "last_vote_slot", Type: "BIGINT", Description: "Last vote slot. The slot of the last vote cast by the vote account."},
				{Name: "root_slot", Type: "BIGINT", Description: "Root slot. The slot of the last root block processed by the vote account."},
			},
		},
		{
			Name:        "solana_leader_schedule",
			Description: "Solana leader schedule. These are the validators that are leaders for given slots in an epoch. Join to solana_vote_accounts.node_pubkey to get the vote account associated with the leader schedule.",
			Columns: []sqltools.ColumnInfo{
				{Name: "snapshot_timestamp", Type: "TIMESTAMP", Description: "Snapshot timestamp"},
				{Name: "current_epoch", Type: "INTEGER", Description: "Current epoch, associated with the snapshot timestamp"},
				{Name: "node_pubkey", Type: "VARCHAR", Description: "Node public key. Join to solana_vote_accounts.node_pubkey to get the vote account associated with the leader schedule."},
				{Name: "slots", Type: "INTEGER[]", Description: "The slots that the node is leader for."},
				{Name: "slot_count", Type: "INTEGER", Description: "The number of slots that the node is leader for. Can be used to calcluate how often the node is leader."},
			},
		},
	},
}
