package rpc

import (
	"fmt"
	"os/exec"

	pb "github.com/malbeclabs/doublezero/e2e/proto/qa/gen/pb-go"
)

type MyTracerouteResult struct {
	Report MyTracerouteReport `json:"report"`
}

type MyTracerouteReport struct {
	MTR  MyTracerouteMTR   `json:"mtr"`
	Hubs []MyTracerouteHub `json:"hubs"`
}

type MyTracerouteMTR struct {
	Src   string `json:"src"`
	Dst   string `json:"dst"`
	Tests uint32 `json:"tests"`
}

type MyTracerouteHub struct {
	Count   uint32  `json:"count"`
	Host    string  `json:"host"`
	LossPct float32 `json:"Loss%"`
	Sent    uint32  `json:"Snt"`
	Last    float32 `json:"Last"`
	Avg     float32 `json:"Avg"`
	Best    float32 `json:"Best"`
	Worst   float32 `json:"Wrst"`
	StdDev  float32 `json:"StDev"`
}

func buildMTRCommandArgs(req *pb.TracerouteRequest) ([]string, error) {
	if _, err := exec.LookPath("mtr"); err != nil {
		return nil, fmt.Errorf("mtr binary not found")
	}
	args := []string{"--no-dns", req.TargetIp}
	if req.Timeout > 0 {
		args = append(args, "--timeout", fmt.Sprintf("%d", req.Timeout))
	}
	if req.Count > 0 {
		args = append(args, "--report-cycles", fmt.Sprintf("%d", req.Count))
	}
	if req.SourceIface != "" {
		args = append(args, "--interface", req.SourceIface)
	}
	if req.SourceIp != "" {
		args = append(args, "--address", req.SourceIp)
	}
	return args, nil
}

func hasMTRBinary() bool {
	if _, err := exec.LookPath("mtr"); err != nil {
		return false
	}
	return true
}
