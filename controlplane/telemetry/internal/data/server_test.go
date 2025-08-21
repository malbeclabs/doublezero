package data_test

import (
	"encoding/json"
	"net"
	"net/http"
	"testing"

	"github.com/malbeclabs/doublezero/config"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func startTestServer(t *testing.T, cfg data.ServerConfig) (addr string, closeFn func()) {
	t.Helper()

	srv, err := data.NewServer(&cfg)
	require.NoError(t, err)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() {
		_ = srv.Serve(t.Context(), ln)
	}()

	return "http://" + ln.Addr().String(), func() {
		_ = ln.Close()
	}
}

func TestServer_envs(t *testing.T) {
	addr, closeFn := startTestServer(t, data.ServerConfig{
		Logger:                      logger,
		MainnetInternetDataProvider: &mockInternetProvider{},
		MainnetDeviceDataProvider:   &mockDeviceProvider{},
		TestnetInternetDataProvider: &mockInternetProvider{},
		TestnetDeviceDataProvider:   &mockDeviceProvider{},
		DevnetInternetDataProvider:  &mockInternetProvider{},
		DevnetDeviceDataProvider:    &mockDeviceProvider{},
	})
	defer closeFn()

	res, err := http.Get(addr + "/envs")
	require.NoError(t, err)
	defer res.Body.Close()

	assert.Equal(t, http.StatusOK, res.StatusCode)

	var envs []string
	require.NoError(t, json.NewDecoder(res.Body).Decode(&envs))
	assert.ElementsMatch(t, []string{config.EnvMainnetBeta, config.EnvTestnet, config.EnvDevnet}, envs)
}
