package arista_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"strings"
	"testing"

	"github.com/malbeclabs/doublezero/controlplane/agent/pkg/arista"
	aristapb "github.com/malbeclabs/doublezero/controlplane/proto/arista/gen/pb-go/arista/EosSdkRpc"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestArista_GetLocalTunnelTargetIPs(t *testing.T) {
	tests := []struct {
		name      string
		clientFn  func() *arista.MockEAPIClient
		wantIPs   []net.IP
		expectErr string
	}{
		{
			name: "successful 31 masklen interface",
			clientFn: func() *arista.MockEAPIClient {
				resp := arista.IPInterfacesBriefResponse{
					Interfaces: map[string]arista.IPInterfaceBrief{
						"Tunnel1": {
							Name:               "Tunnel1",
							InterfaceStatus:    arista.IPInterfaceInterfaceStatusConnected,
							LineProtocolStatus: arista.IPInterfaceLineProtocolStatusUp,
							InterfaceAddress: arista.IPInterfaceAddress{
								IPAddr: arista.IPInterfaceAddressIPAddr{
									Address: "172.16.0.1", MaskLen: 31,
								},
							},
						},
					},
				}
				j, _ := json.Marshal(resp)
				return &arista.MockEAPIClient{
					RunShowCmdFunc: func(_ context.Context, _ *aristapb.RunShowCmdRequest, _ ...grpc.CallOption) (*aristapb.RunShowCmdResponse, error) {
						return &aristapb.RunShowCmdResponse{
							Response: &aristapb.EapiResponse{
								Success:   true,
								Responses: []string{string(j)},
							},
						}, nil
					},
				}
			},
			wantIPs: []net.IP{net.ParseIP("172.16.0.0").To4()},
		},
		{
			name: "successful interface not connected but up",
			clientFn: func() *arista.MockEAPIClient {
				resp := arista.IPInterfacesBriefResponse{
					Interfaces: map[string]arista.IPInterfaceBrief{
						"Tunnel1": {
							Name:               "Tunnel1",
							InterfaceStatus:    "not-connected",
							LineProtocolStatus: arista.IPInterfaceLineProtocolStatusUp,
							InterfaceAddress: arista.IPInterfaceAddress{
								IPAddr: arista.IPInterfaceAddressIPAddr{
									Address: "172.16.0.1", MaskLen: 31,
								},
							},
						},
					},
				}
				j, _ := json.Marshal(resp)
				return &arista.MockEAPIClient{
					RunShowCmdFunc: func(_ context.Context, _ *aristapb.RunShowCmdRequest, _ ...grpc.CallOption) (*aristapb.RunShowCmdResponse, error) {
						return &aristapb.RunShowCmdResponse{
							Response: &aristapb.EapiResponse{
								Success:   true,
								Responses: []string{string(j)},
							},
						}, nil
					},
				}
			},
			wantIPs: []net.IP{net.ParseIP("172.16.0.0").To4()},
		},
		{
			name: "successful interface connected but not up",
			clientFn: func() *arista.MockEAPIClient {
				resp := arista.IPInterfacesBriefResponse{
					Interfaces: map[string]arista.IPInterfaceBrief{
						"Tunnel1": {
							Name:               "Tunnel1",
							InterfaceStatus:    arista.IPInterfaceInterfaceStatusConnected,
							LineProtocolStatus: "not-up",
							InterfaceAddress: arista.IPInterfaceAddress{
								IPAddr: arista.IPInterfaceAddressIPAddr{
									Address: "172.16.0.1", MaskLen: 31,
								},
							},
						},
					},
				}
				j, _ := json.Marshal(resp)
				return &arista.MockEAPIClient{
					RunShowCmdFunc: func(_ context.Context, _ *aristapb.RunShowCmdRequest, _ ...grpc.CallOption) (*aristapb.RunShowCmdResponse, error) {
						return &aristapb.RunShowCmdResponse{
							Response: &aristapb.EapiResponse{
								Success:   true,
								Responses: []string{string(j)},
							},
						}, nil
					},
				}
			},
			wantIPs: []net.IP{net.ParseIP("172.16.0.0").To4()},
		},
		{
			name: "skips non 31 masklen interface",
			clientFn: func() *arista.MockEAPIClient {
				resp := arista.IPInterfacesBriefResponse{
					Interfaces: map[string]arista.IPInterfaceBrief{
						"Tunnel1": {
							Name:               "Tunnel1",
							InterfaceStatus:    arista.IPInterfaceInterfaceStatusConnected,
							LineProtocolStatus: arista.IPInterfaceLineProtocolStatusUp,
							InterfaceAddress: arista.IPInterfaceAddress{
								IPAddr: arista.IPInterfaceAddressIPAddr{
									Address: "172.16.0.1", MaskLen: 24,
								},
							},
						},
					},
				}
				j, _ := json.Marshal(resp)
				return &arista.MockEAPIClient{
					RunShowCmdFunc: func(_ context.Context, _ *aristapb.RunShowCmdRequest, _ ...grpc.CallOption) (*aristapb.RunShowCmdResponse, error) {
						return &aristapb.RunShowCmdResponse{
							Response: &aristapb.EapiResponse{
								Success:   true,
								Responses: []string{string(j)},
							},
						}, nil
					},
				}
			},
			wantIPs: []net.IP{},
		},
		{
			name: "client returns error",
			clientFn: func() *arista.MockEAPIClient {
				return &arista.MockEAPIClient{
					RunShowCmdFunc: func(_ context.Context, _ *aristapb.RunShowCmdRequest, _ ...grpc.CallOption) (*aristapb.RunShowCmdResponse, error) {
						return nil, errors.New("eapi unavailable")
					},
				}
			},
			expectErr: "eapi unavailable",
		},
		{
			name: "nil response",
			clientFn: func() *arista.MockEAPIClient {
				return &arista.MockEAPIClient{
					RunShowCmdFunc: func(_ context.Context, _ *aristapb.RunShowCmdRequest, _ ...grpc.CallOption) (*aristapb.RunShowCmdResponse, error) {
						return &aristapb.RunShowCmdResponse{Response: nil}, nil
					},
				}
			},
			expectErr: "no response",
		},
		{
			name: "unsuccessful response",
			clientFn: func() *arista.MockEAPIClient {
				return &arista.MockEAPIClient{
					RunShowCmdFunc: func(_ context.Context, _ *aristapb.RunShowCmdRequest, _ ...grpc.CallOption) (*aristapb.RunShowCmdResponse, error) {
						return &aristapb.RunShowCmdResponse{
							Response: &aristapb.EapiResponse{
								Success:      false,
								ErrorCode:    1,
								ErrorMessage: "some error",
							},
						}, nil
					},
				}
			},
			expectErr: "error from arista eapi",
		},
		{
			name: "invalid json",
			clientFn: func() *arista.MockEAPIClient {
				return &arista.MockEAPIClient{
					RunShowCmdFunc: func(_ context.Context, _ *aristapb.RunShowCmdRequest, _ ...grpc.CallOption) (*aristapb.RunShowCmdResponse, error) {
						return &aristapb.RunShowCmdResponse{
							Response: &aristapb.EapiResponse{
								Success:   true,
								Responses: []string{"{"},
							},
						}, nil
					},
				}
			},
			expectErr: "failed to unmarshal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logBuf := &strings.Builder{}
			log := slog.New(slog.NewTextHandler(logBuf, nil))
			client := arista.NewEAPIClient(log, tt.clientFn())
			ips, err := client.GetLocalTunnelTargetIPs(context.Background())

			if tt.expectErr != "" {
				require.ErrorContains(t, err, tt.expectErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.wantIPs, ips)
			}
		})
	}
}
