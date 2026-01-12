package gnmi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	gpb "github.com/openconfig/gnmi/proto/gnmi"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/aws"
	"github.com/twmb/franz-go/pkg/sasl/scram"
	"google.golang.org/protobuf/proto"
)

// ErrClientClosed is returned when the Kafka client has been closed.
var ErrClientClosed = errors.New("kafka client closed")

// Consumer defines the interface for consuming gNMI notifications.
type Consumer interface {
	Consume(ctx context.Context) ([]*gpb.Notification, error)
	Commit(ctx context.Context) error
	Close() error
}

// kafkaClient is an interface for the subset of kgo.Client methods we use.
// This allows for mocking in tests.
type kafkaClient interface {
	PollFetches(ctx context.Context) kgo.Fetches
	CommitUncommittedOffsets(ctx context.Context) error
	Close()
}

// KafkaAuthType specifies the authentication method for Kafka.
type KafkaAuthType int

const (
	KafkaAuthTypeSCRAM KafkaAuthType = iota
	KafkaAuthTypeAWSMSK
)

// KafkaConsumer consumes gNMI notifications from a Kafka topic.
type KafkaConsumer struct {
	brokers    []string
	user       string
	pass       string
	topic      string
	group      string
	authType   KafkaAuthType
	disableTLS bool
	client     kafkaClient
	logger     *slog.Logger
	metrics    *ConsumerMetrics
}

// KafkaConsumerOption configures a KafkaConsumer.
type KafkaConsumerOption func(*KafkaConsumer)

// WithKafkaBrokers sets the Kafka broker addresses.
func WithKafkaBrokers(brokers []string) KafkaConsumerOption {
	return func(kc *KafkaConsumer) {
		kc.brokers = brokers
	}
}

// WithKafkaUser sets the SCRAM username.
func WithKafkaUser(user string) KafkaConsumerOption {
	return func(kc *KafkaConsumer) {
		kc.user = user
	}
}

// WithKafkaPassword sets the SCRAM password.
func WithKafkaPassword(pass string) KafkaConsumerOption {
	return func(kc *KafkaConsumer) {
		kc.pass = pass
	}
}

// WithKafkaTopic sets the topic to consume from.
func WithKafkaTopic(topic string) KafkaConsumerOption {
	return func(kc *KafkaConsumer) {
		kc.topic = topic
	}
}

// WithKafkaGroup sets the consumer group ID.
func WithKafkaGroup(group string) KafkaConsumerOption {
	return func(kc *KafkaConsumer) {
		kc.group = group
	}
}

// WithKafkaAuthType sets the authentication type (SCRAM or AWS MSK).
func WithKafkaAuthType(authType KafkaAuthType) KafkaConsumerOption {
	return func(kc *KafkaConsumer) {
		kc.authType = authType
	}
}

// WithKafkaTLSDisabled disables TLS for the Kafka connection.
func WithKafkaTLSDisabled(disabled bool) KafkaConsumerOption {
	return func(kc *KafkaConsumer) {
		kc.disableTLS = disabled
	}
}

// WithKafkaLogger sets the logger for the consumer.
func WithKafkaLogger(logger *slog.Logger) KafkaConsumerOption {
	return func(kc *KafkaConsumer) {
		kc.logger = logger
	}
}

// WithConsumerMetrics sets the metrics for the consumer.
func WithConsumerMetrics(metrics *ConsumerMetrics) KafkaConsumerOption {
	return func(kc *KafkaConsumer) {
		kc.metrics = metrics
	}
}

// withKafkaClient is used for testing to inject a mock client.
func withKafkaClient(client kafkaClient) KafkaConsumerOption {
	return func(kc *KafkaConsumer) {
		kc.client = client
	}
}

// NewKafkaConsumer creates a new KafkaConsumer with the given options.
// Brokers, topic, and consumer group must be configured via their respective options.
func NewKafkaConsumer(opts ...KafkaConsumerOption) (*KafkaConsumer, error) {
	kc := &KafkaConsumer{
		metrics: NewConsumerMetrics(nil), // Always set, unregistered by default
	}
	for _, opt := range opts {
		opt(kc)
	}

	if kc.logger == nil {
		kc.logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	// If a client was injected (for testing), skip creating a real one
	if kc.client != nil {
		return kc, nil
	}

	if len(kc.brokers) == 0 {
		return nil, fmt.Errorf("kafka brokers are required: use WithKafkaBrokers")
	}
	if kc.topic == "" {
		return nil, fmt.Errorf("kafka topic is required: use WithKafkaTopic")
	}
	if kc.group == "" {
		return nil, fmt.Errorf("kafka consumer group is required: use WithKafkaGroup")
	}

	kOpts := []kgo.Opt{}

	switch kc.authType {
	case KafkaAuthTypeSCRAM:
		kOpts = append(kOpts, kgo.SASL(scram.Auth{
			User: kc.user,
			Pass: kc.pass,
		}.AsSha256Mechanism()))
	case KafkaAuthTypeAWSMSK:
		kOpts = append(kOpts, kgo.SASL(aws.ManagedStreamingIAM(func(ctx context.Context) (aws.Auth, error) {
			cfg, err := awsconfig.LoadDefaultConfig(ctx)
			if err != nil {
				return aws.Auth{}, fmt.Errorf("error loading aws config: %w", err)
			}
			creds, err := cfg.Credentials.Retrieve(ctx)
			if err != nil {
				return aws.Auth{}, fmt.Errorf("error retrieving credentials: %w", err)
			}
			return aws.Auth{
				AccessKey:    creds.AccessKeyID,
				SecretKey:    creds.SecretAccessKey,
				SessionToken: creds.SessionToken,
			}, nil
		})))
	}

	if !kc.disableTLS {
		kOpts = append(kOpts, kgo.DialTLS())
	}

	kOpts = append(kOpts,
		kgo.SeedBrokers(kc.brokers...),
		kgo.ConsumeTopics(kc.topic),
		kgo.ConsumerGroup(kc.group),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)

	client, err := kgo.NewClient(kOpts...)
	if err != nil {
		return nil, fmt.Errorf("error creating kafka client: %w", err)
	}
	kc.client = client

	return kc, nil
}

// Consume polls for gNMI notifications from Kafka.
// Messages can be either raw gnmi.Notification or gnmi.SubscribeResponse (with update field).
func (kc *KafkaConsumer) Consume(ctx context.Context) ([]*gpb.Notification, error) {
	kc.logger.Debug("polling for gNMI notifications...")

	fetches := kc.client.PollFetches(ctx)
	if fetches.IsClientClosed() {
		return nil, ErrClientClosed
	}
	if fetches.Empty() {
		return nil, nil
	}

	fetches.EachError(func(topic string, partition int32, err error) {
		kc.logger.Error("error during fetching", "topic", topic, "partition", partition, "error", err)
		kc.metrics.FetchErrors.Inc()
	})

	var notifications []*gpb.Notification
	fetches.EachRecord(func(rec *kgo.Record) {
		// Try to unmarshal as SubscribeResponse first (has update field containing Notification)
		var subscribeResp gpb.SubscribeResponse
		if err := proto.Unmarshal(rec.Value, &subscribeResp); err == nil {
			if update := subscribeResp.GetUpdate(); update != nil {
				notifications = append(notifications, update)
				return
			}
		}

		// Fall back to direct Notification unmarshal
		var notification gpb.Notification
		if err := proto.Unmarshal(rec.Value, &notification); err != nil {
			kc.logger.Error("error unmarshaling gNMI message", "error", err)
			kc.metrics.UnmarshalErrors.Inc()
			return
		}
		notifications = append(notifications, &notification)
	})

	kc.metrics.NotificationsConsumed.Add(float64(len(notifications)))

	return notifications, nil
}

// Commit commits the consumed offsets.
func (kc *KafkaConsumer) Commit(ctx context.Context) error {
	return kc.client.CommitUncommittedOffsets(ctx)
}

// Close closes the Kafka client.
func (kc *KafkaConsumer) Close() error {
	kc.client.Close()
	return nil
}
