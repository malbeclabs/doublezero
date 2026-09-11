package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"sort"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/awsreach"
	collector "github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/collector"
)

const (
	defaultNodeFilePath  = "config/nodes-aws.json"
	nodeFileHTTPTimeout  = 30 * time.Second
	nodeFilePingAttempts = 8
)

var (
	nodeFilePath     string
	nodeFileSkipPing bool
)

var nodefileCmd = &cobra.Command{
	Use:   "nodefile",
	Short: "Generate and verify the cloud region node file",
	Long: `Commands for maintaining the node file that lists cloud regions, their pinned
RIPE Atlas probe IDs, and the address other regions ping to reach them.`,
	// These commands read no ledger state, so they replace the root command's network config setup.
	PersistentPreRun: func(cmd *cobra.Command, args []string) {},
}

var nodefileGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Resolve a ping target for every node from the AWS published address list",
	Run: func(cmd *cobra.Command, args []string) {
		log := collector.NewLogger(collector.LogLevel(logLevel))

		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()

		nodes, targets, ok := loadNodesAndTargets(ctx, log)
		if !ok {
			os.Exit(1)
		}

		var problems []string
		for i, node := range nodes {
			candidates, published := targets[node.Code]
			if !published {
				problems = append(problems, fmt.Sprintf("%s: region absent from the AWS address list", node.Code))
				continue
			}

			chosen, err := chooseNodeTarget(ctx, candidates, node.PingTarget)
			if err != nil {
				if ctx.Err() != nil {
					log.Info("Operation cancelled by signal")
					return
				}
				problems = append(problems, fmt.Sprintf("%s: no published address answered: %s", node.Code, err.Error()))
				continue
			}

			if chosen != node.PingTarget {
				log.Warn("Ping target changed",
					slog.String("code", node.Code),
					slog.String("old_target", node.PingTarget),
					slog.String("new_target", chosen))
			}
			nodes[i].PingTarget = chosen
		}

		if len(problems) > 0 {
			for _, problem := range problems {
				log.Error("Node file generation failed", slog.String("problem", problem))
			}
			os.Exit(1)
		}

		if err := writeNodeFile(nodeFilePath, nodes); err != nil {
			log.Error("Operation failed: write_node_file", slog.String("error", err.Error()))
			os.Exit(1)
		}

		log.Info("Operation completed: generate_node_file",
			slog.String("file", nodeFilePath),
			slog.Int("nodes", len(nodes)),
			slog.Bool("skipped_ping", nodeFileSkipPing))
	},
}

var nodefileVerifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Check every ping target is still published by AWS and still answers",
	Run: func(cmd *cobra.Command, args []string) {
		log := collector.NewLogger(collector.LogLevel(logLevel))

		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()

		nodes, targets, ok := loadNodesAndTargets(ctx, log)
		if !ok {
			os.Exit(1)
		}

		var problems []string
		for _, node := range nodes {
			candidates, published := targets[node.Code]
			if !published {
				problems = append(problems, fmt.Sprintf("%s: region absent from the AWS address list", node.Code))
				continue
			}
			if !slices.Contains(candidates, node.PingTarget) {
				problems = append(problems, fmt.Sprintf("%s: %s is no longer published", node.Code, node.PingTarget))
				continue
			}
			if nodeFileSkipPing {
				continue
			}
			if !awsreach.SystemPing(ctx, node.PingTarget) {
				problems = append(problems, fmt.Sprintf("%s: %s did not answer", node.Code, node.PingTarget))
			}
		}

		if ctx.Err() != nil {
			log.Info("Operation cancelled by signal")
			return
		}

		if len(problems) > 0 {
			for _, problem := range problems {
				log.Error("Node file check failed", slog.String("problem", problem))
			}
			os.Exit(1)
		}

		log.Info("Operation completed: verify_node_file",
			slog.String("file", nodeFilePath),
			slog.Int("nodes", len(nodes)),
			slog.Bool("skipped_ping", nodeFileSkipPing))
	},
}

func loadNodesAndTargets(ctx context.Context, log *slog.Logger) ([]collector.JSONNode, awsreach.RegionTargets, bool) {
	if !nodeFileSkipPing {
		if _, err := exec.LookPath("ping"); err != nil {
			log.Error("Operation failed: ping_binary_missing", slog.String("error", err.Error()))
			return nil, nil, false
		}
	}

	nodes, err := collector.LoadNodesFromJSON(log, nodeFilePath)
	if err != nil {
		log.Error("Operation failed: load_node_file",
			slog.String("file", nodeFilePath),
			slog.String("error", err.Error()))
		return nil, nil, false
	}

	targets, err := awsreach.FetchPrefixes(ctx, &http.Client{Timeout: nodeFileHTTPTimeout}, awsreach.PrefixesURL)
	if err != nil {
		log.Error("Operation failed: fetch_aws_prefixes", slog.String("error", err.Error()))
		return nil, nil, false
	}

	return nodes, targets, true
}

func chooseNodeTarget(ctx context.Context, candidates []string, current string) (string, error) {
	ordered := awsreach.PreferFirst(candidates, current)
	if len(ordered) == 0 {
		return "", fmt.Errorf("no published addresses to choose from")
	}
	if nodeFileSkipPing {
		return ordered[0], nil
	}
	return awsreach.FirstAnswering(ctx, ordered, awsreach.SystemPing, nodeFilePingAttempts)
}

// writeNodeFile sorts nodes by code so a regenerated file differs only where a value changed.
func writeNodeFile(path string, nodes []collector.JSONNode) error {
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Code < nodes[j].Code })

	data, err := json.MarshalIndent(nodes, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode node file: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write node file: %w", err)
	}

	return nil
}
