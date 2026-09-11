package main

import (
	"log/slog"
	"testing"

	collector "github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/collector"
	"github.com/stretchr/testify/require"
)

func TestInternetLatency_Cloud_LoadCloudNodesCarriesEveryField(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.DiscardHandler)

	jsonNodes, err := collector.LoadNodesFromJSON(log, "../../config/nodes-aws.json")
	require.NoError(t, err)

	nodes, err := loadCloudNodes(log, "../../config/nodes-aws.json")
	require.NoError(t, err)
	require.Len(t, nodes, len(jsonNodes))

	for i, node := range nodes {
		require.Equal(t, jsonNodes[i].Code, node.Code)
		require.Equal(t, jsonNodes[i].Cloud, node.Cloud)
		require.Equal(t, jsonNodes[i].Latitude, node.Latitude)
		require.Equal(t, jsonNodes[i].Longitude, node.Longitude)
		require.Equal(t, jsonNodes[i].AtlasProbeIDs, node.AtlasProbeIDs)
		require.Equal(t, jsonNodes[i].PingTarget, node.PingTarget)
	}
}

func TestInternetLatency_Cloud_NodeFilePath(t *testing.T) {
	previous := cloudNodeFile
	defer func() { cloudNodeFile = previous }()

	cloudNodeFile = ""
	require.Empty(t, cloudNodeFilePath(), "cloud mode stays off when neither the flag nor the environment names a file")

	t.Setenv(cloudNodeFileEnvVar, "from-env.json")
	require.Equal(t, "from-env.json", cloudNodeFilePath())

	cloudNodeFile = "from-flag.json"
	require.Equal(t, "from-flag.json", cloudNodeFilePath(), "the flag wins over the environment")
}
