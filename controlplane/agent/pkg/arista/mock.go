package arista

import (
	"context"

	aristapb "github.com/malbeclabs/doublezero/controlplane/proto/arista/gen/pb-go/arista/EosSdkRpc"
	"google.golang.org/grpc"
)

type MockEAPIClient struct {
	RunShowCmdFunc    func(ctx context.Context, req *aristapb.RunShowCmdRequest, opts ...grpc.CallOption) (*aristapb.RunShowCmdResponse, error)
	RunConfigCmdsFunc func(ctx context.Context, in *aristapb.RunConfigCmdsRequest, opts ...grpc.CallOption) (*aristapb.RunConfigCmdsResponse, error)
}

func (m *MockEAPIClient) RunShowCmd(ctx context.Context, req *aristapb.RunShowCmdRequest, opts ...grpc.CallOption) (*aristapb.RunShowCmdResponse, error) {
	return m.RunShowCmdFunc(ctx, req, opts...)
}

func (m *MockEAPIClient) RunConfigCmds(ctx context.Context, in *aristapb.RunConfigCmdsRequest, opts ...grpc.CallOption) (*aristapb.RunConfigCmdsResponse, error) {
	return m.RunConfigCmdsFunc(ctx, in, opts...)
}
