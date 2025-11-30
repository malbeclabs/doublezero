package solmon

import (
	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
)

type Validator struct {
	Pubkey solana.PublicKey

	Node        *solanarpc.GetClusterNodesResult
	VoteAccount *solanarpc.VoteAccountsResult
	LeaderSlots []uint64
}
