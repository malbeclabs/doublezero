package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

func ensureTopic(admin *kafka.AdminClient, topic string, partitions, rf int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	specs := []kafka.TopicSpecification{{
		Topic:             topic,
		NumPartitions:     partitions,
		ReplicationFactor: rf,
	}}

	results, err := admin.CreateTopics(ctx, specs, kafka.SetAdminOperationTimeout(15*time.Second))
	if err != nil {
		return fmt.Errorf("CreateTopics RPC failed: %w", err)
	}

	if len(results) != 1 {
		return fmt.Errorf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	switch r.Error.Code() {
	case kafka.ErrNoError:
		fmt.Printf("created topic %s\n", r.Topic)
		return nil
	case kafka.ErrTopicAlreadyExists:
		fmt.Printf("topic %s already exists, OK\n", r.Topic)
		return nil
	default:
		return fmt.Errorf("failed to create topic %s: %w", r.Topic, r.Error)
	}
}

func main() {
	topic := "test-topic"

	cfg := &kafka.ConfigMap{
		"bootstrap.servers": "localhost:19092",
		"security.protocol": "PLAINTEXT",
		// "debug":               "broker,protocol,security",

		// Without proxy
		// "security.protocol": "SASL_SSL",
		// "sasl.mechanism": "AWS_MSK_IAM",
		// "sasl.username": "unused",
		// "sasl.password": "unused",
		// "sasl.oauthbearer.method": "aws_msk_iam",
	}

	admin, err := kafka.NewAdminClient(cfg)
	if err != nil {
		log.Fatalf("failed to create admin client: %v", err)
	}
	defer admin.Close()
	log.Printf("created admin client")

	m, err := admin.GetMetadata(nil, true, 10_000)
	if err != nil {
		log.Fatalf("metadata failed: %v", err)
	}
	log.Printf("got metadata for %d topics", len(m.Topics))

	if err := ensureTopic(admin, topic, 1, 1); err != nil {
		log.Fatalf("failed to ensure topic: %v", err)
	}
	log.Printf("ensured topic %q", topic)

	// ---- PRODUCER TEST ----
	prod, err := kafka.NewProducer(cfg)
	if err != nil {
		log.Fatalf("failed to create producer: %v", err)
	}
	defer prod.Close()

	deliveryChan := make(chan kafka.Event, 5)

	log.Printf("producing 5 messages")
	for i := 0; i < 5; i++ {
		value := fmt.Sprintf("msg-%d @ %s", i, time.Now().Format(time.RFC3339Nano))
		err = prod.Produce(&kafka.Message{
			TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny},
			Value:          []byte(value),
		}, deliveryChan)
		if err != nil {
			log.Fatalf("produce failed: %v", err)
		}
	}

	// Wait for delivery reports
	for i := 0; i < 5; i++ {
		e := <-deliveryChan
		m, ok := e.(*kafka.Message)
		if !ok {
			log.Printf("got non-message event: %#v", e)
			continue
		}
		if m.TopicPartition.Error != nil {
			log.Fatalf("delivery failed for %v: %v", m.TopicPartition, m.TopicPartition.Error)
		}
		log.Printf("delivered message to %v", m.TopicPartition)
	}
	close(deliveryChan)

	// ---- CONSUMER TEST ----
	consumerCfg := &kafka.ConfigMap{}
	for k, v := range *cfg {
		(*consumerCfg)[k] = v
	}
	(*consumerCfg)["group.id"] = fmt.Sprintf("test-group-%d", time.Now().UnixNano())
	(*consumerCfg)["auto.offset.reset"] = "earliest"
	(*consumerCfg)["enable.auto.commit"] = false

	cons, err := kafka.NewConsumer(consumerCfg)
	if err != nil {
		log.Fatalf("failed to create consumer: %v", err)
	}
	defer cons.Close()

	if err := cons.SubscribeTopics([]string{topic}, nil); err != nil {
		log.Fatalf("subscribe failed: %v", err)
	}
	log.Printf("subscribed consumer to %q, waiting for messages", topic)

	expected := 5
	received := 0
	deadline := time.Now().Add(20 * time.Second)

	for received < expected && time.Now().Before(deadline) {
		ev := cons.Poll(500)
		if ev == nil {
			continue
		}
		switch e := ev.(type) {
		case *kafka.Message:
			log.Printf("consumed message from %v: %s", e.TopicPartition, string(e.Value))
			received++
		case kafka.Error:
			log.Printf("consumer error: %v", e)
		default:
			// ignore other events (rebalance, etc)
		}
	}

	if received < expected {
		log.Fatalf("consumer timed out: expected %d, got %d", expected, received)
	}

	log.Printf("success: produced and consumed %d messages on %q via proxy", received, topic)
}
