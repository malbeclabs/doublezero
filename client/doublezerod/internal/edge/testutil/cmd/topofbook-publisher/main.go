// topofbook-publisher is a standalone tool that sends synthetic Top-of-Book
// v0.1.0 frames to a multicast group. Use it to test the edge feed parser
// in a local devnet environment.
//
// Usage:
//
//	topofbook-publisher -group 239.0.0.1 -port 7000 -instruments 3 -rate 10 -duration 30s
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/edge/testutil"
)

func main() {
	group := flag.String("group", "239.0.0.1", "multicast group IP")
	port := flag.Int("port", 7000, "destination UDP port")
	numInstruments := flag.Int("instruments", 3, "number of instruments to simulate")
	rate := flag.Int("rate", 10, "quotes per second per instrument")
	duration := flag.Duration("duration", 30*time.Second, "how long to publish (0 = until interrupted)")
	flag.Parse()

	groupIP := net.ParseIP(*group)
	if groupIP == nil {
		log.Fatalf("invalid multicast group IP: %s", *group)
	}

	pub, err := testutil.NewPublisher(groupIP, *port)
	if err != nil {
		log.Fatalf("creating publisher: %v", err)
	}
	defer pub.Close()

	instruments := generateInstruments(*numInstruments)

	// Send instrument definitions first.
	for _, inst := range instruments {
		if err := pub.SendInstrumentDefinition(1, inst.id, inst.symbol, inst.leg1, inst.leg2, inst.priceExp, inst.qtyExp); err != nil {
			log.Fatalf("sending instrument def for %s: %v", inst.symbol, err)
		}
		fmt.Printf("sent instrument definition: %s (id=%d, priceExp=%d, qtyExp=%d)\n", inst.symbol, inst.id, inst.priceExp, inst.qtyExp)
	}

	// Publish quotes in a loop.
	interval := time.Duration(float64(time.Second) / float64(*rate))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var deadline <-chan time.Time
	if *duration > 0 {
		timer := time.NewTimer(*duration)
		defer timer.Stop()
		deadline = timer.C
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)

	fmt.Printf("publishing %d instruments at %d quotes/sec to %s:%d\n", len(instruments), *rate, *group, *port)

	quoteCount := 0
	tradeCount := 0
	hbCount := 0

	for {
		select {
		case <-sigCh:
			fmt.Printf("\ninterrupted. sent %d quotes, %d trades, %d heartbeats\n", quoteCount, tradeCount, hbCount)
			return
		case <-deadline:
			fmt.Printf("duration elapsed. sent %d quotes, %d trades, %d heartbeats\n", quoteCount, tradeCount, hbCount)
			return
		case <-ticker.C:
			for _, inst := range instruments {
				bidPrice := inst.basePrice + int64(time.Now().UnixNano()%100) - 50
				askPrice := bidPrice + inst.spread
				bidQty := uint64(100 + time.Now().UnixNano()%900)
				askQty := uint64(100 + time.Now().UnixNano()%900)

				if err := pub.SendQuote(1, inst.id, 1, bidPrice, bidQty, askPrice, askQty); err != nil {
					log.Printf("error sending quote for %s: %v", inst.symbol, err)
					continue
				}
				quoteCount++

				// Occasionally send a trade (1 in 10 quotes).
				if quoteCount%10 == 0 {
					side := uint8(1) // buy
					if time.Now().UnixNano()%2 == 0 {
						side = 2 // sell
					}
					if err := pub.SendTrade(1, inst.id, 1, bidPrice+inst.spread/2, bidQty/2, side); err != nil {
						log.Printf("error sending trade for %s: %v", inst.symbol, err)
					}
					tradeCount++
				}
			}

			// Send heartbeat every 100 ticks.
			if quoteCount%100 == 0 {
				pub.SendHeartbeat(1)
				hbCount++
			}
		}
	}
}

type instrument struct {
	id        uint32
	symbol    string
	leg1      string
	leg2      string
	priceExp  int8
	qtyExp    int8
	basePrice int64
	spread    int64
}

func generateInstruments(n int) []instrument {
	templates := []instrument{
		{1, "BTC-USDT", "BTC", "USDT", -2, -8, 6740000, 50},
		{2, "ETH-USDT", "ETH", "USDT", -2, -6, 350000, 25},
		{3, "SOL-USDT", "SOL", "USDT", -4, -6, 1850000, 1000},
		{4, "DOGE-USDT", "DOGE", "USDT", -6, -2, 150000, 100},
		{5, "AVAX-USDT", "AVAX", "USDT", -4, -4, 350000, 500},
		{6, "LINK-USDT", "LINK", "USDT", -4, -4, 150000, 200},
		{7, "DOT-USDT", "DOT", "USDT", -4, -4, 70000, 100},
		{8, "MATIC-USD", "MATIC", "USD", -6, -2, 800000, 500},
	}
	if n > len(templates) {
		n = len(templates)
	}
	return templates[:n]
}
