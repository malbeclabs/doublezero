# DoubleZero Data Catalog Summary

## Overview
DoubleZero (DZ) is a network of dedicated high-performance links delivering low-latency connectivity globally. The data warehouse contains network telemetry, device status, and Solana blockchain integration data.

## Core Domains

### Network Infrastructure
- **Devices** (`dz_devices`): Hardware switches and routers with status, type, and metro location
- **Links** (`dz_links`): Connections between devices with type (WAN/DZX), status, and committed SLAs
- **Metros** (`dz_metros`): Geographic exchange locations (e.g., NYC, LON, TOK)
- **Contributors** (`dz_contributors`): Device and link operators

### Users & Connectivity
- **Users** (`dz_users`): Connected subscribers/sessions with owner_pubkey, client_ip, dz_ip, and tunnel_id
- Active users have `status = 'activated'` and `dz_ip IS NOT NULL`

### Network Telemetry (Time-Series)
- **Link Latency** (`fact_dz_device_link_latency`): RTT probes, packet loss, jitter per link
- **Interface Counters** (`fact_dz_device_interface_counters`): Traffic, errors, discards, carrier transitions
- **Internet Baseline** (`fact_dz_internet_metro_latency`): Public internet latency for comparison

### Solana Integration
- **Gossip Nodes** (`solana_gossip_nodes`): All Solana network participants
- **Vote Accounts** (`solana_vote_accounts`): Validators only, with stake and vote_pubkey
- **Vote Activity** (`fact_solana_vote_account_activity`): Voting performance metrics
- **Block Production** (`fact_solana_block_production`): Leader slots and blocks produced

### GeoIP
- **GeoIP Records** (`geoip_records`): IP geolocation and ASN data

## Key Patterns

### Current State vs Historical
- `{table}_current` views show current state
- `dim_{table}_history` tables contain all historical versions (SCD Type 2)

### Time Filtering
- Fact tables require time filter on `event_ts` column
- Use `WHERE event_ts >= now() - INTERVAL X HOUR/DAY`

### Common Joins
- DZ Users to Solana: `dz_users.dz_ip = solana_gossip_nodes.gossip_ip`
- Validators: Join `solana_gossip_nodes` to `solana_vote_accounts` via `node_pubkey`
- Link telemetry: Join via `link_pk`, `device_pk` foreign keys

### Important Constraints
- Validator identity: Use `vote_pubkey` (stable identifier)
- User identity: Use `(owner_pubkey, client_ip)` - pk changes on reconnect
- Link comparisons: Only compare WAN links to Internet (not DZX)
- Loss detection: `loss = true` OR `rtt_us = 0`
