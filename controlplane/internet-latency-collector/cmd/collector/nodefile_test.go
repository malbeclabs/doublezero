package main

import (
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"

	collector "github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/collector"
	"github.com/stretchr/testify/require"
)

func TestInternetLatency_NodeFile_CommittedAWSNodes(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.DiscardHandler)

	nodes, err := collector.LoadNodesFromJSON(log, "../../config/nodes-aws.json")
	require.NoError(t, err)
	require.Len(t, nodes, 17, "17 regions give 136 pairs")

	expected := map[string]struct {
		probeIDs []int
		target   string
	}{
		"us-east-1":      {[]int{1003385, 1009925, 1010723, 1012092}, "34.192.0.54"},
		"us-east-2":      {[]int{1003386, 1000074, 1005330, 1015708}, "3.130.0.254"},
		"us-west-1":      {[]int{1003387, 1013400}, "13.52.0.0"},
		"us-west-2":      {[]int{1003388, 1005331, 1007744, 1012207}, "35.95.2.254"},
		"ca-central-1":   {[]int{1003389, 1005332, 1015534}, "3.98.0.0"},
		"sa-east-1":      {[]int{1000709, 1002617, 1015704}, "15.228.0.0"},
		"eu-west-1":      {[]int{1003378, 1002616, 1010727, 1012211}, "3.248.0.0"},
		"eu-west-2":      {[]int{1003377, 1005333, 1009922, 1015778}, "3.8.0.0"},
		"eu-west-3":      {[]int{1003375, 1016689}, "13.36.0.0"},
		"eu-central-1":   {[]int{1000566, 1005334, 1015777, 1016525}, "3.64.0.0"},
		"eu-north-1":     {[]int{1003374, 1005867}, "13.50.0.254"},
		"eu-south-2":     {[]int{1004991, 1016435}, "15.216.0.0"},
		"ap-northeast-1": {[]int{1003384, 1010741, 1012762, 1013401}, "3.112.0.0"},
		"ap-northeast-2": {[]int{1002619, 1015781, 1017320}, "13.209.0.0"},
		"ap-east-1":      {[]int{1012347, 1012349, 1012350, 1012351}, "16.162.0.253"},
		"ap-southeast-1": {[]int{1003382, 1002618, 1012208, 1015779}, "3.0.0.9"},
		"ap-south-1":     {[]int{1003379}, "3.6.0.0"},
	}

	seen := map[string]bool{}
	for i, node := range nodes {
		want, ok := expected[node.Code]
		require.True(t, ok, "unexpected region %s", node.Code)

		require.Equal(t, "aws", node.Cloud, "%s cloud", node.Code)
		require.Equal(t, want.probeIDs, node.AtlasProbeIDs, "%s probe ids", node.Code)
		require.Equal(t, want.target, node.PingTarget, "%s ping target", node.Code)
		require.NotNil(t, net.ParseIP(node.PingTarget).To4(), "%s ping target must be IPv4", node.Code)
		require.NotZero(t, node.Latitude, "%s latitude", node.Code)
		require.NotZero(t, node.Longitude, "%s longitude", node.Code)

		if i > 0 {
			require.Less(t, nodes[i-1].Code, node.Code, "the node file must be sorted by code")
		}

		seen[node.Code] = true
	}

	require.Len(t, seen, len(expected), "every expected region must be present exactly once")
}

func TestInternetLatency_NodeFile_WriteMatchesCommittedFile(t *testing.T) {
	t.Parallel()

	committed, err := os.ReadFile("../../config/nodes-aws.json")
	require.NoError(t, err)

	nodes, err := collector.LoadNodesFromJSON(slog.New(slog.DiscardHandler), "../../config/nodes-aws.json")
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "nodes.json")
	slices.Reverse(nodes)
	require.NoError(t, writeNodeFile(path, nodes))

	written, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(committed), string(written),
		"generate must rewrite an unchanged node file byte for byte")
}

func TestInternetLatency_NodeFile_ChooseNodeTargetSkipPing(t *testing.T) {
	previous := nodeFileSkipPing
	nodeFileSkipPing = true
	defer func() { nodeFileSkipPing = previous }()

	chosen, err := chooseNodeTarget(t.Context(), []string{"1.1.1.1", "2.2.2.2"}, "2.2.2.2")
	require.NoError(t, err)
	require.Equal(t, "2.2.2.2", chosen, "the address already in the file is kept when it is still published")

	_, err = chooseNodeTarget(t.Context(), nil, "2.2.2.2")
	require.Error(t, err)
}
