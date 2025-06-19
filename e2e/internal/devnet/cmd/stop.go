package devnetcmd

import (
	"context"

	"github.com/spf13/cobra"
)

type StopCmd struct{}

func NewStopCmd() *StopCmd {
	return &StopCmd{}
}

func (c *StopCmd) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop all components in the devnet, including devices and clients",
		RunE: withDevnet(func(ctx context.Context, dn *LocalDevnet, cmd *cobra.Command, args []string) error {
			return dn.Stop(ctx)
		}),
	}
	return cmd
}
