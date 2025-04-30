package bgp

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/netip"
	"time"

	"github.com/jwhited/corebgp"
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
	SessionStatusunknown SessionStatus = iota
	SessionStatusPending
	SessionStatusInitializing
	SessionStatusDown
	SessionStatusUp
)

func (s *Session) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		SessionStatus     SessionStatus `json:"session_status"`
		LastSessionUpdate time.Time     `json:"last_session_update"`
	}{
		SessionStatus:     s.SessionStatus,
		LastSessionUpdate: s.LastSessionUpdate,
	})
}

type Session struct {
	SessionStatus     SessionStatus `json:"session_status"`
	LastSessionUpdate time.Time     `json:"last_session_update"`
}

func (s SessionStatus) String() string {
	return [...]string{
		"unknown",
		"pending",
		"initializing",
		"down",
		"up",
	}[s]

}

func (s SessionStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

type PeerConfig struct {
	LocalAddress  net.IP
	RemoteAddress net.IP
	LocalAs       uint32
	RemoteAs      uint32
	Port          int
	RouteTable    int
}

type BgpServer struct {
	server          *corebgp.Server
	addRouteChan    chan NLRI
	deleteRouteChan chan NLRI
	peerStatusChan  chan SessionEvent
	peerStatus      map[string]Session
}

func NewBgpServer(routerID net.IP) (*BgpServer, error) {
	corebgp.SetLogger(log.Print)
	srv, err := corebgp.NewServer(netip.MustParseAddr(routerID.String()))
	if err != nil {
		return nil, fmt.Errorf("error creating bgp server: %v", err)
	}
	return &BgpServer{
		server:          srv,
		addRouteChan:    make(chan NLRI),
		deleteRouteChan: make(chan NLRI),
		peerStatusChan:  make(chan SessionEvent),
		peerStatus:      make(map[string]Session),
	}, nil
}

func (b *BgpServer) Serve(lis []net.Listener) error {
	go func() {
		for {
			update := <-b.GetStatusEvent()
			b.peerStatus[update.PeerAddr.String()] = update.Session
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
	plugin := NewBgpPlugin(b.addRouteChan, b.deleteRouteChan, advertised, p.RouteTable, b.peerStatusChan)
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
func (b *BgpServer) AddRoute() <-chan NLRI {
	return b.addRouteChan
}

func (b *BgpServer) WithdrawRoute() <-chan NLRI {
	return b.deleteRouteChan
}

func (b *BgpServer) GetStatusEvent() <-chan SessionEvent {
	return b.peerStatusChan
}

func (b *BgpServer) GetPeerStatus(ip net.IP) Session {
	if peerStatus, ok := b.peerStatus[ip.String()]; ok {
		return peerStatus
	}
	return Session{SessionStatus: SessionStatusunknown}
}
