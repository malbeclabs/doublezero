package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

type packet struct {
	addr *net.UDPAddr
	data []byte
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	bootstrapServers := os.Getenv("KAFKA_BOOTSTRAP_SERVERS")
	if bootstrapServers == "" {
		log.Fatalf("KAFKA_BOOTSTRAP_SERVERS environment variable is not set")
	}

	cfg := &kafka.ConfigMap{
		"bootstrap.servers": bootstrapServers,
		// TLS/SASL config here if needed
	}

	admin, err := kafka.NewAdminClient(cfg)
	if err != nil {
		log.Fatalf("failed to create admin client: %v", err)
	}
	defer admin.Close()

	if err := ensureTopic(admin, "test-topic", 6, 3); err != nil {
		log.Fatalf("failed to ensure topic: %v", err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "6343"
	}

	addr, err := net.ResolveUDPAddr("udp", ":"+port)
	if err != nil {
		log.Fatalf("resolve udp: %v", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatalf("listen udp: %v", err)
	}
	log.Printf("listening for UDP on %s", conn.LocalAddr())

	// Close socket on shutdown -> unblocks ReadFromUDP immediately
	go func() {
		<-ctx.Done()
		log.Printf("shutdown requested, closing UDP socket")
		_ = conn.Close()
	}()

	packets := make(chan packet, 1024)

	workerCount := runtime.NumCPU()
	for i := 0; i < workerCount; i++ {
		go ingestWorker(ctx, i, packets)
	}

	readLoop(ctx, conn, packets)

	// reader exited; no more packets will be produced
	close(packets)

	// give workers a moment to drain; or use a WaitGroup if you care
	log.Printf("server stopped")
}

func readLoop(ctx context.Context, conn *net.UDPConn, out chan<- packet) {
	buf := make([]byte, 65535)

	for {
		n, remote, err := conn.ReadFromUDP(buf)
		if err != nil {
			// if ctx is done, it's a shutdown; otherwise it's a real error
			if ctx.Err() != nil {
				log.Printf("read loop exiting: %v", err)
				return
			}
			log.Printf("read error: %v", err)
			continue
		}

		// copy data off the shared buffer
		data := make([]byte, n)
		copy(data, buf[:n])

		select {
		case out <- packet{addr: remote, data: data}:
		case <-ctx.Done():
			log.Printf("read loop exiting on context cancel")
			return
		}
	}
}

func ingestWorker(ctx context.Context, id int, in <-chan packet) {
	log.Printf("worker %d started", id)
	for {
		select {
		case <-ctx.Done():
			log.Printf("worker %d exiting on context cancel", id)
			return
		case p, ok := <-in:
			if !ok {
				log.Printf("worker %d channel closed, exiting", id)
				return
			}
			ingestPacket(id, p)
		}
	}
}

// replace this with "write to DB / queue / whatever"
func ingestPacket(workerID int, p packet) {
	log.Printf("worker %d: %d bytes from %s", workerID, len(p.data), p.addr.String())
	// parse p.data and do your real ingestion here
}

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
