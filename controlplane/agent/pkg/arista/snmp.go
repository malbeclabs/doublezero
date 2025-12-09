package arista

import (
	"context"
	"encoding/json"
	"fmt"

	aristapb "github.com/malbeclabs/doublezero/controlplane/proto/arista/gen/pb-go/arista/EosSdkRpc"
)

type ShowSnmpMibIfmibIfindexResponse struct {
	IfIndex map[string]uint64 `json:"ifIndex"`
}

type Command string

const (
	CommandEmpty                   Command = ""
	CommandShowSnmpMibIfmibIfindex Command = "show snmp mib ifmib ifindex"
)

func (e *EAPIClient) ShowSnmpMibIfmibIfindex(ctx context.Context) (*ShowSnmpMibIfmibIfindexResponse, Command, error) {
	response, err := e.Client.RunShowCmd(ctx, &aristapb.RunShowCmdRequest{
		Command: string(CommandShowSnmpMibIfmibIfindex),
	})
	if err != nil {
		return nil, CommandEmpty, fmt.Errorf("failed to execute show snmp mib ifmib ifindex: %w", err)
	}

	if response.Response == nil {
		return nil, CommandEmpty, fmt.Errorf("no response from arista eapi")
	}

	if !response.Response.Success {
		return nil, CommandEmpty, fmt.Errorf("error from arista eapi: code=%d, message=%s", response.Response.ErrorCode, response.Response.ErrorMessage)
	}

	var resp ShowSnmpMibIfmibIfindexResponse
	err = json.Unmarshal([]byte(response.Response.Responses[0]), &resp)
	if err != nil {
		return nil, CommandEmpty, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &resp, CommandShowSnmpMibIfmibIfindex, nil
}
