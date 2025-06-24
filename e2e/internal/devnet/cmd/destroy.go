package devnetcmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/malbeclabs/doublezero/e2e/internal/devnet"
	"github.com/spf13/cobra"
)

type DestroyCmd struct {
	all         bool
	skipConfirm bool
}

func NewDestroyCmd() *DestroyCmd {
	return &DestroyCmd{}
}

func (c *DestroyCmd) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "destroy",
		Short: "Destroy the devnet and all its resources",
		RunE: withDevnet(func(ctx context.Context, dn *LocalDevnet, cmd *cobra.Command, args []string) error {
			if !c.skipConfirm && !c.confirmDestroy(ctx, dn.Spec.DeployID) {
				fmt.Println("--> Destroy operation cancelled.")
				return nil
			}
			return dn.Destroy(ctx, c.all)
		}),
	}
	cmd.Flags().BoolVar(&c.all, "all", false, fmt.Sprintf("Destroy all resources matching %q", devnet.LabelsFilterTypeDevnet))
	cmd.Flags().BoolVarP(&c.skipConfirm, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func (c *DestroyCmd) confirmDestroy(ctx context.Context, deployID string) bool {
	scope := fmt.Sprintf("the current devnet (%s)", deployID)
	if c.all {
		scope = "all devnet resources"
	}

	fmt.Printf("==> ⚠️ Are you sure you want to destroy %s? (y/N): ", scope)

	// Simple input with context cancellation
	done := make(chan bool, 1)
	go func() {
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		done <- strings.TrimSpace(strings.ToLower(response)) == "y" || strings.TrimSpace(strings.ToLower(response)) == "yes"
	}()

	select {
	case confirmed := <-done:
		return confirmed
	case <-ctx.Done():
		fmt.Println()
		return false
	}
}
