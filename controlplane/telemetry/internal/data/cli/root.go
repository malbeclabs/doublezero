package cli

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/lmittmann/tint"
	"github.com/malbeclabs/doublezero/config"
	"github.com/spf13/cobra"
)

type ExitCode int

const (
	exitCodeSuccess = 0
	exitCodeError   = 1
)

func Run() ExitCode {
	rootCmd := &cobra.Command{
		Use:   "telemetry-data",
		Short: "Data CLI for DoubleZero onchain telemetry.",
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

	var env string
	rootCmd.PersistentFlags().StringVarP(&env, "env", "e", config.EnvDevnet, "The network environment to query (devnet, testnet)")

	rootCmd.AddCommand(
		NewDeviceCmd().Command(),
		NewInternetCmd().Command(),
	)

	if err := rootCmd.Execute(); err != nil {
		return exitCodeError
	}

	return exitCodeSuccess
}

func newLogger(verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	return slog.New(tint.NewHandler(os.Stdout, &tint.Options{
		Level:      level,
		TimeFormat: time.Kitchen,
	}))
}
