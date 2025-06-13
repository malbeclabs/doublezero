package telemetry

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"runtime"
	"sync"
	"time"
)

// NewCollector creates a new telemetry collector
func NewCollector(config CollectorConfig, storage Storage) (*Collector, error) {
	if storage == nil {
		return nil, fmt.Errorf("storage cannot be nil")
	}

	c := &Collector{
		config:  config,
		peers:   make(map[string]*PeerDevice),
		samples: make(map[string]*LinkSamples),
		stopCh:  make(chan struct{}),
		storage: storage,
	}

	// Initialize packet pool for efficient memory usage
	c.packetPool = sync.Pool{
		New: func() any {
			return &PingRequest{}
		},
	}

	// Load any persisted samples
	savedSamples, err := storage.LoadSamples()
	if err != nil {
		log.Printf("Warning: failed to load persisted samples: %v", err)
	} else {
		c.samples = savedSamples
		log.Printf("Loaded %d link sample sets from storage", len(savedSamples))
	}

	return c, nil
}

// Start begins the telemetry collection process
func (c *Collector) Start() error {
	// Start UDP listener
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", c.config.ListenPort))
	if err != nil {
		return fmt.Errorf("failed to resolve UDP address: %w", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on UDP port %d: %w", c.config.ListenPort, err)
	}

	log.Printf("Telemetry collector listening on UDP port %d", c.config.ListenPort)

	// Start listener goroutine
	log.Printf("Starting listener goroutine")
	go c.listenLoop(conn)

	// Start measurement ticker
	log.Printf("Starting measurement loop goroutine")
	go c.measurementLoop()

	// Start storage ticker
	log.Printf("Starting storage loop goroutine")
	go c.storageLoop()

	// Send initial pings immediately
	log.Printf("Starting initial ping goroutine")
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("PANIC in initial ping goroutine: %v", r)
			}
		}()
		time.Sleep(100 * time.Millisecond) // Small delay to ensure everything is initialized
		log.Printf("Initial ping goroutine: sending pings after 100ms delay")
		c.sendPingsToAllPeers()
	}()

	return nil
}

// Stop gracefully shuts down the collector
func (c *Collector) Stop() error {
	close(c.stopCh)

	// Save all current samples
	c.mu.RLock()
	defer c.mu.RUnlock()

	for linkKey, samples := range c.samples {
		if err := c.storage.SaveSamples(linkKey, samples); err != nil {
			log.Printf("Error saving samples for %s: %v", linkKey, err)
		}
	}

	return nil
}

// UpdatePeers updates the list of peer devices to measure
func (c *Collector) UpdatePeers(peers []*PeerDevice) {
	log.Printf("UpdatePeers called with %d peers", len(peers))

	c.mu.Lock()
	defer c.mu.Unlock()

	// Clear and rebuild peers map
	c.peers = make(map[string]*PeerDevice)
	for i, peer := range peers {
		if peer == nil {
			log.Printf("WARNING: peer at index %d is nil, skipping", i)
			continue
		}
		c.peers[peer.DevicePubkey] = peer
		log.Printf("Added peer device: %s at %s (link: %s)", peer.DevicePubkey, peer.IP, peer.LinkPubkey)
	}

	log.Printf("UpdatePeers completed, map now has %d peers", len(c.peers))
}

// listenLoop handles incoming UDP ping responses
func (c *Collector) listenLoop(conn *net.UDPConn) {
	defer conn.Close()

	buffer := make([]byte, 2048)
	for {
		select {
		case <-c.stopCh:
			return
		default:
			// Set read deadline to allow periodic checks
			conn.SetReadDeadline(time.Now().Add(1 * time.Second))

			n, addr, err := conn.ReadFromUDP(buffer)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				log.Printf("Error reading UDP: %v", err)
				continue
			}

			log.Printf("Received UDP packet from %s: %d bytes", addr, n)

			if n < 12 { // Minimum packet size (4 bytes ID + 8 bytes timestamp)
				log.Printf("Received packet too small from %s: %d bytes", addr, n)
				continue
			}

			// Check if this is a ping request or response
			// Responses are exactly 20 bytes (4 + 8 + 8)
			// Requests are larger (2048 bytes with padding)
			if n == 20 {
				// This is a response
				go c.processResponse(buffer[:n], addr)
			} else if n >= 2048 {
				// This is a ping request, send response
				go c.respondToPing(buffer[:n], addr, conn)
			} else {
				log.Printf("Received unexpected packet size %d from %s", n, addr)
			}
		}
	}
}

// respondToPing sends a response to an incoming ping request
func (c *Collector) respondToPing(data []byte, addr *net.UDPAddr, conn *net.UDPConn) {
	// Parse request
	packetID := binary.BigEndian.Uint32(data[0:4])
	requestTimestamp := binary.BigEndian.Uint64(data[4:12])

	// Build response
	response := make([]byte, 20)
	binary.BigEndian.PutUint32(response[0:4], packetID)
	binary.BigEndian.PutUint64(response[4:12], requestTimestamp)
	binary.BigEndian.PutUint64(response[12:20], uint64(time.Now().UnixMicro()))

	// Send response
	_, err := conn.WriteToUDP(response, addr)
	if err != nil {
		log.Printf("Failed to send ping response to %s: %v", addr, err)
	}
}

// processResponse handles a received ping response
func (c *Collector) processResponse(data []byte, addr *net.UDPAddr) {
	var resp PingResponse

	// Parse response
	resp.PacketID = binary.BigEndian.Uint32(data[0:4])
	resp.RequestTimestampMicroseconds = binary.BigEndian.Uint64(data[4:12])
	// resp.ResponseTimestampMicroseconds = binary.BigEndian.Uint64(data[12:20])

	// Calculate RTT
	now := uint64(time.Now().UnixMicro())
	rtt := uint32(now - resp.RequestTimestampMicroseconds)

	// Find which peer this is from
	c.mu.RLock()
	var sourcePeer *PeerDevice
	for _, peer := range c.peers {
		if peer.IP == addr.IP.String() {
			sourcePeer = peer
			break
		}
	}
	c.mu.RUnlock()

	if sourcePeer == nil {
		log.Printf("Received response from unknown peer: %s", addr.IP)
		return
	}

	// Record the sample
	c.recordSample(sourcePeer, rtt, resp.PacketID)
}

// recordSample adds a new RTT sample for a link
func (c *Collector) recordSample(peer *PeerDevice, rttMicros uint32, packetID uint32) {
	linkKey := c.getLinkKey(peer)

	peerShort := peer.DevicePubkey
	if len(peerShort) > 8 {
		peerShort = peerShort[:8]
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	samples, exists := c.samples[linkKey]
	if !exists {
		samples = &LinkSamples{
			DeviceAPubkey:                c.config.LocalDevicePubkey,
			DeviceZPubkey:                peer.DevicePubkey,
			LinkPubkey:                   peer.LinkPubkey,
			LocationAPubkey:              c.config.LocalLocationPubkey,
			LocationZPubkey:              peer.LocationPubkey,
			Samples:                      make([]RTTSample, 0, c.config.MaxSamplesPerLink),
			StartTimestamp:               time.Now(),
			SamplingIntervalMicroseconds: uint64(c.config.SamplingIntervalSeconds * 1000000),
		}
		c.samples[linkKey] = samples
	}

	// Add the sample
	sample := RTTSample{
		RTTMicroseconds: rttMicros,
		Timestamp:       time.Now(),
		PacketID:        packetID,
	}

	samples.Samples = append(samples.Samples, sample)
	samples.NextSampleIndex++

	// Rotate if needed
	if len(samples.Samples) >= c.config.MaxSamplesPerLink {
		if err := c.storage.RotateSamples(linkKey); err != nil {
			log.Printf("Error rotating samples for %s: %v", linkKey, err)
		}
		// Keep only the last half of samples
		samples.Samples = samples.Samples[len(samples.Samples)/2:]
		samples.StartTimestamp = time.Now()
	}
}

// measurementLoop periodically sends ping requests to all peers
func (c *Collector) measurementLoop() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC in measurementLoop: %v", r)
		}
	}()

	log.Printf("Starting measurement loop with interval %d seconds", c.config.SamplingIntervalSeconds)

	// Validate config
	if c.config.SamplingIntervalSeconds <= 0 {
		log.Printf("ERROR: Invalid sampling interval: %d seconds", c.config.SamplingIntervalSeconds)
		return
	}

	ticker := time.NewTicker(time.Duration(c.config.SamplingIntervalSeconds) * time.Second)
	defer ticker.Stop()

	// Send first ping immediately
	log.Printf("Sending initial pings from measurementLoop")
	c.sendPingsToAllPeers()

	for {
		select {
		case <-c.stopCh:
			log.Printf("Stopping measurement loop")
			return
		case <-ticker.C:
			log.Printf("Ticker fired, sending pings")
			c.sendPingsToAllPeers()
		}
	}
}

// sendPingsToAllPeers sends UDP ping requests to all configured peers
func (c *Collector) sendPingsToAllPeers() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC in sendPingsToAllPeers: %v", r)
			// Also print stack trace
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			log.Printf("Stack trace:\n%s", buf[:n])
		}
	}()

	log.Printf("sendPingsToAllPeers called")

	c.mu.RLock()
	peerCount := len(c.peers)
	log.Printf("DEBUG: Creating peers slice with capacity %d", peerCount)
	peers := make([]*PeerDevice, 0, peerCount)
	idx := 0
	for key, peer := range c.peers {
		log.Printf("DEBUG: Copying peer %d from map key %s", idx, key)
		if peer == nil {
			log.Printf("ERROR: peer in map at key %s is nil", key)
			continue
		}
		peers = append(peers, peer)
		log.Printf("DEBUG: Successfully appended peer %d, slice len now %d", idx, len(peers))
		idx++
	}
	c.mu.RUnlock()

	log.Printf("Found %d peers in map", peerCount)

	if len(peers) > 0 {
		log.Printf("Sending pings to %d peers", len(peers))

		// Add more debugging before the loop
		log.Printf("DEBUG: About to start validation loop")

		// Simple validation without complex operations
		for i := range peers {
			log.Printf("DEBUG: Validation loop iteration %d", i)
			if i >= len(peers) {
				log.Printf("ERROR: index %d >= len(peers) %d", i, len(peers))
				break
			}
			if peers[i] == nil {
				log.Printf("ERROR: peer at index %d is nil, skipping", i)
				continue
			}
			log.Printf("DEBUG: About to access peer IP at index %d", i)
			ip := peers[i].IP
			log.Printf("Will ping peer %d at IP %s", i, ip)
		}
		log.Printf("DEBUG: Validation loop completed")
	} else {
		log.Printf("No peers to ping")
	}

	log.Printf("About to enter main ping loop with %d peers", len(peers))

	for i := range peers {
		log.Printf("In main ping loop, iteration %d", i)
		peer := peers[i]
		if peer == nil {
			log.Printf("ERROR: peer at index %d is nil", i)
			continue
		}
		log.Printf("About to call sendPing for peer %d/%d: IP=%s", i+1, len(peers), peer.IP)
		// Call sendPing directly (not in goroutine) for debugging
		c.sendPing(peer)
		log.Printf("Finished calling sendPing for peer %d/%d", i+1, len(peers))
	}

	log.Printf("sendPingsToAllPeers completed successfully")
}

// sendPing sends a single UDP ping request to a peer
func (c *Collector) sendPing(peer *PeerDevice) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC in sendPing: %v", r)
		}
	}()

	peerShort := peer.DevicePubkey
	if len(peerShort) > 8 {
		peerShort = peerShort[:8]
	}
	log.Printf("sendPing called for peer %s at %s", peerShort, peer.IP)

	// Check if packet pool is nil
	if c.packetPool.New == nil {
		log.Printf("ERROR: packet pool New function is nil")
		return
	}

	// Get packet from pool
	poolItem := c.packetPool.Get()
	if poolItem == nil {
		log.Printf("ERROR: packetPool.Get() returned nil")
		return
	}

	req, ok := poolItem.(*PingRequest)
	if !ok {
		log.Printf("ERROR: packetPool.Get() returned type %T, expected *PingRequest", poolItem)
		return
	}
	if req == nil {
		log.Printf("ERROR: PingRequest is nil after type assertion")
		return
	}
	defer c.packetPool.Put(req)

	// Generate packet ID
	c.idMu.Lock()
	c.nextID++
	packetID := c.nextID
	c.idMu.Unlock()

	// Prepare request
	req.PacketID = packetID
	req.TimestampMicroseconds = uint64(time.Now().UnixMicro())
	req.SourceDevice = c.config.LocalDevicePubkey

	// Serialize to bytes
	data := make([]byte, 2048)
	binary.BigEndian.PutUint32(data[0:4], req.PacketID)
	binary.BigEndian.PutUint64(data[4:12], req.TimestampMicroseconds)
	copy(data[12:], []byte(req.SourceDevice))

	// Send UDP packet
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", peer.IP, c.config.ListenPort))
	if err != nil {
		log.Printf("Failed to resolve peer address %s: %v", peer.IP, err)
		return
	}

	// Use a specific local address if needed (bind to any available)
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		log.Printf("Failed to dial peer %s: %v", peer.IP, err)
		return
	}
	defer conn.Close()

	// Log the local address being used
	localAddr := conn.LocalAddr()
	log.Printf("Sending ping from %s to %s", localAddr, addr)

	n, err := conn.Write(data)
	if err != nil {
		log.Printf("Failed to send ping to %s: %v", peer.IP, err)
	} else {
		log.Printf("Sent ping to %s (packet ID: %d, %d bytes)", peer.IP, packetID, n)
	}
}

// storageLoop periodically saves samples to disk
func (c *Collector) storageLoop() {
	// Use submission interval from config, defaulting to 60 seconds if not set
	interval := time.Duration(c.config.SubmissionIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 60 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.saveSamples()
		}
	}
}

// saveSamples persists current samples to storage
func (c *Collector) saveSamples() {
	c.mu.RLock()
	defer c.mu.RUnlock()

	saved := 0
	for linkKey, samples := range c.samples {
		if err := c.storage.SaveSamples(linkKey, samples); err != nil {
			log.Printf("Error saving samples for %s: %v", linkKey, err)
		} else {
			saved++
		}
	}

	if saved > 0 {
		log.Printf("Saved samples for %d links to storage", saved)
	}
}

// getLinkKey generates a unique key for a link
func (c *Collector) getLinkKey(peer *PeerDevice) string {
	// Ensure consistent ordering (smaller pubkey first)
	if c.config.LocalDevicePubkey < peer.DevicePubkey {
		return fmt.Sprintf("%s:%s:%s", c.config.LocalDevicePubkey, peer.DevicePubkey, peer.LinkPubkey)
	}
	return fmt.Sprintf("%s:%s:%s", peer.DevicePubkey, c.config.LocalDevicePubkey, peer.LinkPubkey)
}

// GetCurrentSamples returns a copy of current samples (for testing/monitoring)
func (c *Collector) GetCurrentSamples() map[string]*LinkSamples {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]*LinkSamples)
	for k, v := range c.samples {
		// Create a copy to avoid race conditions
		samplesCopy := &LinkSamples{
			DeviceAPubkey:                v.DeviceAPubkey,
			DeviceZPubkey:                v.DeviceZPubkey,
			LinkPubkey:                   v.LinkPubkey,
			LocationAPubkey:              v.LocationAPubkey,
			LocationZPubkey:              v.LocationZPubkey,
			Samples:                      make([]RTTSample, len(v.Samples)),
			StartTimestamp:               v.StartTimestamp,
			NextSampleIndex:              v.NextSampleIndex,
			SamplingIntervalMicroseconds: v.SamplingIntervalMicroseconds,
		}
		copy(samplesCopy.Samples, v.Samples)
		result[k] = samplesCopy
	}

	return result
}
