package gm

import (
	"fmt"
	"net"
	"sync"
	"testing"

	"github.com/gagliardetto/solana-go"
	influxdb2api "github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/influxdata/influxdb-client-go/v2/api/write"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/dz"
	"github.com/malbeclabs/doublezero/telemetry/global-monitor/internal/sol"
	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/geoip"
	"github.com/stretchr/testify/require"
)

func requireTag(t *testing.T, tags map[string]string, k, want string) {
	t.Helper()
	v, ok := tags[k]
	require.True(t, ok, "missing tag %q", k)
	require.Equal(t, want, v, "tag %q mismatch", k)
}

func requireField[T any](t *testing.T, fields map[string]any, k string) T {
	t.Helper()
	v, ok := fields[k]
	require.True(t, ok, "missing field %q", k)
	out, ok := v.(T)
	require.True(t, ok, "field %q has type %T", k, v)
	return out
}

func pointTags(p *write.Point) map[string]string {
	out := map[string]string{}
	for _, t := range p.TagList() {
		out[t.Key] = t.Value
	}
	return out
}

func pointFields(p *write.Point) map[string]any {
	out := map[string]any{}
	for _, f := range p.FieldList() {
		out[f.Key] = f.Value
	}
	return out
}

type fakeWriteAPI struct {
	mu     sync.Mutex
	points []*write.Point
	errCh  chan error
}

var _ influxdb2api.WriteAPI = (*fakeWriteAPI)(nil)

func newFakeWriteAPI() *fakeWriteAPI { return &fakeWriteAPI{errCh: make(chan error, 1)} }

func (f *fakeWriteAPI) WritePoint(p *write.Point) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.points = append(f.points, p)
}
func (f *fakeWriteAPI) WriteRecord(_ string)                                       {}
func (f *fakeWriteAPI) Flush()                                                     {}
func (f *fakeWriteAPI) Errors() <-chan error                                       { return f.errCh }
func (f *fakeWriteAPI) SetWriteFailedCallback(cb influxdb2api.WriteFailedCallback) {}

func (f *fakeWriteAPI) Points() []*write.Point {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*write.Point, len(f.points))
	copy(out, f.points)
	return out
}

type fakeGeoIP struct {
	rec *geoip.Record
}

var _ geoip.Resolver = (*fakeGeoIP)(nil)

func (g *fakeGeoIP) Resolve(_ net.IP) *geoip.Record { return g.rec }

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
