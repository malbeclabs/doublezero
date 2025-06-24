package devnetcmd

import (
	"context"
	"fmt"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/spf13/cobra"
)

type StartCmd struct{}

func NewStartCmd() *StartCmd {
	return &StartCmd{}
}

func (c *StartCmd) Command() *cobra.Command {
	var noBuild bool

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the core devnet components; ledger, manager, activator, and controller",
		RunE: withDevnet(func(ctx context.Context, dn *LocalDevnet, cmd *cobra.Command, args []string) error {
			verbose, err := cmd.Root().PersistentFlags().GetBool("verbose")
			if err != nil {
				return fmt.Errorf("failed to get verbose flag: %w", err)
			}
			var buildConfig *devnet.BuildConfig
			if !noBuild {
				buildConfig = &devnet.BuildConfig{
					Verbose: verbose,
				}
			}
			return dn.Start(ctx, buildConfig)
		}),
	}

	cmd.Flags().BoolVar(&noBuild, "no-build", false, "Don't build the docker images")

	return cmd
}
