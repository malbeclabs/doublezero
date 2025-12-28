// testsender sends mock Solana shreds to a multicast address for testing.
package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"time"

	flag "github.com/spf13/pflag"
)

func main() {
	multicastIP := flag.String("ip", "239.0.0.1", "Multicast IP")
	port := flag.Int("port", 5000, "Multicast port")
	count := flag.Int("count", 10, "Number of shreds to send")
	interval := flag.Duration("interval", 100*time.Millisecond, "Interval between shreds")
	slot := flag.Uint64("slot", 300000000, "Starting slot number")
	flag.Parse()

	addr := &net.UDPAddr{
		IP:   net.ParseIP(*multicastIP),
		Port: *port,
	}

	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to dial: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	fmt.Printf("Sending %d mock shreds to %s:%d\n", *count, *multicastIP, *port)

	for i := 0; i < *count; i++ {
		// Alternate between data and code shreds
		isDataShred := i%2 == 0
		shred := createMockShred(*slot, uint32(i), isDataShred)

		n, err := conn.Write(shred)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to send shred %d: %v\n", i, err)
			continue
		}

		shredType := "CODE"
		if isDataShred {
			shredType = "DATA"
		}
		fmt.Printf("Sent shred #%d [%s] slot=%d index=%d (%d bytes)\n", i+1, shredType, *slot, i, n)

		// Increment slot every 10 shreds
		if (i+1)%10 == 0 {
			*slot++
		}

		time.Sleep(*interval)
	}

	fmt.Println("Done!")
}

// createMockShred creates a mock Solana shred packet.
func createMockShred(slot uint64, index uint32, isData bool) []byte {
	// Create a 200-byte mock shred (enough for headers + some payload)
	shred := make([]byte, 200)

	// Signature (64 bytes) - mock with index-based pattern
	for i := 0; i < 64; i++ {
		shred[i] = byte((index + uint32(i)) % 256)
	}

	// Variant byte at offset 0x40
	if isData {
		shred[0x40] = 0xA5 // Legacy Data shred
	} else {
		shred[0x40] = 0x5A // Legacy Code shred
	}

	// Slot (8 bytes, little-endian) at offset 0x41
	binary.LittleEndian.PutUint64(shred[0x41:], slot)

	// Shred index (4 bytes) at offset 0x49
	binary.LittleEndian.PutUint32(shred[0x49:], index)

	// Shred version (2 bytes) at offset 0x4D
	binary.LittleEndian.PutUint16(shred[0x4D:], 1)

	// FEC set index (4 bytes) at offset 0x4F
	fecIndex := (index / 32) * 32 // FEC sets of 32
	binary.LittleEndian.PutUint32(shred[0x4F:], fecIndex)

	if isData {
		// Data shred specific fields
		// Parent offset at 0x53
		binary.LittleEndian.PutUint16(shred[0x53:], 1)

		// Data flags at 0x55
		flags := uint8(index % 64) // batch tick
		if index%32 == 31 {
			flags |= 0x40 // batch complete
		}
		if index%64 == 63 {
			flags |= 0x80 // block complete
		}
		shred[0x55] = flags

		// Data size at 0x56
		binary.LittleEndian.PutUint16(shred[0x56:], 180)

		// Payload starts at 0x58
		payload := fmt.Sprintf("DATA-SLOT:%d-IDX:%d-TIME:%d", slot, index, time.Now().UnixNano())
		copy(shred[0x58:], []byte(payload))
	} else {
		// Code shred specific fields
		// Num data shreds at 0x53
		binary.LittleEndian.PutUint16(shred[0x53:], 32)

		// Num coding shreds at 0x55
		binary.LittleEndian.PutUint16(shred[0x55:], 32)

		// Position at 0x57
		position := index % 32
		binary.LittleEndian.PutUint16(shred[0x57:], uint16(position))

		// Payload starts at 0x59
		payload := fmt.Sprintf("CODE-SLOT:%d-IDX:%d-POS:%d", slot, index, position)
		copy(shred[0x59:], []byte(payload))
	}

	return shred
}
