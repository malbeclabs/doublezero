package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/malbeclabs/doublezero/e2e/internal/poll"
	pb "github.com/malbeclabs/doublezero/e2e/proto/qa/gen/pb-go"
	probing "github.com/prometheus-community/pro-bing"
	"golang.org/x/net/ipv4"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type Joiner interface {
	JoinGroup(ctx context.Context, group net.IP, port string, ifName string) error
	Stop()
	GetStatistics(net.IP) uint64
}

type Option func(*QAAgent)

type QAAgent struct {
	pb.UnimplementedQAAgentServiceServer
	listener      net.Listener
	mcastListener Joiner
	log           *slog.Logger
}

// NewQAAgent creates a new QAAgent instance. It accepts an address (i.e. localhost:443) to listen
// on and a Joiner interface for managing multicast group joins.
func NewQAAgent(logger *slog.Logger, addr string, j Joiner) (*QAAgent, error) {
	q := &QAAgent{
		mcastListener: j,
		log:           logger,
	}
	for _, opt := range []Option{} {
		opt(q)
	}

	if q.listener == nil {
		lis, err := net.Listen("tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("failed to listen: %v", err)
		}
		q.listener = lis
	}
	return q, nil
}

// Start starts the QAAgent and blocks until the context is done or an error occurs.
func (q *QAAgent) Start(ctx context.Context) error {
	agent := grpc.NewServer()
	pb.RegisterQAAgentServiceServer(agent, q)

	errChan := make(chan error)
	go func() {
		if err := agent.Serve(q.listener); err != nil {
			errChan <- err
		}
	}()

	select {
	case <-ctx.Done():
		q.log.Info("Stopping QA Agent...")
		agent.GracefulStop()
		q.mcastListener.Stop()
		return nil
	case err := <-errChan:
		if err != nil {
			return fmt.Errorf("agent error: %v", err)
		}
		return nil
	}
}

// Ping implements the Ping RPC, executes a set of ICMP pings, and reports the results to the caller.
// This requires CAP_NET_RAW capability to run successfully due to the use of raw sockets.
func (q *QAAgent) Ping(ctx context.Context, req *pb.PingRequest) (*pb.PingResult, error) {
	q.log.Info("Received Ping request for target IP", "target_ip", req.GetTargetIp())
	pinger, err := probing.NewPinger(req.GetTargetIp())
	if err != nil {
		return nil, fmt.Errorf("failed to create pinger: %v", err)
	}
	if req.GetPingType() == pb.PingRequest_ICMP {
		pinger.SetPrivileged(true)
	}
	if req.GetPingType() == pb.PingRequest_UDP {
		pinger.SetPrivileged(false)
	}
	pinger.Count = 5
	pinger.Source = req.GetSourceIp()
	pinger.InterfaceName = req.GetSourceIface()
	if req.GetTimeout() > 0 {
		pinger.Timeout = time.Duration(req.GetTimeout()) * time.Second
	}
	if err := pinger.Run(); err != nil {
		return nil, fmt.Errorf("ping failed: %v", err)
	}
	stats := pinger.Statistics()
	q.log.Info("Ping statistics", "target_ip", req.GetTargetIp(), "packets_sent", stats.PacketsSent, "packets_received", stats.PacketsRecv)
	return &pb.PingResult{PacketsSent: uint32(stats.PacketsSent), PacketsReceived: uint32(stats.PacketsRecv)}, nil
}

// MulticastJoin implements the MulticastJoin RPC, joins the requested multicast group and counts
// received packets per joined group. Use the GetStatistics RPC to retrieve the stats.
func (q *QAAgent) MulticastJoin(ctx context.Context, req *pb.MulticastJoinRequest) (*pb.MulticastJoinResult, error) {
	for _, group := range req.GetGroups() {
		ip := net.ParseIP(group.GetGroup())
		if ip == nil {
			return nil, fmt.Errorf("invalid group IP: %s", group.GetGroup())
		}
		q.log.Info("Joining multicast group", "group", ip.String(), "port", group.GetPort(), "interface", group.GetIface())
		err := q.mcastListener.JoinGroup(context.Background(), ip, fmt.Sprintf("%d", group.GetPort()), group.GetIface())
		if err != nil {
			return nil, err
		}
	}
	return &pb.MulticastJoinResult{}, nil
}

// MulticastLeave implements the MulticastLeave RPC and stops listening to all multicast groups.
func (q *QAAgent) MulticastLeave(ctx context.Context, in *emptypb.Empty) (*emptypb.Empty, error) {
	q.log.Info("Leaving all multicast groups.")
	q.mcastListener.Stop()
	return &emptypb.Empty{}, nil
}

// ConnectUnicast implements the ConnectUnicast RPC. This establishes a unicast tunnel to DoubleZero
// using IBRL mode. This call will block until the tunnel is up according to the DoubleZero status
// output or return an error if the tunnel is not up within 20 seconds.
func (q *QAAgent) ConnectUnicast(ctx context.Context, req *pb.ConnectUnicastRequest) (*pb.Result, error) {
	q.log.Info("Received ConnectUnicast request", "client_ip", req.GetClientIp(), "device_code", req.GetDeviceCode())
	clientIP := req.GetClientIp()
	deviceCode := req.GetDeviceCode()
	cmds := []string{"connect", "ibrl"}
	if clientIP != "" {
		cmds = append(cmds, "--client-ip", clientIP)
	}
	if deviceCode != "" {
		cmds = append(cmds, "--device", deviceCode)
	}
	cmd := exec.Command("doublezero", cmds...)
	res, err := runCmd(cmd)
	if err != nil {
		q.log.Error("Failed to connect unicast for client", "client_ip", clientIP, "output", res.GetOutput())
		return res, fmt.Errorf("failed to connect unicast for client %s: %v", clientIP, err)
	}

	q.log.Info("Successfully connected IBRL mode tunnel")

	condition := func() (bool, error) {
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		status, err := fetchStatus(ctx)
		if err != nil {
			q.log.Warn("fetchStatus error", "error", err)
			return false, err
		}
		if len(status) == 0 {
			q.log.Warn("fetchStatus returned empty status")
			return false, fmt.Errorf("empty status response")
		}
		currentState := status[0].DoubleZeroStatus.State
		q.log.Info("Polling tunnel status", "state", currentState, "tunnel_name", status[0].TunnelName, "doublezero_ip", status[0].DoubleZeroIP)
		return currentState == "up", nil
	}

	err = poll.Until(ctx, condition, 60*time.Second, 1*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed while polling for session status: %v", err)
	}

	return res, nil
}

// Disconnect implements the Disconnect RPC, which removes the current tunnel from DoubleZero.
func (q *QAAgent) Disconnect(ctx context.Context, req *emptypb.Empty) (*pb.Result, error) {
	q.log.Info("Received Disconnect request")
	cmd := exec.Command("doublezero", "disconnect")
	output, err := cmd.CombinedOutput()

	res := &pb.Result{
		Output: strings.Split(string(output), "\n"),
	}

	if err != nil {
		res.Success = false
		q.log.Error("Failed to disconnect", "output", string(output))
		if exitErr, ok := err.(*exec.ExitError); ok {
			res.ReturnCode = int32(exitErr.ExitCode())
		} else {
			res.ReturnCode = -1
			res.Output = append(res.Output, err.Error())
		}
	} else {
		res.Success = true
		res.ReturnCode = 0
		q.log.Info("Successfully disconnected")
	}

	return res, nil
}

type StatusResponse struct {
	TunnelName       string `json:"tunnel_name"`
	DoubleZeroIP     net.IP `json:"doublezero_ip"`
	UserType         string `json:"user_type"`
	DoubleZeroStatus struct {
		State string `json:"session_status"`
	} `json:"doublezero_status"`
}

// GetStatus implements the GetStatus RPC, which retrieves the current status of the configured DoubleZero
// tunnel. This is equivalent to the `doublezero status` command.
func (q *QAAgent) GetStatus(ctx context.Context, req *emptypb.Empty) (*pb.StatusResponse, error) {
	q.log.Info("Received GetStatus request")
	status, err := fetchStatus(ctx)
	if err != nil {
		return nil, err
	}
	if len(status) == 0 {
		return nil, fmt.Errorf("error fetching status: no data available")
	}

	resp := &pb.StatusResponse{}
	for _, s := range status {
		r := &pb.Status{
			TunnelName:    s.TunnelName,
			DoubleZeroIp:  s.DoubleZeroIP.String(),
			UserType:      s.UserType,
			SessionStatus: s.DoubleZeroStatus.State,
		}
		resp.Status = append(resp.Status, r)
	}
	return resp, nil
}

// CreateMulticastGroup implements the CreateMulticastGroup RPC, which creates a multicast group
// with the specified code and maximum bandwidth.
func (q *QAAgent) CreateMulticastGroup(ctx context.Context, req *pb.CreateMulticastGroupRequest) (*pb.Result, error) {
	if req.GetCode() == "" {
		return nil, fmt.Errorf("code is required")
	}
	if req.GetMaxBandwidth() == "" {
		return nil, fmt.Errorf("bandwidth is required")
	}
	q.log.Info("Received CreateMulticastGroup request", "code", req.GetCode(), "bandwidth", req.GetMaxBandwidth())
	cmd := exec.Command("doublezero", "multicast", "group", "create", "--code", req.GetCode(), "--max-bandwidth", req.GetMaxBandwidth(), "--owner", "me")
	result, err := runCmd(cmd)
	if err != nil {
		q.log.Error("Failed to create multicast group", "error", err, "output", result.Output)
		return nil, err
	}
	return result, nil
}

// DeleteMulticastGroup implements the DeleteMulticastGroup RPC, which deletes a multicast group
// by its public key.
func (q *QAAgent) DeleteMulticastGroup(ctx context.Context, req *pb.DeleteMulticastGroupRequest) (*pb.Result, error) {
	if req.GetPubkey() == "" {
		return nil, fmt.Errorf("pubkey is required")
	}
	q.log.Info("Received DeleteMulticastGroup request", "pubkey", req.GetPubkey())
	cmd := exec.Command("doublezero", "multicast", "group", "delete", "--pubkey", req.GetPubkey())
	result, err := runCmd(cmd)
	if err != nil {
		q.log.Error("Failed to delete multicast group", "error", err, "output", result.Output)
		return nil, err
	}
	return result, nil
}

// ConnectMulticast implements the ConnectMulticast RPC, which connects to a multicast group
// as either a publisher or subscriber.
func (q *QAAgent) ConnectMulticast(ctx context.Context, req *pb.ConnectMulticastRequest) (*pb.Result, error) {
	if req.GetCode() == "" {
		return nil, fmt.Errorf("code is required")
	}
	if req.GetMode() == pb.ConnectMulticastRequest_UNSPECIFIED {
		return nil, fmt.Errorf("mode is required")
	}
	mode := ""
	if req.GetMode() == pb.ConnectMulticastRequest_PUBLISHER {
		mode = "publisher"
	}
	if req.GetMode() == pb.ConnectMulticastRequest_SUBSCRIBER {
		mode = "subscriber"
	}
	q.log.Info("Received ConnectMulticast request", "code", req.GetCode(), "mode", mode)
	cmd := exec.Command("doublezero", "connect", "multicast", mode, req.Code)
	result, err := runCmd(cmd)
	if err != nil {
		q.log.Error("Failed to connect multicast", "error", err, "output", result.Output)
		return nil, err
	}
	return result, nil
}

// MulticastAllowListAdd implements the MulticastAllowListAdd RPC, which adds a publisher or subscriber
// to the multicast allowlist for a specific group.
func (q *QAAgent) MulticastAllowListAdd(ctx context.Context, req *pb.MulticastAllowListAddRequest) (*pb.Result, error) {
	if req.GetPubkey() == "" {
		return nil, fmt.Errorf("pubkey is required")
	}
	if req.GetCode() == "" {
		return nil, fmt.Errorf("group is required")
	}
	if req.GetMode() == pb.MulticastAllowListAddRequest_UNSPECIFIED {
		return nil, fmt.Errorf("mode is required")
	}
	mode := ""
	if req.GetMode() == pb.MulticastAllowListAddRequest_PUBLISHER {
		mode = "publisher"
	}
	if req.GetMode() == pb.MulticastAllowListAddRequest_SUBSCRIBER {
		mode = "subscriber"
	}
	q.log.Info("Received MulticastAllowListAdd request", "pubkey", req.GetPubkey(), "code", req.GetCode(), "mode", mode)
	cmd := exec.Command("doublezero", "multicast", "group", "allowlist", mode, "add", "--pubkey", req.GetPubkey(), "--code", req.GetCode())
	result, err := runCmd(cmd)
	if err != nil {
		q.log.Error("Failed to add multicast allowlist entry", "error", err, "output", result.Output)
		return nil, err
	}
	return result, nil
}

// MulticastSend implements the MulticastSend RPC, which sends multicast packets to a specified group
// for a given duration.
func (q *QAAgent) MulticastSend(ctx context.Context, req *pb.MulticastSendRequest) (*emptypb.Empty, error) {
	if req.GetGroup() == "" {
		return &emptypb.Empty{}, fmt.Errorf("group is required")
	}
	if req.GetPort() == 0 {
		return &emptypb.Empty{}, fmt.Errorf("port is required")
	}
	if req.GetDuration() == 0 {
		return &emptypb.Empty{}, fmt.Errorf("duration is required")
	}

	q.log.Info("Received MulticastSend request", "group", req.GetGroup(), "port", req.GetPort(), "duration", req.GetDuration())

	addr := fmt.Sprintf("%s:%d", req.GetGroup(), req.GetPort())
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return &emptypb.Empty{}, fmt.Errorf("failed to resolve multicast address: %w", err)
	}
	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		return &emptypb.Empty{}, fmt.Errorf("failed to dial multicast address: %w", err)
	}
	defer conn.Close()

	p := ipv4.NewPacketConn(conn)
	if err := p.SetMulticastTTL(64); err != nil {
		return &emptypb.Empty{}, fmt.Errorf("failed to set multicast TTL: %w", err)
	}

	var packetsSent uint64
	payload := []byte("hello multicast from QAAgent")

	sendCtx, cancel := context.WithTimeout(ctx, time.Duration(req.GetDuration())*time.Second)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

loop:
	for {
		select {
		case <-sendCtx.Done():
			break loop
		case <-ticker.C:
			_, err := p.WriteTo(payload, nil, nil)
			if err != nil {
				q.log.Error("Failed to write to multicast group", "error", err)
				break loop
			}
			packetsSent++
		}
	}

	q.log.Info("Finished sending multicast packets", "group", req.GetGroup(), "packets_sent", packetsSent)

	return &emptypb.Empty{}, nil
}

// MulticastReport implements the MulticastReport RPC, which retrieves statistics for multicast groups
// that the agent is currently listening to. It returns the number of packets received for each group.
func (q *QAAgent) MulticastReport(ctx context.Context, req *pb.MulticastReportRequest) (*pb.MulticastReportResult, error) {
	if len(req.GetGroups()) == 0 {
		return nil, fmt.Errorf("at least one group is required")
	}

	q.log.Info("Received MulticastReport request", "groups", req.GetGroups())
	reports := make(map[string]*pb.MulticastReport)

	for _, group := range req.GetGroups() {
		ip := net.ParseIP(group.Group)
		if ip == nil {
			return nil, fmt.Errorf("invalid group IP: %s", group)
		}
		packets := q.mcastListener.GetStatistics(ip)
		reports[ip.String()] = &pb.MulticastReport{
			PacketCount: packets,
		}
	}
	q.log.Info("Multicast report generated", "reports", reports)
	return &pb.MulticastReportResult{Reports: reports}, nil
}

// runCmd executes a command and returns the result in a structured format.
func runCmd(cmd *exec.Cmd) (*pb.Result, error) {
	output, err := cmd.CombinedOutput()
	res := &pb.Result{
		Output: strings.Split(string(output), "\n"),
	}
	if err != nil {
		res.Success = false
		if exitErr, ok := err.(*exec.ExitError); ok {
			res.ReturnCode = int32(exitErr.ExitCode())
		} else {
			res.ReturnCode = -1
		}
		return res, fmt.Errorf("command failed: %v, output: %s", err, strings.Join(res.Output, "\n"))
	}
	res.Success = true
	res.ReturnCode = 0
	return res, nil
}

// fetchStatus retrieves the current status of the configured DoubleZero tunnel via the doublezerod
// unix socket.
func fetchStatus(ctx context.Context) ([]StatusResponse, error) {
	sockFile := "/var/run/doublezerod/doublezerod.sock"

	client := http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				dialer := net.Dialer{}
				return dialer.DialContext(ctx, "unix", sockFile)
			},
		},
		Timeout: 5 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", "http://doublezero/status", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create status request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error during status request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status request returned non-200 status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read status response body: %w", err)
	}

	var status []StatusResponse
	if err := json.Unmarshal(body, &status); err != nil {
		return nil, fmt.Errorf("failed to unmarshal status response: %w", err)
	}
	return status, nil
}
