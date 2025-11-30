package dzmon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

const (
	daemonAPISocketPath = "/var/run/doublezerod/doublezerod.sock"
)

type DaemonClient interface {
	GetRoutes(ctx context.Context) ([]Route, error)
}

type Route struct {
	UserType    UserType    `json:"user_type"`
	LocalIP     string      `json:"local_ip"`
	PeerIP      string      `json:"peer_ip"`
	KernelState KernelState `json:"kernel_state"`
}

type KernelState string

const (
	KernelStateUnknown KernelState = "unknown"
	KernelStatePresent KernelState = "present"
	KernelStateAbsent  KernelState = "absent"
)

func (r KernelState) String() string {
	return string(r)
}

type UserType int

const (
	UserTypeUnknown UserType = iota
	UserTypeIBRL
	UserTypeIBRLWithAllocatedIP
	UserTypeEdgeFiltering
	UserTypeMulticast
)

type userTypes []UserType

var ValidUserTypes = userTypes{
	UserTypeIBRL,
	UserTypeIBRLWithAllocatedIP,
	UserTypeEdgeFiltering,
	UserTypeMulticast,
}

func (u UserType) String() string {
	return [...]string{
		"Unknown",
		"IBRL",
		"IBRLWithAllocatedIP",
		"EdgeFiltering",
		"Multicast",
	}[u]
}

func (u UserType) FromString(userType string) UserType {
	return map[string]UserType{
		"Unknown":             UserTypeUnknown,
		"IBRL":                UserTypeIBRL,
		"IBRLWithAllocatedIP": UserTypeIBRLWithAllocatedIP,
		"EdgeFiltering":       UserTypeEdgeFiltering,
		"Multicast":           UserTypeMulticast,
	}[userType]
}

func (u UserType) MarshalJSON() ([]byte, error) {
	return json.Marshal(u.String())
}

func (u *UserType) UnmarshalJSON(b []byte) error {
	var s string
	err := json.Unmarshal(b, &s)

	if err != nil {
		return err
	}
	*u = u.FromString(s)
	return nil
}

type localDaemonClient struct {
	http *http.Client
}

func NewDaemonClient() *localDaemonClient {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return net.DialTimeout("unix", daemonAPISocketPath, 5*time.Second)
		},
	}
	return &localDaemonClient{http: &http.Client{Transport: tr}}
}

func (c *localDaemonClient) GetRoutes(ctx context.Context) ([]Route, error) {
	var routes []Route

	req, err := http.NewRequestWithContext(ctx, "GET", "http://unix/routes", nil)
	if err != nil {
		return nil, err
	}
	req.Host = "localhost"

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %s", resp.Status)
	}

	if err := json.NewDecoder(resp.Body).Decode(&routes); err != nil {
		return nil, err
	}

	return routes, nil
}
