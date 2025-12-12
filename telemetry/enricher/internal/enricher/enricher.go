// Package enricher implements the enricher process and associated annotators.
// The enricher process reads off of redpanda topic containing unenriched flow
// records in json format, enriches the flow with additional information from
// each annotator, and publishes the flow to an enriched redpanda topic.
//
// Annotators must be registered in the RegisterAnnotators method of the enricher
// and must implement the Annotator interface.
package enricher

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"

	"google.golang.org/protobuf/proto"

	"github.com/ClickHouse/clickhouse-go/v2"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	flow "github.com/malbeclabs/doublezero/telemetry/proto/flow/gen/pb-go"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/aws"
	"github.com/twmb/franz-go/plugin/kprom"
)

type EnricherOption func(*Enricher)

// WithClickhouseAddr adds the address of the clickhouse server/cluster.
func WithClickhouseAddr(addr string) EnricherOption {
	return func(e *Enricher) {
		e.chAddr = addr
	}
}

// WithClickhouseCreds sets the username/password credentials for the
// clickhouse server/cluster.
func WithClickhouseCreds(user, password string) EnricherOption {
	return func(e *Enricher) {
		e.chUser = user
		e.chPass = password
	}
}

// WithClickhouseTLSEnabled enables/disables TLS on the connection to clickhouse.
func WithClickhouseTLSEnabled(enabled bool) EnricherOption {
	return func(e *Enricher) {
		e.chTLS = enabled
	}
}

// WithKafkaBroker sets the kafka broker for the enricher.
func WithKafkaBroker(broker string) EnricherOption {
	return func(e *Enricher) {
		e.kBroker = broker
	}
}

// WithKafkaCreds sets the username/password credentials for the
// connection to kafka.
func WithKafkaCreds(user, password string) EnricherOption {
	return func(e *Enricher) {
		e.kUser = user
		e.kPass = password
	}
}

// WithKafkaTLSEnabled enables/disables TLS on the connection to kafka.
func WithKafkaTLSEnabled(enabled bool) EnricherOption {
	return func(e *Enricher) {
		e.kTLS = enabled
	}
}

// WithKafkaConsumerTopic sets the topic to consume from for unenriched
// flow records.
func WithKafkaConsumerTopic(topic string) EnricherOption {
	return func(e *Enricher) {
		e.kConsumerTopic = topic
	}
}

// WithKafkaConsumerGroup sets the consumer group name when consuming
// unenriched flow records.
func WithKafkaConsumerGroup(group string) EnricherOption {
	return func(e *Enricher) {
		e.kConsumerGroup = group
	}
}

// WithKafkaProducerTopic sets the topic to send enriched flow records.
func WithKafkaProducerTopic(topic string) EnricherOption {
	return func(e *Enricher) {
		e.kProducerTopic = topic
	}
}

// WithKafkaMetrics enables prometheus metrics on production/consumption
// via kafka.
func WithKafkaMetrics(enabled bool) EnricherOption {
	return func(e *Enricher) {
		e.kMetrics = enabled
	}
}

type Enricher struct {
	chAddr         string
	chUser         string
	chPass         string
	chTLS          bool
	chConn         clickhouse.Conn
	kBroker        string
	kTLS           bool
	kUser          string
	kPass          string
	kConsumerTopic string // topic to consume unenriched flow records
	kConsumerGroup string
	kProducerTopic string // topic to produce enriched flow records
	kMetrics       bool
	kConn          *kgo.Client
	annotators     []Annotator
}

func NewEnricher(opts ...EnricherOption) *Enricher {
	e := &Enricher{
		chAddr:         "localhost:9440",
		chUser:         "default",
		chPass:         "default",
		chTLS:          true,
		kBroker:        "localhost:9000",
		kTLS:           true,
		kConsumerTopic: "flows_raw",
		kConsumerGroup: "enricher",
		kProducerTopic: "flows_enriched",
		kMetrics:       false,
	}

	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Run starts the enricher instance, sets up a clickhouse client, a redpanda client,
// initializes registered annotators and begins enriching flow records via redpanda.
func (e *Enricher) Run(ctx context.Context) error {
	// Setup clickhouse client
	chOpts := &clickhouse.Options{
		Addr:     []string{e.chAddr},
		Protocol: clickhouse.Native,
		Auth: clickhouse.Auth{
			Username: e.chUser,
			Password: e.chPass,
		},
	}
	if e.chTLS {
		chOpts.TLS = &tls.Config{}
	}
	conn, err := clickhouse.Open(chOpts)
	if err != nil {
		return fmt.Errorf("error opening native clickhouse connection: %w", err)
	}
	e.chConn = conn

	// Setup kafa client
	kOpts := []kgo.Opt{}
	kOpts = append(kOpts,
		kgo.SeedBrokers(e.kBroker),
		// kgo.SASL(scram.Auth{User: e.kUser, Pass: e.kPass}.AsSha256Mechanism()),
		kgo.SASL(aws.ManagedStreamingIAM(func(ctx context.Context) (aws.Auth, error) {
			cfg, err := awsconfig.LoadDefaultConfig(ctx)
			if err != nil {
				return aws.Auth{}, fmt.Errorf("failed to load aws config: %w", err)
			}

			// Retrieve the temporary credentials
			creds, err := cfg.Credentials.Retrieve(ctx)
			if err != nil {
				return aws.Auth{}, fmt.Errorf("failed to retrieve credentials: %w", err)
			}

			// Return them in the format franz-go expects
			return aws.Auth{
				AccessKey:    creds.AccessKeyID,
				SecretKey:    creds.SecretAccessKey,
				SessionToken: creds.SessionToken,
			}, nil
		})),
		kgo.SeedBrokers(e.kBroker),
		kgo.ConsumeTopics(e.kConsumerTopic),
		kgo.ConsumerGroup(e.kConsumerGroup),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		kgo.DialTLS(),
	)
	if e.kTLS {
		kOpts = append(kOpts, kgo.DialTLSConfig(new(tls.Config)))
	}

	if e.kMetrics {
		metrics := kprom.NewMetrics("kgo")
		kOpts = append(kOpts, kgo.WithHooks(metrics))

		go func() {
			http.Handle("/metrics", metrics.Handler())
			log.Fatal(http.ListenAndServe("localhost:2112", nil))
		}()
	}

	log.Println("creating kafka client")
	client, err := kgo.NewClient(kOpts...)
	if err != nil {
		return fmt.Errorf("error creating redpanda client: %v", err)
	}
	e.kConn = client
	defer e.kConn.Close()

	if err := e.RegisterAnnotators(ctx); err != nil {
		return fmt.Errorf("error while initializing annotators: %v", err)
	}

	// Let's annotate some flow records
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			log.Println("polling for records to enrich...")
			fetches := client.PollFetches(ctx)
			log.Println("polling for records to enrich...")
			if fetches.IsClientClosed() {
				return nil
			}
			if fetches.Empty() {
				log.Println("no records to enrich")
				continue
			}
			fetches.EachError(func(topic string, partition int32, err error) {
				log.Printf("error during fetching on topic %s, parition %d: %v", topic, partition, err)
			})

			batch, err := e.chConn.PrepareBatch(ctx, `INSERT INTO default.flows (
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
				log.Printf("error beginning clickhouse batch: %v", err)
				continue
			}

			var count int
			fetches.EachRecord(func(record *kgo.Record) {
				log.Println("received record for enrichment")

				// unmarshal protobuf
				var sample flow.FlowSample
				if err := proto.Unmarshal(record.Value, &sample); err != nil {
					log.Printf("error unmarshalling protobuf: %v", err)
					return
				}

				// decode sflow samples
				samples, err := DecodeSFlow(&sample)
				if err != nil {
					log.Printf("error decoding sflow record: %v", err)
					return
				}
				if len(samples) == 0 {
					return
				}

				// write to clickhouse
				// TODO: metric how many samples we've processed
				// TODO: metric how many batches we've written
				// TODO: metric the time to write a batch
				for _, sample := range samples {
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
						log.Printf("error appending to clickhouse batch: %v", err)
					} else {
						count++
					}
				}
			})

			if count > 0 {
				if err := batch.Send(); err != nil {
					log.Printf("error sending clickhouse batch: %v", err)
					_ = batch.Close()
					continue
				}
			}
			if err := batch.Close(); err != nil {
				log.Printf("error closing clickhouse batch: %v", err)
			}

			if err := e.kConn.CommitUncommittedOffsets(ctx); err != nil {
				// TODO: metric
				log.Printf("commit records failed: %v\n", err)
				continue
			}
		}
	}
}

type Annotator interface {
	Init(context.Context, clickhouse.Conn) error
	Annotate(*FlowSample) error
	String() string
}

// RegisterAnnotators initializes a set of annotators for use during enrichment.
// Annotators must implement the Annotator interface.
func (e *Enricher) RegisterAnnotators(ctx context.Context) error {
	e.annotators = []Annotator{
		// NewIfNameAnnotator(),
	}

	for _, a := range e.annotators {
		if err := a.Init(ctx, e.chConn); err != nil {
			return fmt.Errorf("error initializing annotator %s: %v", a.String(), err)
		}
	}
	return nil
}
