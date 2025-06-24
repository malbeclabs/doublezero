package devnetcmd

import (
	"context"
	"fmt"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/spf13/cobra"
)

type BuildCmd struct{}

func NewBuildCmd() *BuildCmd {
	return &BuildCmd{}
}

func (c *BuildCmd) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build the docker images. This may take a minute or two",
		RunE: withDevnet(func(ctx context.Context, dn *LocalDevnet, cmd *cobra.Command, args []string) error {
			verbose, err := cmd.Root().PersistentFlags().GetBool("verbose")
			if err != nil {
				return fmt.Errorf("failed to get verbose flag: %w", err)
			}
			return devnet.BuildContainerImages(ctx, dn.log, dn.workspaceDir, verbose)
		}),
	}

	return cmd
}
