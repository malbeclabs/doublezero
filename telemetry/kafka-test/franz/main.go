package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kversion"
)

func main() {
	topic := "test-topic"
	groupID := fmt.Sprintf("franz-test-group-%d", time.Now().UnixNano())

	// Single client used for admin, produce, and consume.
	cl, err := kgo.NewClient(
		kgo.SeedBrokers("localhost:19092"), // talking plaintext to the proxy
		// <<< IMPORTANT: avoid the broken ApiVersions response via proxy
		kgo.MaxVersions(kversion.V2_8_0()),
		// Consumer config
		kgo.ConsumerGroup(groupID),
		kgo.ConsumeTopics(topic),
		// start from earliest so we see what we just produced
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	if err != nil {
		log.Fatalf("failed to create franz-go client: %v", err)
	}
	defer cl.Close()
	log.Printf("created franz-go client (group=%s, topic=%s)", groupID, topic)

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	// ---------- ADMIN: ensure topic ----------
	adm := kadm.NewClient(cl)

	resps, err := adm.CreateTopics(ctx, 1, 1, nil, topic)
	if err != nil {
		log.Fatalf("CreateTopics RPC failed: %v", err)
	}
	res := resps[topic]
	if res.Err != nil && !errors.Is(res.Err, kerr.TopicAlreadyExists) {
		log.Fatalf("failed to create topic %q: %v", topic, res.Err)
	}
	if errors.Is(res.Err, kerr.TopicAlreadyExists) {
		log.Printf("topic %q already exists, OK", topic)
	} else {
		log.Printf("created topic %q", topic)
	}

	// ---------- PRODUCE ----------
	var recs []*kgo.Record
	for i := 0; i < 5; i++ {
		v := fmt.Sprintf("franz-msg-%d @ %s", i, time.Now().Format(time.RFC3339Nano))
		recs = append(recs, &kgo.Record{
			Topic: topic,
			Value: []byte(v),
		})
	}

	log.Printf("producing %d messages...", len(recs))
	produceResults := cl.ProduceSync(ctx, recs...)
	if err := produceResults.FirstErr(); err != nil {
		log.Fatalf("produce failed: %v", err)
	}
	for _, r := range produceResults {
		log.Printf("produced to %s[%d] offset=%d", r.Record.Topic, r.Record.Partition, r.Record.Offset)
	}

	// ---------- CONSUME ----------
	log.Printf("consuming messages from %q (group=%s)...", topic, groupID)

	expected := len(recs)
	got := 0
	deadline := time.Now().Add(20 * time.Second)

	for got < expected && time.Now().Before(deadline) {
		fetches := cl.PollFetches(ctx)
		if errs := fetches.Errors(); len(errs) > 0 {
			for _, e := range errs {
				log.Printf("consume error on %s[%d]: %v", e.Topic, e.Partition, e.Err)
			}
		}

		iter := fetches.RecordIter()
		for !iter.Done() {
			r := iter.Next()
			log.Printf("consumed from %s[%d] offset=%d: %s",
				r.Topic, r.Partition, r.Offset, string(r.Value))
			got++
			if got >= expected {
				break
			}
		}
	}

	if got < expected {
		log.Fatalf("consume timed out: expected %d, got %d", expected, got)
	}

	log.Printf("success: produced and consumed %d messages on %q via franz-go + proxy", got, topic)
}
