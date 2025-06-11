package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"slices"
	"strings"
	"time"

	arista "github.com/malbeclabs/doublezero/controlplane/proto/arista/gen/pb-go/arista/EosSdkRpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// [{ "vrfs": { "default": { "peerList": [{"peerAddress": "192.168.1.1", "asn": "65342", "linkType": "internal", "routerId": "0.0.0.0", "vrf": "default"}]}}}]
type Peer struct {
	PeerAddress string `json:"peerAddress"`
}

type VRF struct {
	Peers []Peer `json:"peerList"`
}

type ShowBgpNeighborsResponse struct {
	VRFs map[string]VRF `json:"vrfs"`
}

// show configuration sessions: [{ "sessions": { "doublezero-agent-12345678": { "state": "pending", "completedTime": 1736543591.7642765, "commitUser": "", "description": "", "instances": { "868": { "user": "root", "terminal": "vty5", "currentTerminal": false } } }, "blah1": { "state": "pending", "commitUser": "", "description": "", "instances": {} } }, "maxSavedSessions": 1, "maxOpenSessions": 5, "mergeOnCommit": false, "saveToStartupConfigOnCommit": false }]
type Session struct {
	State string `json:"state"`
}

// Define a struct for the overall config that includes the sessions map.
type ShowConfigurationSessions struct {
	Sessions map[string]Session `json:"sessions"`
}

// show configuration lock: [{ "userInfo": { "username": "root", "userTty": "vty4", "transactionName": "doublezero", "lockAcquireTime": 1739312029.188106 } } ]
type UserInfo struct {
	Username        string  `json:"username"`
	TransactionName string  `json:"transactionName"`
	LockAcquireTime float64 `json:"lockAcquireTime"`
}

type ShowConfigurationLock struct {
	UserInfo UserInfo `json:"userInfo"`
}

// NewClientConn creates a new instance grpc client
func NewClientConn(device string) (*grpc.ClientConn, error) {
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}

	rpcConn, err := grpc.NewClient(device, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to device: %w", err)
	}

	return rpcConn, err
}

// NewEapiClient creates a new instance of EapiClient with the provided context and device address.
// If clientConn is set to nil, generate a new clientConn using the specified device. Unit tests can use this
// to pass in a mock clientConn.
func NewEapiClient(device string, clientConn *grpc.ClientConn) *EapiClient {
	client := arista.NewEapiMgrServiceClient(clientConn)

	return &EapiClient{
		Client: client,
	}
}

// Generate a distinguisher that to append to the EOS config session name to make it unique
// as required by EOS https://www.arista.com/en/um-eos/eos-configure-session
func getEosConfigurationSessionDistinguisher() string {
	return fmt.Sprintf("%d", time.Now().Unix())
}

// EapiClient encapsulates the gRPC client and connection logic for interacting with the Arista device
type EapiClient struct {
	Client arista.EapiMgrServiceClient
}

func (e *EapiClient) clearStaleConfigSessions(ctx context.Context) error {
	cmd := &arista.RunShowCmdRequest{
		Command: "show configuration sessions",
	}

	resp, err := e.Client.RunShowCmd(ctx, cmd)
	if err != nil {
		return fmt.Errorf("failed to run 'show configuration sessions': %w", err)
	}

	if !resp.Response.Success {
		return fmt.Errorf("error running eAPI cmd '%s': %s", cmd.Command, resp.Response.ErrorMessage)
	}

	var configSessionsOnSwitch ShowConfigurationSessions

	err = json.Unmarshal([]byte(resp.Response.Responses[0]), &configSessionsOnSwitch)
	if err != nil {
		return fmt.Errorf("Error unmarshalling JSON: %v", err)
	}

	for sessionName, session := range configSessionsOnSwitch.Sessions {
		if strings.HasPrefix(sessionName, "doublezero-agent-") && session.State == "pending" {
			err = e.clearConfigSession(ctx, sessionName)
			if err != nil {
				log.Println("ClearConfigSession() failed: ", err)
			}
		}
	}
	return nil
}

func (e *EapiClient) clearConfigSession(ctx context.Context, sessionName string) error {
	log.Printf("Found stale Arista EOS configuration session \"%s\", attempting to remove", sessionName)

	cmd := &arista.RunShowCmdRequest{
		Command: fmt.Sprintf("configure session %s abort", sessionName),
	}

	resp, err := e.Client.RunShowCmd(ctx, cmd)
	if err != nil {
		return fmt.Errorf("failed to run config commands: %w", err)
	}

	if !resp.Response.Success {
		return fmt.Errorf("error running eAPI cmd '%s': %s", cmd.Command, resp.Response.ErrorMessage)
	}
	log.Printf("Stale Arista EOS configuration session \"%s\" removed", sessionName)
	return nil
}

func (e *EapiClient) getLock(ctx context.Context) (ShowConfigurationLock, error) {
	var configLock ShowConfigurationLock

	cmd := &arista.RunShowCmdRequest{
		Command: "show configuration lock",
	}

	resp, err := e.Client.RunShowCmd(ctx, cmd)
	if err != nil {
		return ShowConfigurationLock{}, fmt.Errorf("failed to run `show configuration lock`: %w", err)
	}

	if !resp.Response.Success {
		return ShowConfigurationLock{}, fmt.Errorf("error running eAPI cmd '%s': %s", cmd.Command, resp.Response.ErrorMessage)
	}

	err = json.Unmarshal([]byte(resp.Response.Responses[0]), &configLock)
	if err != nil {
		return ShowConfigurationLock{}, fmt.Errorf("error unmarshalling JSON: %v", err)
	}
	return configLock, nil
}

func (e *EapiClient) forceConfigUnlock(ctx context.Context) error {
	cmd := &arista.RunConfigCmdsRequest{
		Commands: []string{
			"configure unlock force",
		},
	}

	resp, err := e.Client.RunConfigCmds(ctx, cmd)
	if err != nil {
		return fmt.Errorf("failed to run config commands: %w", err)
	}

	if !resp.Response.Success {
		return fmt.Errorf("error running eAPI cmd '%s': %s", cmd.Commands, resp.Response.ErrorMessage)
	}
	return nil
}

func (e *EapiClient) startLock(ctx context.Context, maxLockAge int) error {
	configLock, err := e.getLock(ctx)
	if err != nil {
		return fmt.Errorf("failed to call getLock: %w", err)
	}

	if configLock.UserInfo.LockAcquireTime == 0 {
		// No lock is present
		return nil
	}
	currentTime := float64(time.Now().Unix())
	infoString := fmt.Sprintf("user %s, transaction '%s', created at timestamp %f, age %f seconds, max lock age %d seconds",
		configLock.UserInfo.Username,
		configLock.UserInfo.TransactionName,
		configLock.UserInfo.LockAcquireTime,
		currentTime-configLock.UserInfo.LockAcquireTime,
		maxLockAge,
	)

	if currentTime-configLock.UserInfo.LockAcquireTime > float64(maxLockAge) {
		log.Printf("Attempting to force configuration unlock (%s)\n", infoString)
		err = e.forceConfigUnlock(ctx)
		if err != nil {
			return fmt.Errorf("failed to call forceConfigUnlock`: %w", err)
		}
		log.Printf("forced unlock of configuration lock (%s)\n", infoString)
		return nil
	}

	return fmt.Errorf("not overriding lock since its age is less than the configured max (%s)", infoString)
}

// Start a configuration session and apply the received config commands to it.
func (e *EapiClient) startConfigSession(ctx context.Context, config string) ([]string, string, error) {
	configSlice := strings.Split(config, "\n")
	log.Printf("Received %d lines of configuration from controller", len(configSlice))
	sessionName := fmt.Sprintf("doublezero-agent-%s", getEosConfigurationSessionDistinguisher())
	configSlice = append([]string{fmt.Sprintf("configure session %s", sessionName)}, configSlice...)
	cmd := &arista.RunConfigCmdsRequest{
		Commands: configSlice,
	}

	resp, err := e.Client.RunConfigCmds(ctx, cmd)
	if err != nil {
		return nil, "", fmt.Errorf("failed to run config commands: %w", err)
	}

	if !resp.Response.Success {
		return nil, "", fmt.Errorf("error running eAPI cmd '%s': %s", cmd.Commands, resp.Response.ErrorMessage)
	}

	return configSlice, sessionName, nil
}

// CheckConfigChanges determines whether any changes were made during the configuration session.
func (e *EapiClient) CheckConfigChanges(sessionName string, diffCmd *exec.Cmd) (string, error) {
	if diffCmd == nil {
		diffCmd = exec.Command("ip", "netns", "exec", "default", "/usr/bin/Cli", "-p", "15", "-c", fmt.Sprintf("show session-config named %s diffs", sessionName))
	}

	var out strings.Builder
	diffCmd.Stdout = &out

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	diffCmd = exec.CommandContext(ctx, diffCmd.Path, diffCmd.Args[1:]...)
	diffCmd.Stdout = &out

	err := diffCmd.Run()

	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("could not get diff because /usr/bin/Cli command timed out after 60 seconds")
	}

	if err != nil {
		return "", fmt.Errorf("could not get diff because \"show session-config named %s diffs\" failed with error: %v, output: %s", sessionName, err, out.String())
	}

	diffs := out.String()

	return diffs, nil
}

// commitOrCancelSession commits the configuration session if changes were made, otherwise cancels it.
func (e *EapiClient) commitOrCancelSession(ctx context.Context, sessionName, diffs string) error {
	var commitCommand string
	if diffs == "" {
		log.Println("No changes detected, exiting config session without committing")
		commitCommand = fmt.Sprintf("configure session %s abort", sessionName)
	} else {
		log.Printf("Committing config session due to diffs detected: %s\n", diffs)
		commitCommand = fmt.Sprintf("configure session %s commit", sessionName)
	}

	cmd := &arista.RunConfigCmdsRequest{
		Commands: []string{
			commitCommand,
			"configure unlock transaction doublezero",
			"copy running-config startup-config",
		},
	}

	resp, err := e.Client.RunConfigCmds(ctx, cmd)
	if err != nil {
		return fmt.Errorf("failed to finalize configuration session with command %s: %w", commitCommand, err)
	}

	if !resp.Response.Success {
		return fmt.Errorf("error running '%s': %s", commitCommand, resp.Response.ErrorMessage)
	}

	return nil
}

// Apply the Arista EOS configuration commands we received from the controller to the local device
func (e *EapiClient) AddConfigToDevice(ctx context.Context, config string, diffCmd *exec.Cmd, maxLockAge int) ([]string, error) {
	// Remove any stale doublezero configuration sessions
	err := e.clearStaleConfigSessions(ctx)
	if err != nil {
		log.Printf("ClearStaleConfigSessions failed with error %q. Attempting configuration anyway", err)
	}

	err = e.startLock(ctx, maxLockAge)
	if err != nil {
		return nil, err
	}

	configSlice, sessionName, err := e.startConfigSession(ctx, config)
	if err != nil {
		return nil, err
	}

	diffs, err := e.CheckConfigChanges(sessionName, diffCmd)
	if err != nil {
		return nil, err
	}

	err = e.commitOrCancelSession(ctx, sessionName, diffs)
	if err != nil {
		return nil, err
	}

	return configSlice, nil
}

// Retrieve a list of BGP neighbor IP addresses from the local switch.
// (The slice of strings return value is intended to be used only by unit tests)
func (e *EapiClient) GetBgpNeighbors(ctx context.Context) (map[string][]string, error) {
	cmd := &arista.RunShowCmdRequest{
		Command: "show ip bgp neighbors vrf all",
	}

	resp, err := e.Client.RunShowCmd(ctx, cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to run config commands: %w", err)
	}

	if !resp.Response.Success {
		return nil, fmt.Errorf("error running eAPI cmd '%s': %s", cmd.Command, resp.Response.ErrorMessage)
	}

	neighbors := &ShowBgpNeighborsResponse{}
	err = json.Unmarshal([]byte(resp.Response.Responses[0]), neighbors)
	// Add error handling to decide which Unmarshal errors should be fatal
	if err != nil {
		log.Printf("Warning: json.Unmarshal returned error %v\n", err)
	}

	neighborIpMap := make(map[string][]string)

	for vrfName, vrf := range neighbors.VRFs {
		for _, peer := range vrf.Peers {
			if !slices.Contains(neighborIpMap[vrfName], peer.PeerAddress) {
				neighborIpMap[vrfName] = append(neighborIpMap[vrfName], peer.PeerAddress)
			}
		}
		slices.Sort(neighborIpMap[vrfName])
	}

	return neighborIpMap, nil
}
