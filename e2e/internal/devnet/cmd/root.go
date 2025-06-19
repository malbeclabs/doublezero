package devnetcmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

type ExitCode int

type ContextKey string

const (
	contextKeyLog ContextKey = "log"
)

const (
	exitCodeSuccess = 0
	exitCodeError   = 1
)

func Run() ExitCode {
	rootCmd := &cobra.Command{
		Use:   "devnet",
		Short: "Run a persistent local DoubleZero devnet locally in containers.",
		RunE: func(cmd *cobra.Command, args []string) error {
			err := cmd.Help()
			if err != nil {
				return fmt.Errorf("failed to show help: %w", err)
			}
			return nil
		},
	}

	var verbose bool
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "set debug logging level")

	var deployID string
	rootCmd.PersistentFlags().StringVar(&deployID, "deploy-id", envWithDefault("DZ_DEPLOY_ID", defaultDeployID), "deploy identifier (env: DZ_DEPLOY_ID, default: "+defaultDeployID+")")

	rootCmd.AddCommand(
		NewBuildCmd().Command(),
		NewStartCmd().Command(),
		NewStopCmd().Command(),
		NewDestroyCmd().Command(),
		NewAddDeviceCmd().Command(),
		NewAddClientCmd().Command(),
		NewDeployProgramsCmd().Command(),
		NewStartLedgerCmd().Command(),
	)

	if err := rootCmd.Execute(); err != nil {
		return exitCodeError
	}

	return exitCodeSuccess
}

func withDevnet(f func(ctx context.Context, dn *LocalDevnet, cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()

		verbose, err := cmd.Root().PersistentFlags().GetBool("verbose")
		if err != nil {
			return fmt.Errorf("failed to get verbose flag: %w", err)
		}
		log := newLogger(verbose)
		ctx = context.WithValue(ctx, contextKeyLog, log)

		deployID, err := cmd.Root().PersistentFlags().GetString("deploy-id")
		if err != nil {
			return fmt.Errorf("failed to get deploy-id flag: %w", err)
		}

		dn, err := NewLocalDevnet(log, deployID)
		if err != nil {
			return fmt.Errorf("failed to create devnet: %w", err)
		}

		err = f(ctx, dn, cmd, args)
		if err != nil {
			log.Error("failed to run command", "error", err)
			return err
		}

		return err
	}
}

func envWithDefault(envVar, defaultValue string) string {
	if value := os.Getenv(envVar); value != "" {
		return value
	}
	return defaultValue
}
