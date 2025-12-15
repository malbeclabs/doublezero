package enricher

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"os"

	"github.com/ClickHouse/clickhouse-go/v2"
)

type ClickhouseOption func(*ClickhouseWriter)

type ClickhouseWriter struct {
	db     string
	addr   string
	user   string
	pass   string
	useTLS bool
	conn   clickhouse.Conn
	logger *slog.Logger
}

func WithClickhouseLogger(logger *slog.Logger) ClickhouseOption {
	return func(cw *ClickhouseWriter) {
		cw.logger = logger
	}
}

func WithClickhouseDB(db string) ClickhouseOption {
	return func(cw *ClickhouseWriter) {
		cw.db = db
	}
}

func WithClickhouseUser(user string) ClickhouseOption {
	return func(cw *ClickhouseWriter) {
		cw.user = user
	}
}

func WithClickhousePassword(pass string) ClickhouseOption {
	return func(cw *ClickhouseWriter) {
		cw.pass = pass
	}
}

func WithClickhouseAddr(addr string) ClickhouseOption {
	return func(cw *ClickhouseWriter) {
		cw.addr = addr
	}
}

func WithTLS(useTLS bool) ClickhouseOption {
	return func(cw *ClickhouseWriter) {
		cw.useTLS = useTLS
	}
}

func NewClickhouseWriter(opts ...ClickhouseOption) (*ClickhouseWriter, error) {
	cw := &ClickhouseWriter{
		user:   "default",
		pass:   "default",
		addr:   "localhost:9440",
		useTLS: false,
	}
	for _, opt := range opts {
		opt(cw)
	}

	if cw.logger == nil {
		cw.logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	chOpts := &clickhouse.Options{
		Addr: []string{cw.addr},
		Auth: clickhouse.Auth{
			Database: cw.db,
			Username: cw.user,
			Password: cw.pass,
		},
	}
	if cw.useTLS {
		chOpts.TLS = &tls.Config{}
	}
	conn, err := clickhouse.Open(chOpts)
	if err != nil {
		return nil, err
	}
	cw.conn = conn
	return cw, nil
}

func (cw *ClickhouseWriter) BatchInsert(ctx context.Context, samples []FlowSample) error {
	if len(samples) == 0 {
		return nil
	}
	batch, err := cw.conn.PrepareBatch(ctx, `INSERT INTO default.flows (
				type,
				time_received_ns,
				sequence_num,
				sampling_rate,
				sampler_address,
				time_flow_start_ns,
				time_flow_end_ns,
				bytes,
				packets,
				src_addr,
				dst_addr,
				etype,
				proto,
				src_port,
				dst_port,
				in_if,
				out_if,
				src_mac,
				dst_mac,
				src_vlan,
				dst_vlan,
				vlan_id,
				ip_tos,
				forwarding_status,
				ip_ttl,
				ip_flags,
				tcp_flags,
				icmp_type,
				icmp_code,
				ipv6_flow_label,
				fragment_id,
				fragment_offset,
				src_as,
				dst_as,
				next_hop,
				next_hop_as,
				src_net,
				dst_net,
				bgp_next_hop,
				bgp_communities,
				as_path,
				mpls_ttl,
				mpls_label,
				mpls_ip,
				observation_domain_id,
				observation_point_id,
				layer_stack,
				layer_size,
				ipv6_routing_header_addresses,
				ipv6_routing_header_seg_left,
				in_ifname,
				out_ifname
			)`)
	if err != nil {
		return fmt.Errorf("error beginning clickhouse batch: %v", err)
	}
	for _, sample := range samples {
		// TODO: metric how many samples we've processed
		// TODO: metric how many batches we've written

		ipv6Addrs := make([]string, len(sample.Ipv6RoutingHeaderAddresses))
		for i, ip := range sample.Ipv6RoutingHeaderAddresses {
			ipv6Addrs[i] = ip.String()
		}
		err = batch.Append(
			sample.Type,
			sample.TimeReceivedNs,
			sample.SequenceNum,
			sample.SamplingRate,
			sample.SamplerAddress.String(),
			sample.TimeFlowStartNs,
			sample.TimeFlowEndNs,
			sample.Bytes,
			sample.Packets,
			sample.SrcAddress.String(),
			sample.DstAddress.String(),
			sample.EType,
			sample.Proto,
			sample.SrcPort,
			sample.DstPort,
			sample.InputIfIndex,
			sample.OutputIfIndex,
			sample.SrcMac,
			sample.DstMac,
			sample.SrcVlan,
			sample.DstVlan,
			sample.VlanId,
			sample.IpTos,
			sample.ForwardingStatus,
			sample.IpTtl,
			sample.IpFlags,
			sample.TcpFlags,
			sample.IcmpType,
			sample.IcmpCode,
			sample.Ipv6FlowLabel,
			sample.FragmentId,
			sample.FragmentOffset,
			sample.SrcAs,
			sample.DstAs,
			sample.NextHop.String(),
			sample.NextHopAs,
			sample.SrcNet,
			sample.DstNet,
			sample.BgpNextHop.String(),
			sample.BgpCommunities,
			sample.AsPath,
			sample.MplsTtl,
			sample.MplsLabel,
			sample.MplsIp,
			sample.ObservationDomainId,
			sample.ObservationPointId,
			sample.LayerStack,
			sample.LayerSize,
			ipv6Addrs,
			sample.Ipv6RoutingHeaderSegLeft,
			sample.InputInterface,
			sample.OutputInterface,
		)
		if err != nil {
			cw.logger.Error("error appending to clickhouse batch", "error", err)
		}
	}
	if err := batch.Send(); err != nil {
		_ = batch.Close()
		return fmt.Errorf("error sending clickhouse batch: %v", err)
	}
	if err := batch.Close(); err != nil {
		return fmt.Errorf("error closing clickhouse batch: %v", err)
	}
	cw.logger.Info("sent records to clickhouse", "count", len(samples))
	return nil
}
