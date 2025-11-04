package liveness

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

/*** ---------- BFD-lite core types kept with Manager ---------- ***/

type State uint8

const (
	AdminDown State = iota
	Down
	Init
	Up
)

type Ctrl struct {
	Version    uint8
	Diag       uint8
	State      State
	Flags      uint8 // bit1=Poll, bit0=Final
	DetectMult uint8
	Length     uint8

	MyDiscr         uint32
	YourDiscr       uint32
	DesiredMinTxUs  uint32
	RequiredMinRxUs uint32
	RouteHash       uint32
}

func (c *Ctrl) Marshal() []byte {
	b := make([]byte, 40)
	vd := (c.Version&0x7)<<5 | (c.Diag & 0x1f)
	sf := (uint8(c.State)&0x3)<<6 | (c.Flags & 0x3f)
	b[0], b[1], b[2], b[3] = vd, sf, c.DetectMult, 40
	be := binary.BigEndian
	be.PutUint32(b[4:8], c.MyDiscr)
	be.PutUint32(b[8:12], c.YourDiscr)
	be.PutUint32(b[12:16], c.DesiredMinTxUs)
	be.PutUint32(b[16:20], c.RequiredMinRxUs)
	be.PutUint32(b[20:24], c.RouteHash)
	// padding [24:40] left zero
	return b
}

func ParseCtrl(b []byte) (*Ctrl, error) {
	if len(b) < 40 {
		return nil, fmt.Errorf("short")
	}
	if b[3] != 40 {
		return nil, fmt.Errorf("bad length")
	}
	vd, sf := b[0], b[1]
	c := &Ctrl{
		Version:    (vd >> 5) & 0x7,
		Diag:       vd & 0x1f,
		State:      State((sf >> 6) & 0x3),
		Flags:      sf & 0x3f,
		DetectMult: b[2],
		Length:     b[3],
	}
	rd := func(off int) uint32 { return binary.BigEndian.Uint32(b[off : off+4]) }
	c.MyDiscr = rd(4)
	c.YourDiscr = rd(8)
	c.DesiredMinTxUs = rd(12)
	c.RequiredMinRxUs = rd(16)
	c.RouteHash = rd(20)
	return c, nil
}

type Peer struct {
	iface   string
	theirIP string
	ourIP   string
}

func NewPeer(iface string, theirIP net.IP, ourIP net.IP) Peer {
	if theirIP == nil {
		theirIP = net.IPv4zero
	}
	if ourIP == nil {
		ourIP = net.IPv4zero
	}
	return Peer{iface: iface, theirIP: theirIP.String(), ourIP: ourIP.String()}
}

type Session struct {
	route *routing.Route

	myDisc   uint32
	yourDisc uint32
	state    State

	detectMult               uint8
	localTxMin, localRxMin   time.Duration
	remoteTxMin, remoteRxMin time.Duration

	nextTx, detectDeadline, lastRx time.Time

	peer     *Peer
	peerAddr *net.UDPAddr

	mgr *Manager
	mu  sync.Mutex
}

type Manager struct {
	ctx    context.Context
	log    *slog.Logger
	cancel context.CancelFunc
	conn   *net.UDPConn
	port   int

	minTxFloor time.Duration
	maxTxCeil  time.Duration

	mu sync.Mutex
	// peers    map[Peer]struct{}
	sessions map[Peer]*Session
	// idx      map[uint32]*Session // by DstHash

	// composed workers
	sched *Scheduler
	recv  *Receiver

	onUp   func(s *Session)
	onDown func(s *Session)
}

func NewManager(ctx context.Context, log *slog.Logger, iface string, bindIP string, port int) (*Manager, error) {
	laddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", bindIP, port))
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", laddr)
	if err != nil {
		return nil, err
	}

	log.Info("liveness: manager listening on", "address", conn.LocalAddr().String())

	ctx, cancel := context.WithCancel(ctx)
	m := &Manager{
		ctx:    ctx,
		cancel: cancel,
		log:    log,
		conn:   conn,
		port:   port,
		// TODO(snormore): Make these configurable
		minTxFloor: 50 * time.Millisecond,
		maxTxCeil:  1 * time.Second,
		// peers:      make(map[Peer]struct{}),
		sessions: make(map[Peer]*Session),
		// idx:        make(map[uint32]*Session),
		onUp:   func(s *Session) {},
		onDown: func(s *Session) {},
	}

	// Wire scheduler and receiver
	m.sched = NewScheduler(m)
	m.recv = NewReceiver(m, m.sched)

	// Start workers
	m.sched.Start()
	m.recv.Start()

	return m, nil
}

func (m *Manager) Close() error {
	m.cancel()
	time.Sleep(10 * time.Millisecond)
	return m.conn.Close()
}

func (m *Manager) RegisterRoute(r *routing.Route, peerAddr *net.UDPAddr, iface string, txMin, rxMin time.Duration, detectMult uint8) (*Session, error) {
	// peerAddr, err := net.ResolveUDPAddr("udp", peerAddrFor(r, m.port))
	// if err != nil {
	// 	return nil, err
	// }

	peer := NewPeer(iface, r.Dst.IP, r.Src)

	m.log.Info("liveness: registering route", "route", r.String(), "peerAddr", peerAddr, "txMin", txMin, "rxMin", rxMin, "detectMult", detectMult)

	m.mu.Lock()
	if s, ok := m.sessions[peer]; ok {
		m.mu.Unlock()
		return s, nil
	}
	s := &Session{
		route: r,
		// Initial Phase: State = Down, random discriminator
		myDisc:     rand32(),
		state:      Down,
		detectMult: detectMult,
		localTxMin: txMin,
		localRxMin: rxMin,
		peer:       &peer,
		peerAddr:   peerAddr,
		mgr:        m,
	}
	m.sessions[peer] = s
	// schedule TX immediately; DO NOT schedule detect yet (no continuity to monitor)
	m.sched.scheduleTx(time.Now(), s)
	m.mu.Unlock()

	return s, nil
}

func (m *Manager) WithdrawRoute(r *routing.Route, iface string) {
	m.log.Info("liveness: withdrawing route", "route", r.String(), "iface", iface)

	peer := NewPeer(iface, r.Dst.IP, r.Src)

	m.mu.Lock()
	delete(m.sessions, peer)
	m.mu.Unlock()
}

func (m *Manager) AdminDownAll() {
	m.log.Info("liveness: admin down all")

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		s.mu.Lock()
		s.state = AdminDown
		s.mu.Unlock()
	}
}

func (m *Manager) PollAll() {
	m.log.Info("liveness: polling all")

	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for _, s := range m.sessions {
		m.sched.scheduleTx(now, s)
	}
}

func (m *Manager) LocalAddr() *net.UDPAddr {
	if m.conn == nil {
		return nil
	}
	if addr, ok := m.conn.LocalAddr().(*net.UDPAddr); ok {
		return addr
	}
	return nil
}

func rand32() uint32 {
	var b [4]byte
	_, _ = rand.Read(b[:])
	v := binary.BigEndian.Uint32(b[:])
	if v == 0 {
		v = 1
	}
	return v
}
