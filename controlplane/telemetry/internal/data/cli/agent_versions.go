package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	devicedata "github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/device"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

type AgentVersionsCmd struct{}

func NewAgentVersionsCmd() *AgentVersionsCmd {
	return &AgentVersionsCmd{}
}

func (c *AgentVersionsCmd) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent-versions",
		Short: "Show telemetry agent version for each device",
		RunE: func(cmd *cobra.Command, args []string) error {
			verbose, err := cmd.Root().PersistentFlags().GetBool("verbose")
			if err != nil {
				return fmt.Errorf("failed to get verbose flag: %w", err)
			}
			env, err := cmd.Root().PersistentFlags().GetString("env")
			if err != nil {
				return fmt.Errorf("failed to get env flag: %w", err)
			}

			log := newLogger(verbose)

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			provider, _, err := newDeviceProvider(log, env)
			if err != nil {
				log.Error("Failed to get provider", "error", err)
				os.Exit(1)
			}

			versions, err := provider.GetAgentVersions(ctx)
			if err != nil {
				log.Error("Failed to get agent versions", "error", err)
				os.Exit(1)
			}

			printAgentVersions(versions, env)
			return nil
		},
	}

	return cmd
}

func printAgentVersions(versions []devicedata.DeviceAgentVersion, env string) {
	fmt.Println("Environment:", env)
	fmt.Printf("Devices reporting: %d\n", len(versions))

	table := tablewriter.NewWriter(os.Stdout)
	table.SetAutoWrapText(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_CENTER)
	table.SetAutoFormatHeaders(false)
	table.SetBorder(true)
	table.SetRowLine(true)
	table.SetHeader([]string{
		"Device PK",
		"Device Code",
		"Version",
		"Commit",
		"Last Sample",
	})

	for _, v := range versions {
		table.Append([]string{
			v.DevicePK,
			v.DeviceCode,
			v.Version,
			v.Commit,
			v.Timestamp,
		})
	}
	table.Render()
}
