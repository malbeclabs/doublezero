package enricher

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	flow "github.com/malbeclabs/doublezero/telemetry/proto/flow/gen/pb-go"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/aws"
	"github.com/twmb/franz-go/pkg/sasl/scram"
	"google.golang.org/protobuf/proto"
)

// kafkaClient is an interface for the subset of kgo.Client methods we use.
// This allows for mocking in tests.
type kafkaClient interface {
	PollFetches(ctx context.Context) kgo.Fetches
	CommitUncommittedOffsets(ctx context.Context) error
	Close()
}

type KafkaFlowConsumer struct {
	broker     []string
	user       string
	pass       string
	topic      string
	group      string
	authType   KafkaAuthType
	disableTLS bool
	client     kafkaClient
	logger     *slog.Logger
	metrics    *FlowConsumerMetrics
}

type KafkaOption func(*KafkaFlowConsumer)

func WithKafkaLogger(logger *slog.Logger) KafkaOption {
	return func(kfc *KafkaFlowConsumer) {
		kfc.logger = logger
	}
}

func WithKafkaBroker(brokers []string) KafkaOption {
	return func(kfc *KafkaFlowConsumer) {
		kfc.broker = brokers
	}
}

func WithKafkaUser(user string) KafkaOption {
	return func(kfc *KafkaFlowConsumer) {
		kfc.user = user
	}
}

func WithKafkaPassword(pass string) KafkaOption {
	return func(kfc *KafkaFlowConsumer) {
		kfc.pass = pass
	}
}

func WithKafkaConsumerTopic(topic string) KafkaOption {
	return func(kfc *KafkaFlowConsumer) {
		kfc.topic = topic
	}
}

func WithKafkaConsumerGroup(group string) KafkaOption {
	return func(kfc *KafkaFlowConsumer) {
		kfc.group = group
	}
}

func WithKafkaTLSDisabled(disableTLS bool) KafkaOption {
	return func(kfc *KafkaFlowConsumer) {
		kfc.disableTLS = disableTLS
	}
}

type KafkaAuthType int

const (
	KafkaAuthTypeSCRAM KafkaAuthType = iota
	KafkaAuthTypeAWSMSK
)

func WithKafkaAuthType(authType KafkaAuthType) KafkaOption {
	return func(kfc *KafkaFlowConsumer) {
		kfc.authType = authType
	}
}

func WithFlowConsumerMetrics(metrics *FlowConsumerMetrics) KafkaOption {
	return func(kfc *KafkaFlowConsumer) {
		kfc.metrics = metrics
	}
}

// withKafkaClient is used for testing to inject a mock client.
func withKafkaClient(client kafkaClient) KafkaOption {
	return func(kfc *KafkaFlowConsumer) {
		kfc.client = client
	}
}

func NewKafkaFlowConsumer(opts ...KafkaOption) (*KafkaFlowConsumer, error) {
	kfc := &KafkaFlowConsumer{}
	for _, opt := range opts {
		opt(kfc)
	}
	if kfc.logger == nil {
		kfc.logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	// If a client was injected (for testing), skip creating a real one
	if kfc.client != nil {
		return kfc, nil
	}

	kOpts := []kgo.Opt{}
	if kfc.authType == KafkaAuthTypeSCRAM {
		kOpts = append(kOpts,
			kgo.SASL(scram.Auth{
				User: kfc.user,
				Pass: kfc.pass,
			}.AsSha256Mechanism()),
		)
	}
	if kfc.authType == KafkaAuthTypeAWSMSK {
		kOpts = append(kOpts, kgo.SASL(aws.ManagedStreamingIAM(func(ctx context.Context) (aws.Auth, error) {
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
		})))
	}
	if !kfc.disableTLS {
		kOpts = append(kOpts, kgo.DialTLS())
	}
	kOpts = append(kOpts,
		kgo.SeedBrokers(kfc.broker...),
		kgo.ConsumeTopics(kfc.topic),
		kgo.ConsumerGroup(kfc.group),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	client, err := kgo.NewClient(kOpts...)
	if err != nil {
		return nil, fmt.Errorf("error creating kafka client: %v", err)
	}
	kfc.client = client
	return kfc, nil
}

func (kfc *KafkaFlowConsumer) ConsumeFlowRecords(ctx context.Context) ([]FlowSample, error) {
	kfc.logger.Info("polling for records to enrich...")
	fetches := kfc.client.PollFetches(ctx)
	if fetches.IsClientClosed() {
		return nil, nil
	}
	if fetches.Empty() {
		kfc.logger.Info("no records to enrich")
		return nil, nil
	}
	fetches.EachError(func(topic string, partition int32, err error) {
		kfc.logger.Error("error during fetching", "topic", topic, "partition", partition, "error", err)
	})
	samples := []FlowSample{}
	fetches.EachRecord(func(rec *kgo.Record) {
		var sample flow.FlowSample
		if err := proto.Unmarshal(rec.Value, &sample); err != nil {
			kfc.logger.Error("error unmarshaling flow record from kafka", "error", err)
			kfc.metrics.FlowUnmarshalErrors.Inc()
			return
		}
		decoded, err := DecodeSFlow(&sample)
		if err != nil {
			kfc.logger.Error("error decoding sFlow", "error", err)
			kfc.metrics.FlowDecodeErrors.Inc()
			return
		}
		samples = append(samples, decoded...)
	})
	kfc.metrics.FlowsDecodedTotal.Add(float64(len(samples)))
	return samples, nil
}

func (kfc *KafkaFlowConsumer) CommitOffsets(ctx context.Context) error {
	return kfc.client.CommitUncommittedOffsets(ctx)
}

func (kfc *KafkaFlowConsumer) Close() error {
	kfc.client.Close()
	return nil
}
