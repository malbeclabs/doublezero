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
	"database/sql"
	"encoding/json"
	"fmt"
	"log"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/scram"
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

// WithRedpandaBroker sets the redpanda broker for the enricher.
func WithRedpandaBroker(broker string) EnricherOption {
	return func(e *Enricher) {
		e.rpBroker = broker
	}
}

// WithRedpandaCreds sets the username/password credentials for the
// connection to redpanda.
func WithRedpandaCreds(user, password string) EnricherOption {
	return func(e *Enricher) {
		e.rpUser = user
		e.rpPass = password
	}
}

// WithRedpandaTLSEnabled enables/disables TLS on the connection to redpanda.
func WithRedpandaTLSEnabled(enabled bool) EnricherOption {
	return func(e *Enricher) {
		e.rpTLS = enabled
	}
}

// WithRedpandaConsumerTopic sets the topic to consume from for unenriched
// flow records.
func WithRedpandaConsumerTopic(topic string) EnricherOption {
	return func(e *Enricher) {
		e.rpConsumerTopic = topic
	}
}

// WithRedpandaConsumerGroup sets the consumer group name when consuming
// unenriched flow records.
func WithRedpandaConsumerGroup(group string) EnricherOption {
	return func(e *Enricher) {
		e.rpConsumerGroup = group
	}
}

// WithRedpandaProducerTopic sets the topic to send enriched flow records.
func WithRedpandaProducerTopic(topic string) EnricherOption {
	return func(e *Enricher) {
		e.rpProducerTopic = topic
	}
}

type Enricher struct {
	chAddr          string
	chUser          string
	chPass          string
	chTLS           bool
	chConn          *sql.DB
	rpBroker        string
	rpTLS           bool
	rpUser          string
	rpPass          string
	rpConsumerTopic string // topic to consume unenriched flow records
	rpConsumerGroup string
	rpProducerTopic string // topic to produce enriched flow records
	rpConn          *kgo.Client
	annotators      []Annotator
}

func NewEnricher(opts ...EnricherOption) *Enricher {
	e := &Enricher{
		chAddr:          "localhost:9440",
		chUser:          "default",
		chPass:          "default",
		chTLS:           true,
		rpBroker:        "localhost:9000",
		rpTLS:           true,
		rpConsumerTopic: "flows_raw",
		rpConsumerGroup: "enricher",
		rpProducerTopic: "flows_enriched",
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
	e.chConn = clickhouse.OpenDB(chOpts)

	// Setup redpanda client
	rpOpts := []kgo.Opt{}
	rpOpts = append(rpOpts,
		kgo.SeedBrokers(e.rpBroker),
		kgo.SASL(scram.Auth{User: e.rpUser, Pass: e.rpPass}.AsSha256Mechanism()),
		kgo.SeedBrokers(e.rpBroker),
		kgo.ConsumeTopics(e.rpConsumerTopic),
		kgo.ConsumerGroup(e.rpConsumerGroup),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	if e.rpTLS {
		rpOpts = append(rpOpts, kgo.DialTLSConfig(new(tls.Config)))
	}

	client, err := kgo.NewClient(rpOpts...)
	if err != nil {
		return fmt.Errorf("error creating redpanda client: %v", err)
	}
	e.rpConn = client
	defer e.rpConn.Close()

	if err := e.RegisterAnnotators(ctx); err != nil {
		return fmt.Errorf("error while initializing annotators: %v", err)
	}

	// Let's annotate some flow records
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			fetches := client.PollFetches(ctx)
			if fetches.IsClientClosed() {
				return nil
			}
			if fetches.Empty() {
				continue
			}
			fetches.EachError(func(topic string, partition int32, err error) {
				log.Printf("error during fetching on topic %s, parition %d: %v", topic, partition, err)
			})
			fetches.EachRecord(func(record *kgo.Record) {
				// unmarshal flow record
				flow := &FlowSample{}
				err := json.Unmarshal(record.Value, flow)
				if err != nil {
					log.Printf("error unmarshalling flow record: %v", err)
					return
				}
				// annotate flow record
				for _, a := range e.annotators {
					if err := a.Annotate(flow); err != nil {
						log.Printf("error annotating flow with %s: %v", a.String(), err)
					}
				}
				// write flow record back onto topic
				body, err := json.Marshal(flow)
				if err != nil {
					log.Printf("error marshaling enriched record to json: %v", err)
					return
				}
				record.Topic = e.rpProducerTopic
				record.Value = body
				client.Produce(ctx, record, func(record *kgo.Record, err error) {
					if err != nil {
						// TODO: metric
						fmt.Printf("error producing message to redpanda: %v \n", err)
					}
				})
			})
			if err := client.CommitUncommittedOffsets(ctx); err != nil {
				// TODO: metric
				log.Printf("commit records failed: %v\n", err)
				continue
			}
		}
	}
}

type Annotator interface {
	Init(context.Context, *sql.DB) error
	Annotate(*FlowSample) error
	String() string
}

// RegisterAnnotators initializes a set of annotators for use during enrichment.
// Annotators must implement the Annotator interface.
func (e *Enricher) RegisterAnnotators(ctx context.Context) error {
	e.annotators = []Annotator{
		NewIfNameAnnotator(),
	}

	for _, a := range e.annotators {
		if err := a.Init(ctx, e.chConn); err != nil {
			return fmt.Errorf("error initializing annotator %s: %v", a.String(), err)
		}
	}
	return nil
}
