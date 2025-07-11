# TWAMP Light

A Go implementation of [TWAMP Light](https://datatracker.ietf.org/doc/html/rfc5357) (Two-Way Active Measurement Protocol Light) for UDP-based round-trip time measurement. This library provides a simplified approach to network latency testing without the complexity of the full TWAMP protocol.

## Overview

TWAMP Light eliminates the control protocol, authentication, and session management overhead of full TWAMP while maintaining the core concept: sending probe packets and measuring round-trip time from their reflection.

### Key Features

- Direct UDP communication without TCP session establishment
- 48-byte probe packets with NTP timestamps and sequence numbers
- Simple packet reflection without modification
- Thread-safe sender with shared UDP connection
- Single-threaded reflector for test environments
- Kernel-level receive timestamping using `SO_TIMESTAMPNS` (when supported)

## Protocol

This implementation follows the TWAMP Light concept from [RFC 5357](https://datatracker.ietf.org/doc/html/rfc5357) Appendix I, which describes a simplified architectural approach for two-way measurement. The packet format has been simplified from the complex RFC 5357 TWAMP-Test specification to a practical 48-byte fixed size for easy deployment and debugging.

### Packet Structure

```
Offset  Size  Field
0-3     4     Sequence Number (big-endian)
4-7     4     NTP Timestamp Seconds
8-11    4     NTP Timestamp Fraction
12-47   36    Padding (zeros)
```

The sequence number increments with each probe for packet ordering. NTP timestamps provide ~233 picosecond precision using the NTP epoch (January 1, 1900) as required by RFC 5357. Padding ensures consistent packet size for predictable network behavior.

## Implementation

- Single probe mode: one measurement per call
- RTT-only measurement: no loss/jitter statistics
- Dual timeout system: socket timeout + context cancellation
- Error handling: `ErrTimeout` and `ErrInvalidPacket` for specific failure modes
- Packet validation: reflector validates size and format, sender validates size only

### Timestamping Precision

This implementation uses the Linux kernel’s `SO_TIMESTAMPNS` socket option to obtain nanosecond-precision receive timestamps directly from the kernel when supported. This minimizes userspace jitter and syscall latency in round-trip time (RTT) measurements. If kernel timestamping is unavailable (e.g. non-Linux platforms), the implementation falls back to using `time.Now()` in userspace.

- **Kernel Timestamps**: Enabled via `recvmsg` and `SO_TIMESTAMPNS` on supported platforms
- **Fallback**: Transparent fallback to userspace wallclock timestamps
- **Clamping**: RTTs that appear negative due to clock inconsistencies are conservatively clamped to zero

A benchmark is included that compares the implementations:
```console
$ make bench-udp

BenchmarkUDPTimestampedReader_Kernel-8           3420692              3553 ns/op                 1.304 avgRTT_us              1341 worstRTT_us
BenchmarkUDPTimestampedReader_Wallclock-8        4451139              2700 ns/op                 2.560 avgRTT_us              3934 worstRTT_us
```

### TWAMP Light Conformance
- **Core Concept**: ✅ Simple packet reflection with timestamps (matches RFC 5357 Appendix I)
- **No Control Protocol**: ✅ Direct UDP communication without TCP session establishment
- **NTP Timestamps**: ✅ Uses NTP epoch and format per RFC 5905 (required by RFC 5357)
- **Main Deviation**: Packet format simplified from complex RFC 5357 TWAMP-Test to 48-byte fixed size

### NTP Timestamp

Timestamps follow RFC 5905 with 32-bit seconds and fractional parts:

```go
func ntpTimestamp(t time.Time) (uint32, uint32) {
    const ntpEpochOffset = 2208988800  // Seconds between 1900-01-01 and 1970-01-01
    secs := uint32(t.Unix()) + ntpEpochOffset
    nanos := uint64(t.Nanosecond())
    frac := uint32((nanos * (1 << 32)) / 1e9)
    return secs, frac
}
```

- Epoch: January 1, 1900 00:00:00 UTC
- Precision: ~233 picoseconds (2^-32 seconds)
- Monotonic: Always increases with time
- Leap second handling: Compatible with Go's time package

## Use Cases

- **Network diagnostics**: Direct RTT measurement without protocol overhead
- **Service health checks**: Simple latency monitoring without authentication complexity
- **Test environments**: Network simulation validation with predictable packet behavior
- **Embedded systems**: Minimal resource usage with single-probe patterns

## Testing

```bash
# Unit tests with race detection
make test

# Benchmarks
make bench

# Fuzz testing
make fuzz
```

Tests validate timestamp accuracy, packet reflection behavior, timeout handling, and concurrent usage patterns. Fuzz testing exercises packet parsing with malformed inputs.

## TWAMP Light vs Full TWAMP

TWAMP Light removes several components from the full protocol:

- **Control Protocol**: No TCP-based session establishment (port 862)
- **Authentication**: No HMAC-SHA1 authentication of test sessions
- **Encryption**: No AES encryption of test packets
- **Session Management**: No persistent test sessions or configuration negotiation
- **Statistics**: No aggregated loss, jitter, or delay variation metrics

This simplification makes TWAMP Light suitable for environments where security and session management overhead are not required.
