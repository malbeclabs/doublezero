package bgp

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/netip"
	"sync"

	"github.com/jwhited/corebgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/liveness"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

var (
	ErrBgpPeerExists    = errors.New("bgp peer already exists")
	ErrBgpPeerNotExists = errors.New("bgp peer does not exist")
)

type SessionEvent struct {
	PeerAddr net.IP
	Session  Session
}

type SessionStatus int

const (
	SessionStatusPending SessionStatus = iota
	SessionStatusInitializing
	SessionStatusDown
	SessionStatusUp
	SessionStatusFailed
	SessionStatusUnreachable
)

func (s *Session) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		SessionStatus     SessionStatus `json:"session_status"`
		LastSessionUpdate int64         `json:"last_session_update"`
	}{
		SessionStatus:     s.SessionStatus,
		LastSessionUpdate: s.LastSessionUpdate,
	})
}

type Session struct {
	SessionStatus     SessionStatus `json:"session_status"`
	LastSessionUpdate int64         `json:"last_session_update"`
}

func (s SessionStatus) String() string {
	return [...]string{
		"Pending BGP Session",
		"Initializing BGP Session",
		"BGP Session Down",
		"BGP Session Up",
		"BGP Session Failed",
		"Network Unreachable",
	}[s]
}

func (s SessionStatus) FromString(sessionStatus string) SessionStatus {
	return map[string]SessionStatus{
		"Pending BGP Session":      SessionStatusPending,
		"Initializing BGP Session": SessionStatusInitializing,
		"BGP Session Down":         SessionStatusDown,
		"BGP Session Up":           SessionStatusUp,
		"BGP Session Failed":       SessionStatusFailed,
		"Network Unreachable":      SessionStatusUnreachable,
	}[sessionStatus]
}

func (s SessionStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

func (s *SessionStatus) UnmarshalJSON(b []byte) error {
	var n string
	err := json.Unmarshal(b, &n)
	if err != nil {
		return err
	}
	*s = s.FromString(n)
	return nil
}

type RouteReaderWriter interface {
	RouteAdd(*routing.Route) error
	RouteDelete(*routing.Route) error
	RouteByProtocol(int) ([]*routing.Route, error)
}

type PeerConfig struct {
	LocalAddress         net.IP
	RemoteAddress        net.IP
	LocalAs              uint32
	RemoteAs             uint32
	Port                 int
	RouteSrc             net.IP
	RouteTable           int
	NoUninstall          bool
	NoInstall            bool
	Interface            string
	AllowLivenessEnabled bool
	LivenessPort         int
}

type BgpServer struct {
	server            *corebgp.Server
	peerStatusChan    chan SessionEvent
	peerStatus        map[string]Session
	peerStatusLock    sync.Mutex
	routeReaderWriter RouteReaderWriter
	livenessManager   liveness.Manager
}

func NewBgpServer(routerID net.IP, rrw RouteReaderWriter, lm liveness.Manager) (*BgpServer, error) {
	corebgp.SetLogger(log.Print)
	srv, err := corebgp.NewServer(netip.MustParseAddr(routerID.String()))
	if err != nil {
		return nil, fmt.Errorf("error creating bgp server: %v", err)
	}
	return &BgpServer{
		server:            srv,
		peerStatusChan:    make(chan SessionEvent),
		peerStatus:        make(map[string]Session),
		peerStatusLock:    sync.Mutex{},
		routeReaderWriter: rrw,
		livenessManager:   lm,
	}, nil
}

func (b *BgpServer) Serve(lis []net.Listener) error {
	go func() {
		for {
			update := <-b.GetStatusEvent()
			b.peerStatusLock.Lock()
			b.peerStatus[update.PeerAddr.String()] = update.Session
			b.peerStatusLock.Unlock()
		}
	}()
	return b.server.Serve(lis)
}

func (b *BgpServer) AddPeer(p *PeerConfig, advertised []NLRI) error {
	peerOpts := make([]corebgp.PeerOption, 0)
	peerOpts = append(peerOpts, corebgp.WithLocalAddress(netip.MustParseAddr(p.LocalAddress.String())))
	if p.Port != 0 {
		peerOpts = append(peerOpts, corebgp.WithPort(p.Port))
	}
	rrw := newRouteReaderWriterWithNoUninstall(b.routeReaderWriter, p.NoUninstall)
	if p.AllowLivenessEnabled && b.livenessManager != nil {
		rrw = liveness.NewRouteReaderWriter(b.livenessManager, b.routeReaderWriter, p.Interface, p.NoUninstall)
	}
	plugin := NewBgpPlugin(advertised, p.RouteSrc, p.RouteTable, b.peerStatusChan, p.NoInstall, rrw)
	plugin.peerAddr = p.RemoteAddress
	plugin.startSessionTimeout()
	err := b.server.AddPeer(corebgp.PeerConfig{
		RemoteAddress: netip.MustParseAddr(p.RemoteAddress.String()),
		LocalAS:       p.LocalAs,
		RemoteAS:      p.RemoteAs,
	}, plugin, peerOpts...)
	if err != nil && errors.Is(err, corebgp.ErrPeerAlreadyExists) {
		return ErrBgpPeerExists
	}
	return err
}

func (b *BgpServer) DeletePeer(ip net.IP) error {
	if ip == nil {
		return fmt.Errorf("no peeer ip provided")
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return fmt.Errorf("malformed peer address")
	}
	err := b.server.DeletePeer(addr)
	if errors.Is(err, corebgp.ErrPeerNotExist) {
		return ErrBgpPeerNotExists
	}
	return err
}

func (b *BgpServer) GetStatusEvent() <-chan SessionEvent {
	return b.peerStatusChan
}

func (b *BgpServer) GetPeerStatus(ip net.IP) Session {
	b.peerStatusLock.Lock()
	defer b.peerStatusLock.Unlock()
	if peerStatus, ok := b.peerStatus[ip.String()]; ok {
		return peerStatus
	}
	return Session{SessionStatus: SessionStatusPending}
}
