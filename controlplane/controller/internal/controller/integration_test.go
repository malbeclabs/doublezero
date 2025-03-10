//go:build integration

package controller_test

import (
	"context"
	"log"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/malbeclabs/doublezero/controlplane/controller/internal/controller"
	dzsdk "github.com/malbeclabs/doublezero/smartcontract/sdk/go"

	"github.com/docker/docker/api/types"
	dockernetwork "github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	eapi "github.com/aristanetworks/goeapi"
)

type mockOnChainFetcher struct {
	Users   []dzsdk.User
	Devices []dzsdk.Device
}

func (m *mockOnChainFetcher) Load(context.Context) error {
	return nil
}

func (m *mockOnChainFetcher) GetDevices() []dzsdk.Device {
	return []dzsdk.Device{
		{
			AccountType:    dzsdk.AccountType(0),
			Owner:          [32]uint8{},
			LocationPubKey: [32]uint8{},
			ExchangePubKey: [32]uint8{},
			DeviceType:     0,
			PublicIp:       [4]uint8{2, 2, 2, 2},
			Status:         dzsdk.DeviceStatusActivated,
			Code:           "abc01",
			PubKey:         [32]byte{1},
		},
	}
}

func (m *mockOnChainFetcher) GetUsers() []dzsdk.User {
	return []dzsdk.User{
		{
			AccountType:  dzsdk.AccountType(0),
			Owner:        [32]uint8{},
			UserType:     dzsdk.UserUserType(dzsdk.UserTypeServer),
			DevicePubKey: [32]uint8{1},
			CyoaType:     dzsdk.CyoaTypeGREOverDIA,
			ClientIp:     [4]uint8{1, 1, 1, 1},
			DzIp:         [4]uint8{100, 100, 100, 100},
			TunnelId:     uint16(500),
			TunnelNet:    [5]uint8{169, 254, 0, 0, 31},
			Status:       dzsdk.UserStatusActivated,
		},
	}
}

func TestIntegrationController(t *testing.T) {

	lis, err := net.Listen("tcp", net.JoinHostPort("0.0.0.0", "7004"))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	ctl, err := controller.NewController(
		controller.WithAccountFetcher(&mockOnChainFetcher{}),
		controller.WithListener(lis),
	)
	if err != nil {
		t.Fatalf("error creating controller: %v", err)
	}

	ctx := context.Background()
	go func() {
		if err := ctl.Run(ctx); err != nil {
			log.Fatalf("error starting controller: %v", err)
		}
	}()
	defer ctx.Done()

	ipam := dockernetwork.IPAM{
		Driver: "default",
		Config: []dockernetwork.IPAMConfig{
			{
				Subnet:  "172.16.99.0/24",
				Gateway: "172.16.99.1",
			},
		},
		Options: map[string]string{
			"driver": "bridge",
		},
	}

	nc := dockernetwork.CreateOptions{
		Driver: "bridge",
		Labels: testcontainers.GenericLabels(),
	}

	netReq := testcontainers.NetworkRequest{
		Driver:     nc.Driver,
		Internal:   nc.Internal,
		EnableIPv6: nc.EnableIPv6,
		Name:       "1_mgmt",
		Labels:     nc.Labels,
		Attachable: nc.Attachable,
		IPAM:       &ipam,
	}
	n, err := testcontainers.GenericNetwork(ctx, testcontainers.GenericNetworkRequest{
		NetworkRequest: netReq,
	})
	if err != nil {
		t.Fatalf("could not create network: %v", err)
	}

	net1 := n.(*testcontainers.DockerNetwork)

	netReq = testcontainers.NetworkRequest{
		Driver:     nc.Driver,
		Internal:   nc.Internal,
		EnableIPv6: nc.EnableIPv6,
		Name:       "eth1",
		Labels:     nc.Labels,
		Attachable: nc.Attachable,
		IPAM:       nc.IPAM,
	}
	n, err = testcontainers.GenericNetwork(ctx, testcontainers.GenericNetworkRequest{
		NetworkRequest: netReq,
	})
	if err != nil {
		t.Fatalf("could not create network: %v", err)
	}

	net2 := n.(*testcontainers.DockerNetwork)

	testcontainers.CleanupNetwork(t, net1)
	testcontainers.CleanupNetwork(t, net2)

	tests := []struct {
		Name          string
		StartupConfig string
	}{
		{
			Name:          "verify_controller_configuration_is_applied",
			StartupConfig: "./controlplane/internal/pkg/controller/fixtures/integration/startup.config.base.txt",
		},
		{
			Name:          "verify_unknown_neighbors_are_removed",
			StartupConfig: "./controlplane/internal/pkg/controller/fixtures/integration/startup.config.unknown.neighbors.txt",
		},
	}
	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			eapiPort := "80/tcp"
			req := testcontainers.ContainerRequest{
				FromDockerfile: testcontainers.FromDockerfile{
					Context:    "../../../../.",
					Dockerfile: "controlplane/internal/pkg/controller/Dockerfile.test",
					KeepImage:  false,
					BuildOptionsModifier: func(options *types.ImageBuildOptions) {
						if len(options.BuildArgs) == 0 {
							options.BuildArgs = map[string]*string{"CONFIG": &test.StartupConfig}
							return
						}
						options.BuildArgs["CONFIG"] = &test.StartupConfig
					},
				},
				Networks:   []string{net1.Name, net2.Name}, // This is kind of a hack - testcontainers uses uuids for networks and we can't control ordering
				Privileged: true,                           // This is extremely important for a cEOS container to start
				EnpointSettingsModifier: func(m map[string]*dockernetwork.EndpointSettings) {
					m[net1.Name].IPAMConfig = &dockernetwork.EndpointIPAMConfig{
						IPv4Address: "172.16.99.2",
					}
				},
				ExposedPorts: []string{eapiPort},
				WaitingFor:   wait.ForListeningPort(nat.Port(eapiPort)),
			}

			container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
				ContainerRequest: req,
				Started:          true,
			})
			if err != nil {
				t.Fatalf("error creating container: %v", err)
			}

			testcontainers.CleanupContainer(t, container)

			port, err := container.MappedPort(ctx, nat.Port(eapiPort))
			if err != nil {
				t.Fatalf("could not get mapped eapi port of container: %v", err)
			}

			// TODO: this sucks; the listening port check above works but thats just nginx,
			// not the actual backend
			time.Sleep(15 * time.Second)

			dut, err := eapi.Connect("http", "localhost", "admin", "admin", port.Int())
			if err != nil {
				t.Fatalf("error connecting to dut: %v", err)
			}

			resp, err := dut.GetConfig("running-config", "", "text")
			if err != nil {
				t.Fatalf("error getting config from dut: %v", err)
			}

			config := strings.Split(resp["output"].(string), "\n")
			if len(config) < 4 {
				t.Fatalf("incomplete config fetched from device: %s", strings.Join(config, "\n"))
			}

			// we need to strip out the first 2 lines since they contain info that changes on every container boot
			got := strings.Join(config[2:], "\n")

			gold := "./fixtures/integration/integration.golden.txt"
			want, err := os.ReadFile(gold)
			if err != nil {
				t.Fatalf("error reading test fixture %s: %v", gold, err)
			}

			if diff := cmp.Diff(string(want), got); diff != "" {
				t.Errorf("config mismatch (-want +got): %s\n", diff)
			}
		})
	}
}
