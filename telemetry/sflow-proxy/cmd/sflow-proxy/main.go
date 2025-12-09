package main

import (
	"bytes"
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/netsampler/goflow2/v2/decoders/sflow"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kversion"
	"github.com/twmb/franz-go/pkg/sasl/aws"
)

type packet struct {
	addr *net.UDPAddr
	data []byte
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

	kafkaBrokers := os.Getenv("KAFKA_BROKERS")
	if kafkaBrokers == "" {
		kafkaBrokers = "127.0.0.1:19092"
	}

	seeds := kgo.SeedBrokers(strings.Split(kafkaBrokers, ",")...)
	opts := []kgo.Opt{
		seeds,
		kgo.AllowAutoTopicCreation(),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.ProducerBatchCompression(kgo.SnappyCompression()),
		kgo.ProducerLinger(1 * time.Second),
		kgo.MaxVersions(kversion.V2_8_0()),
	}

	if strings.ToLower(os.Getenv("KAFKA_AUTH_IAM_ENABLED")) == "true" {
		awsCfg, err := config.LoadDefaultConfig(ctx)
		if err != nil {
			log.Fatalf("failed to load aws config: %v", err)
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

	kafkaClient, err := kgo.NewClient(opts...)
	if err != nil {
		log.Fatalf("kafka client: %v", err)
	}
	defer kafkaClient.Close()

	workerCount := runtime.NumCPU()
	for i := 0; i < workerCount; i++ {
		go ingestWorker(ctx, i, packets, kafkaClient)
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

func ingestWorker(ctx context.Context, id int, in <-chan packet, kafkaClient *kgo.Client) {
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
			ingestPacket(ctx, id, p, kafkaClient)
		}
	}
}

func ingestPacket(ctx context.Context, workerID int, p packet, kafkaClient *kgo.Client) {
	// we need to check this a valid sflow packet before sending to kafka
	var msg sflow.Packet
	err := sflow.DecodeMessage(bytes.NewBuffer(p.data), &msg)
	if err != nil {
		log.Printf("worker %d: sflow decode error: %v", workerID, err)
		return
	}

	var hasFlowSample bool
	for _, sample := range msg.Samples {
		if _, ok := sample.(sflow.FlowSample); ok {
			hasFlowSample = true
			break
		}
	}

	if !hasFlowSample {
		return // skip packets without flow samples
	}

	rec := &kgo.Record{
		Topic: "flows_raw_devnet",
		Value: p.data,
	}

	kafkaClient.Produce(ctx, rec, func(r *kgo.Record, err error) {
		if err != nil {
			log.Printf("worker %d: kafka produce error: %v", workerID, err)
		}
	})
}
