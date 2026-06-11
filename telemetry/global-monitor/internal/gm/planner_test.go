package gm

import (
	"fmt"
	"net"
	"sync"

	"github.com/gagliardetto/solana-go"
	chwriter "github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/clickhouse"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/dz"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/sol"
	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/geoip"
)

type fakeGeoIP struct {
	rec *geoip.Record
}

var _ geoip.Resolver = (*fakeGeoIP)(nil)

func (g *fakeGeoIP) Resolve(_ net.IP) *geoip.Record { return g.rec }

// fakeProbeWriter implements chwriter.ProbeWriter and captures appended rows.
type fakeProbeWriter struct {
	mu             sync.Mutex
	solICMPRows    []chwriter.SolanaValidatorICMPProbeRow
	solTPUQUICRows []chwriter.SolanaValidatorTPUQUICProbeRow
	dzUserICMPRows []chwriter.DoubleZeroUserICMPProbeRow
}

var _ chwriter.ProbeWriter = (*fakeProbeWriter)(nil)

func newFakeProbeWriter() *fakeProbeWriter { return &fakeProbeWriter{} }

func (f *fakeProbeWriter) AppendSolanaValidatorICMPProbe(row chwriter.SolanaValidatorICMPProbeRow) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.solICMPRows = append(f.solICMPRows, row)
}

func (f *fakeProbeWriter) AppendSolanaValidatorTPUQUICProbe(row chwriter.SolanaValidatorTPUQUICProbeRow) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.solTPUQUICRows = append(f.solTPUQUICRows, row)
}

func (f *fakeProbeWriter) AppendDoubleZeroUserICMPProbe(row chwriter.DoubleZeroUserICMPProbeRow) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dzUserICMPRows = append(f.dzUserICMPRows, row)
}

func (f *fakeProbeWriter) SolICMPRows() []chwriter.SolanaValidatorICMPProbeRow {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]chwriter.SolanaValidatorICMPProbeRow, len(f.solICMPRows))
	copy(out, f.solICMPRows)
	return out
}

func (f *fakeProbeWriter) SolTPUQUICRows() []chwriter.SolanaValidatorTPUQUICProbeRow {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]chwriter.SolanaValidatorTPUQUICProbeRow, len(f.solTPUQUICRows))
	copy(out, f.solTPUQUICRows)
	return out
}

func (f *fakeProbeWriter) DZUserICMPRows() []chwriter.DoubleZeroUserICMPProbeRow {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]chwriter.DoubleZeroUserICMPProbeRow, len(f.dzUserICMPRows))
	copy(out, f.dzUserICMPRows)
	return out
}

func pk(i byte) solana.PublicKey {
	b := make([]byte, 32)
	b[31] = i
	return solana.PublicKeyFromBytes(b)
}

func ip4(s string) net.IP { return net.ParseIP(s).To4() }

func mkUser(pub solana.PublicKey, clientIP, dzip string, exch string, userType dz.UserType, validatorPK solana.PublicKey) dz.User {
	return dz.User{
		PubKey:      pub,
		ClientIP:    ip4(clientIP),
		DZIP:        ip4(dzip),
		UserType:    userType,
		ValidatorPK: validatorPK,
		Device: &dz.Device{
			Code: fmt.Sprintf("dev-%s", pub.String()[:6]),
			Exchange: &dz.Exchange{
				Code: exch,
				Name: "Exchange " + exch,
			},
		},
	}
}

func mkSource(publicIface string, publicIP string, dzIface string, sourceUser *dz.User) *Source {
	return &Source{
		PublicIface: publicIface,
		PublicIP:    ip4(publicIP),
		DZIface:     dzIface,
		Metro:       "yyz",
		MetroName:   "Toronto",
		Host:        "host1",
		User:        sourceUser,
	}
}

func mkValidator(pub solana.PublicKey, gossipIP string) *sol.Validator {
	return &sol.Validator{
		Node: sol.GossipNode{
			Pubkey:   pub,
			GossipIP: net.ParseIP(gossipIP).To4(),
		},
		LeaderRatio: 0,
	}
}

func mkValidatorTPU(pub solana.PublicKey, tpuquicIP string, port uint16) *sol.Validator {
	return &sol.Validator{
		Node: sol.GossipNode{
			Pubkey:    pub,
			TPUQUICIP: net.ParseIP(tpuquicIP).To4(),
			TPUQUICPort: func() uint16 {
				return port
			}(),
		},
		LeaderRatio: 0,
	}
}
