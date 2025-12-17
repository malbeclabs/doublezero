package kafka

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kversion"
	"github.com/twmb/franz-go/pkg/sasl/aws"
)

type Config struct {
	Brokers []string
	AuthIAM bool
}

func (c *Config) Validate() error {
	if len(c.Brokers) == 0 {
		return errors.New("brokers are required")
	}
	return nil
}

type Client struct {
	client *kgo.Client
}

func NewClient(ctx context.Context, cfg *Config) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate config: %w", err)
	}

	seeds := kgo.SeedBrokers(cfg.Brokers...)
	opts := []kgo.Opt{
		seeds,
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.ProducerBatchCompression(kgo.SnappyCompression()),
		kgo.ProducerLinger(1 * time.Second),
		kgo.MaxVersions(kversion.V2_8_0()),
	}

	if cfg.AuthIAM {
		awsCfg, err := config.LoadDefaultConfig(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to load aws config: %w", err)
		}
		opts = append(opts, kgo.SASL(aws.ManagedStreamingIAM(func(ctx context.Context) (aws.Auth, error) {
			creds, err := awsCfg.Credentials.Retrieve(ctx)
			if err != nil {
				return aws.Auth{}, err
			}
			return aws.Auth{
				AccessKey:    creds.AccessKeyID,
				SecretKey:    creds.SecretAccessKey,
				SessionToken: creds.SessionToken,
			}, nil
		})))
		opts = append(opts, kgo.DialTLS())
	}

	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka client: %w", err)
	}

	return &Client{client: client}, nil
}

func (k *Client) Close() {
	k.client.Close()
}

func (k *Client) Produce(
	ctx context.Context,
	record *kgo.Record,
	fn func(*kgo.Record, error),
) {
	k.client.Produce(ctx, record, fn)
}

func (k *Client) EnsureTopic(
	ctx context.Context,
	topic string,
	partitions int,
	replication int,
) error {
	adm := kadm.NewClient(k.client)
	_, err := adm.CreateTopic(
		ctx,
		int32(partitions),
		int16(replication),
		nil,
		topic,
	)
	if err != nil {
		if strings.Contains(err.Error(), "TOPIC_ALREADY_EXISTS") {
			return nil // ignore error if topic already exists
		}
		return fmt.Errorf("create topic: %w", err)
	}
	return nil
}
