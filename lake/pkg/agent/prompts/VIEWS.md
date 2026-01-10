# View Definitions (Historical Reference)

This file contains the historical view definitions that were previously used in the database. These views no longer exist, but the definitions are preserved here for reference. The prompts have been updated to use table-specific logic instead of these views.

## DoubleZero Views

### dz_owners_internal
```sql
CREATE OR REPLACE VIEW dz_owners_internal AS
SELECT 'DZfHfcCXTLwgZeCRKQ1FL1UuwAwFAZM93g86NMYpfYan' AS owner_pk;
```

### dz_links_current_health
```sql
CREATE OR REPLACE VIEW dz_links_current_health AS
SELECT
  l.pk AS link_pk,
  l.code AS link_code,
  l.status,
  (l.status = 'activated') AS is_operational,
  (
    l.status IN ('soft-drained','hard-drained')
    OR l.isis_delay_override_ns = 1000000000
  ) AS is_soft_drained,
  (l.status = 'hard-drained') AS is_hard_drained,
  l.link_type,
  l.committed_rtt_ns,
  l.isis_delay_override_ns,
  l.bandwidth_bps,
  l.side_a_pk, l.side_z_pk,
  l.side_a_iface_name, l.side_z_iface_name,
  l.as_of_ts
FROM dz_links_current l;
```

### dz_link_telemetry
```sql
CREATE OR REPLACE VIEW dz_link_telemetry AS
SELECT
  t.time,
  t.link_pk,
  t.origin_device_pk,
  t.target_device_pk,
  t.rtt_us,
  t.loss,
  t.epoch,
  t.sample_index,
  t.ipdv_us AS jitter_ipdv_us,
  l.link_type
FROM dz_device_link_latency_samples_raw t
LEFT JOIN dz_links_current l ON l.pk = t.link_pk;
```

### dz_link_telemetry_with_committed
```sql
CREATE OR REPLACE VIEW dz_link_telemetry_with_committed AS
SELECT
  t.time,
  t.link_pk,
  t.origin_device_pk,
  t.target_device_pk,
  t.rtt_us,
  t.loss,
  t.jitter_ipdv_us,
  t.link_type,
  l.committed_rtt_ns,
  (l.committed_rtt_ns / 1000.0) AS committed_rtt_us,
  l.committed_jitter_ns,
  (l.committed_jitter_ns / 1000.0) AS committed_jitter_us,
  (t.rtt_us - (l.committed_rtt_ns / 1000.0)) AS rtt_minus_committed_us,
  (t.jitter_ipdv_us - (l.committed_jitter_ns / 1000.0)) AS jitter_minus_committed_us,
  (
    t.rtt_us IS NOT NULL
    AND l.committed_rtt_ns IS NOT NULL
    AND (t.rtt_us - (l.committed_rtt_ns / 1000.0)) >= 2000
    AND t.rtt_us >= (l.committed_rtt_ns / 1000.0) * 1.25
  ) AS is_significant_rtt_violation,
  (
    t.jitter_ipdv_us IS NOT NULL
    AND l.committed_jitter_ns IS NOT NULL
    AND (t.jitter_ipdv_us - (l.committed_jitter_ns / 1000.0)) >= 2000
    AND t.jitter_ipdv_us >= (l.committed_jitter_ns / 1000.0) * 1.25
  ) AS is_significant_jitter_violation
FROM dz_link_telemetry t
JOIN dz_links_current l ON l.pk = t.link_pk;
```

### dz_device_iface_health
```sql
CREATE OR REPLACE VIEW dz_device_iface_health AS
SELECT
  time,
  device_pk,
  host,
  intf,
  link_pk,
  link_side,
  coalesce(in_errors_delta, 0) AS in_errors,
  coalesce(out_errors_delta, 0) AS out_errors,
  coalesce(in_discards_delta, 0) AS in_discards,
  coalesce(out_discards_delta, 0) AS out_discards,
  coalesce(carrier_transitions_delta, 0) AS carrier_transitions
FROM dz_device_iface_usage_raw;
```

### dz_link_traffic
```sql
CREATE OR REPLACE VIEW dz_link_traffic AS
SELECT
  u.time,
  u.link_pk,
  l.code AS link_code,
  l.link_type,
  l.bandwidth_bps,
  SUM(COALESCE(u.in_octets_delta, 0) + COALESCE(u.out_octets_delta, 0)) AS total_octets_delta,
  SUM(
    CASE
      WHEN u.delta_duration > 0 THEN
        ((COALESCE(u.in_octets_delta, 0) + COALESCE(u.out_octets_delta, 0)) * 8.0) / u.delta_duration
      ELSE NULL
    END
  ) AS throughput_bps,
  SUM(
    CASE
      WHEN u.delta_duration > 0 THEN
        (COALESCE(u.in_octets_delta, 0) * 8.0) / u.delta_duration
      ELSE NULL
    END
  ) AS in_throughput_bps,
  SUM(
    CASE
      WHEN u.delta_duration > 0 THEN
        (COALESCE(u.out_octets_delta, 0) * 8.0) / u.delta_duration
      ELSE NULL
    END
  ) AS out_throughput_bps,
  SUM(COALESCE(u.in_pkts_delta, 0) + COALESCE(u.out_pkts_delta, 0)) AS total_pkts_delta
FROM dz_device_iface_usage_raw u
INNER JOIN dz_links_current l ON l.pk = u.link_pk
WHERE u.link_pk IS NOT NULL
  AND u.delta_duration > 0
  AND u.in_octets_delta >= 0
  AND u.out_octets_delta >= 0
GROUP BY u.time, u.link_pk, l.code, l.link_type, l.bandwidth_bps;
```

### dz_device_traffic
```sql
CREATE OR REPLACE VIEW dz_device_traffic AS
SELECT
  u.time,
  u.device_pk,
  d.code AS device_code,
  SUM(COALESCE(u.in_octets_delta, 0) + COALESCE(u.out_octets_delta, 0)) AS total_octets_delta,
  SUM(
    CASE
      WHEN u.delta_duration > 0 THEN
        ((COALESCE(u.in_octets_delta, 0) + COALESCE(u.out_octets_delta, 0)) * 8.0) / u.delta_duration
      ELSE NULL
    END
  ) AS throughput_bps,
  SUM(
    CASE
      WHEN u.delta_duration > 0 THEN
        (COALESCE(u.in_octets_delta, 0) * 8.0) / u.delta_duration
      ELSE NULL
    END
  ) AS in_throughput_bps,
  SUM(
    CASE
      WHEN u.delta_duration > 0 THEN
        (COALESCE(u.out_octets_delta, 0) * 8.0) / u.delta_duration
      ELSE NULL
    END
  ) AS out_throughput_bps,
  SUM(COALESCE(u.in_pkts_delta, 0) + COALESCE(u.out_pkts_delta, 0)) AS total_pkts_delta
FROM dz_device_iface_usage_raw u
INNER JOIN dz_devices_current d ON d.pk = u.device_pk
WHERE u.delta_duration > 0
  AND u.in_octets_delta >= 0
  AND u.out_octets_delta >= 0
GROUP BY u.time, u.device_pk, d.code;
```

### dz_user_device_traffic
```sql
CREATE OR REPLACE VIEW dz_user_device_traffic AS
WITH traffic AS (
  SELECT
    u.time,
    u.device_pk,
    d.code AS device_code,
    u.user_tunnel_id,
    u.in_octets_delta,
    u.out_octets_delta,
    u.in_pkts_delta,
    u.out_pkts_delta,
    u.delta_duration
  FROM dz_device_iface_usage_raw u
  INNER JOIN dz_devices_current d ON d.pk = u.device_pk
  WHERE u.user_tunnel_id IS NOT NULL
    AND u.delta_duration > 0
    AND u.in_octets_delta >= 0
    AND u.out_octets_delta >= 0
),
users AS (
  SELECT
    device_pk,
    CAST(tunnel_id AS BIGINT) AS user_tunnel_id,
    pk AS user_pk,
    valid_from,
    valid_to
  FROM dz_users_history
  WHERE op != 'D'
),
traffic_with_user AS (
  SELECT
    t.time,
    t.device_pk,
    t.device_code,
    t.user_tunnel_id,
    u.user_pk,
    t.in_octets_delta,
    t.out_octets_delta,
    t.in_pkts_delta,
    t.out_pkts_delta,
    t.delta_duration
  FROM traffic t
  ASOF JOIN users u
    ON t.device_pk = u.device_pk
   AND t.user_tunnel_id = u.user_tunnel_id
   AND u.valid_from <= t.time
  WHERE u.valid_to IS NULL OR t.time <= u.valid_to
)
SELECT
  time,
  device_pk,
  device_code,
  user_tunnel_id,
  user_pk,
  SUM(COALESCE(in_octets_delta, 0) + COALESCE(out_octets_delta, 0)) AS total_octets_delta,
  SUM(
    CASE
      WHEN delta_duration > 0 THEN
        ((COALESCE(in_octets_delta, 0) + COALESCE(out_octets_delta, 0)) * 8.0) / delta_duration
      ELSE NULL
    END
  ) AS throughput_bps,
  SUM(
    CASE
      WHEN delta_duration > 0 THEN
        (COALESCE(in_octets_delta, 0) * 8.0) / delta_duration
      ELSE NULL
    END
  ) AS in_throughput_bps,
  SUM(
    CASE
      WHEN delta_duration > 0 THEN
        (COALESCE(out_octets_delta, 0) * 8.0) / delta_duration
      ELSE NULL
    END
  ) AS out_throughput_bps,
  SUM(COALESCE(in_pkts_delta, 0) + COALESCE(out_pkts_delta, 0)) AS total_pkts_delta
FROM traffic_with_user
GROUP BY time, device_pk, device_code, user_tunnel_id, user_pk;
```

### dz_metro_to_metro_latency
```sql
CREATE OR REPLACE VIEW dz_metro_to_metro_latency AS
WITH s AS (
  SELECT
    time,
    origin_device_pk,
    target_device_pk,
    CASE WHEN rtt_us > 0 THEN rtt_us ELSE NULL END AS rtt_us,
    CASE WHEN (loss = true) OR (rtt_us = 0) THEN 1 ELSE 0 END AS is_loss,
    CASE WHEN ipdv_us > 0 THEN ipdv_us ELSE NULL END AS jitter_ipdv_us
  FROM dz_device_link_latency_samples_raw
),
dev AS (
  SELECT
    pk AS device_pk,
    metro_pk
  FROM dz_devices_current
)
SELECT
  s.time,
  d1.metro_pk AS origin_metro_pk,
  d2.metro_pk AS target_metro_pk,
  s.rtt_us AS dz_rtt_us,
  s.is_loss AS dz_is_loss,
  s.jitter_ipdv_us AS dz_jitter_us
FROM s
JOIN dev d1 ON d1.device_pk = s.origin_device_pk
JOIN dev d2 ON d2.device_pk = s.target_device_pk
WHERE
  d1.metro_pk IS NOT NULL
  AND d2.metro_pk IS NOT NULL;
```

### dz_public_internet_metro_to_metro_latency
```sql
CREATE OR REPLACE VIEW dz_public_internet_metro_to_metro_latency AS
SELECT
  time,
  origin_metro_pk,
  target_metro_pk,
  data_provider,
  CASE WHEN rtt_us > 0 THEN rtt_us ELSE NULL END AS internet_rtt_us,
  CASE WHEN ipdv_us > 0 THEN ipdv_us ELSE NULL END AS internet_jitter_us
FROM dz_internet_metro_latency_samples_raw;
```

### dz_vs_public_internet_metro_to_metro
```sql
CREATE OR REPLACE VIEW dz_vs_public_internet_metro_to_metro AS
SELECT
  dz.time,
  dz.origin_metro_pk,
  dz.target_metro_pk,
  inet.data_provider,
  dz.dz_rtt_us,
  dz.dz_is_loss,
  dz.dz_jitter_us,
  inet.internet_rtt_us,
  inet.internet_jitter_us,
  (inet.internet_rtt_us - dz.dz_rtt_us) AS rtt_improvement_us,
  CASE
    WHEN inet.internet_rtt_us IS NULL OR inet.internet_rtt_us = 0 THEN NULL
    ELSE (dz.dz_rtt_us / inet.internet_rtt_us)
  END AS rtt_ratio_dz_over_internet,
  (inet.internet_jitter_us - dz.dz_jitter_us) AS jitter_improvement_us,
  CASE
    WHEN inet.internet_jitter_us IS NULL OR inet.internet_jitter_us = 0 THEN NULL
    ELSE (dz.dz_jitter_us / inet.internet_jitter_us)
  END AS jitter_ratio_dz_over_internet
FROM dz_metro_to_metro_latency dz
JOIN dz_public_internet_metro_to_metro_latency inet
  ON inet.origin_metro_pk = dz.origin_metro_pk
  AND inet.target_metro_pk = dz.target_metro_pk
  AND inet.time >= dz.time - INTERVAL '1 minute'
  AND inet.time <= dz.time + INTERVAL '1 minute';
```

### dz_vs_public_internet_metro_to_metro_named
```sql
CREATE OR REPLACE VIEW dz_vs_public_internet_metro_to_metro_named AS
WITH base AS (
  SELECT * FROM dz_vs_public_internet_metro_to_metro
)
SELECT
  base.*,
  mo.code AS origin_metro_code,
  mo.name AS origin_metro_name,
  mt.code AS target_metro_code,
  mt.name AS target_metro_name
FROM base
LEFT JOIN dz_metros_current mo ON mo.pk = base.origin_metro_pk
LEFT JOIN dz_metros_current mt ON mt.pk = base.target_metro_pk;
```

### dz_link_health_core
```sql
CREATE OR REPLACE VIEW dz_link_health_core AS
WITH lt AS (
  SELECT * FROM dz_link_telemetry_with_committed
),
lh AS (
  SELECT
    l.pk AS link_pk,
    l.code AS link_code,
    l.side_a_pk,
    l.side_z_pk,
    l.side_a_iface_name,
    l.side_z_iface_name,
    l.status,
    (l.status IN ('soft-drained','hard-drained') OR l.isis_delay_override_ns = 1000000000) AS is_soft_drained,
    (l.status = 'hard-drained') AS is_hard_drained
  FROM dz_links_current l
)
SELECT
  lt.time,
  lh.link_pk,
  lh.link_code,
  lh.status,
  lh.is_soft_drained,
  lh.is_hard_drained,
  lt.link_type,
  lt.rtt_us,
  lt.loss,
  lt.jitter_ipdv_us,
  lt.committed_rtt_us,
  lt.rtt_minus_committed_us,
  lt.is_significant_rtt_violation,
  lt.committed_jitter_us,
  lt.jitter_minus_committed_us,
  lt.is_significant_jitter_violation,
  lh.side_a_pk AS side_a_device_pk,
  lh.side_a_iface_name AS side_a_intf,
  lh.side_z_pk AS side_z_device_pk,
  lh.side_z_iface_name AS side_z_intf
FROM lt
JOIN lh ON lh.link_pk = lt.link_pk;
```

### dz_link_health
```sql
CREATE OR REPLACE VIEW dz_link_health AS
WITH
core AS (SELECT * FROM dz_link_health_core),
ia AS (SELECT * FROM dz_device_iface_health)
SELECT
  core.*,
  a.in_errors AS side_a_in_errors,
  a.out_errors AS side_a_out_errors,
  a.in_discards AS side_a_in_discards,
  a.out_discards AS side_a_out_discards,
  a.carrier_transitions AS side_a_carrier_transitions,
  z.in_errors AS side_z_in_errors,
  z.out_errors AS side_z_out_errors,
  z.in_discards AS side_z_in_discards,
  z.out_discards AS side_z_out_discards,
  z.carrier_transitions AS side_z_carrier_transitions
FROM core
LEFT JOIN ia a
  ON a.time = core.time AND a.device_pk = core.side_a_device_pk AND a.intf = core.side_a_intf
LEFT JOIN ia z
  ON z.time = core.time AND z.device_pk = core.side_z_device_pk AND z.intf = core.side_z_intf;
```

### dz_device_events
```sql
CREATE OR REPLACE VIEW dz_device_events AS
SELECT
  valid_from AS time,
  'device' AS entity_kind,
  pk AS entity_pk,
  code AS entity_code,
  'status_change' AS event_type,
  status AS event_value,
  NULL::VARCHAR AS extra
FROM dz_devices_history
WHERE valid_from IS NOT NULL;
```

### dz_link_events
```sql
CREATE OR REPLACE VIEW dz_link_events AS
SELECT
  valid_from AS time,
  'link' AS entity_kind,
  pk AS entity_pk,
  code AS entity_code,
  CASE
    WHEN isis_delay_override_ns = 1000000000 THEN 'drain_soft_override'
    ELSE 'status_or_config_change'
  END AS event_type,
  status AS event_value,
  CASE
    WHEN isis_delay_override_ns IS NULL THEN NULL
    ELSE 'isis_delay_override_ns=' || isis_delay_override_ns::VARCHAR
  END AS extra
FROM dz_links_history
WHERE valid_from IS NOT NULL;
```

### dz_network_timeline
```sql
CREATE OR REPLACE VIEW dz_network_timeline AS
SELECT * FROM dz_device_events
UNION ALL
SELECT * FROM dz_link_events;
```

### dz_users_active_now
```sql
CREATE OR REPLACE VIEW dz_users_active_now AS
SELECT
  pk AS user_pubkey,
  owner_pk,
  status,
  kind,
  client_ip,
  dz_ip,
  device_pk,
  tunnel_id,
  valid_from,
  valid_to,
  op,
  run_id
FROM dz_users_history
WHERE op != 'D'
  AND valid_from <= CURRENT_TIMESTAMP
  AND (valid_to IS NULL OR valid_to > CURRENT_TIMESTAMP);
```

### dz_users_identities
```sql
CREATE OR REPLACE VIEW dz_users_identities AS
SELECT DISTINCT
  owner_pk,
  client_ip
FROM dz_users_history
WHERE op != 'D'
  AND owner_pk IS NOT NULL
  AND client_ip IS NOT NULL;
```

### dz_users_intervals
```sql
CREATE OR REPLACE VIEW dz_users_intervals AS
SELECT
  pk AS user_pubkey,
  owner_pk,
  status,
  kind,
  client_ip,
  dz_ip,
  device_pk,
  tunnel_id,
  valid_from,
  COALESCE(valid_to, TIMESTAMP '9999-12-31 00:00:00') AS valid_to,
  run_id
FROM dz_users_history
WHERE op != 'D';
```

## Solana Views

### solana_block_production_delta
```sql
CREATE OR REPLACE VIEW solana_block_production_delta AS
WITH x AS (
  SELECT
    epoch,
    time,
    leader_identity_pubkey,
    leader_slots_assigned_cum,
    blocks_produced_cum,
    lag(leader_slots_assigned_cum) OVER (PARTITION BY epoch, leader_identity_pubkey ORDER BY time) AS prev_assigned_cum,
    lag(blocks_produced_cum)       OVER (PARTITION BY epoch, leader_identity_pubkey ORDER BY time) AS prev_produced_cum
  FROM solana_block_production_raw
)
SELECT
  epoch,
  time,
  leader_identity_pubkey,
  leader_slots_assigned_cum,
  blocks_produced_cum,
  (leader_slots_assigned_cum - blocks_produced_cum) AS slots_skipped_cum,
  leader_slots_assigned_cum - coalesce(prev_assigned_cum, leader_slots_assigned_cum) AS leader_slots_assigned_delta,
  blocks_produced_cum       - coalesce(prev_produced_cum, blocks_produced_cum)       AS blocks_produced_delta,
  (leader_slots_assigned_cum - coalesce(prev_assigned_cum, leader_slots_assigned_cum))
  - (blocks_produced_cum     - coalesce(prev_produced_cum, blocks_produced_cum))     AS slots_skipped_delta,
  CASE
    WHEN leader_slots_assigned_cum = 0 THEN NULL
    ELSE blocks_produced_cum::DOUBLE / leader_slots_assigned_cum
  END AS produce_rate_cum
FROM x;
```

### solana_gossip_nodes_current_state
```sql
CREATE OR REPLACE VIEW solana_gossip_nodes_current_state AS
SELECT
  pubkey AS node_identity_pubkey,
  version,
  gossip_ip, gossip_port,
  tpuquic_ip, tpuquic_port,
  epoch,
  as_of_ts
FROM solana_gossip_nodes_current;
```

### solana_validator_health
```sql
CREATE OR REPLACE VIEW solana_validator_health AS
WITH v AS (
  SELECT
    time,
    vote_account_pubkey,
    node_identity_pubkey,
    (cluster_slot - last_vote_slot) AS vote_lag_slots,
    (cluster_slot - root_slot) AS root_lag_slots,
    (last_vote_slot - root_slot) AS vote_root_gap,
    credits_delta,
    is_delinquent,
    activated_stake_sol,
    commission
  FROM solana_vote_account_activity_raw
),
p_raw AS (
  SELECT
    epoch,
    time,
    leader_identity_pubkey,
    leader_slots_assigned_cum,
    blocks_produced_cum,
    lag(leader_slots_assigned_cum) OVER (PARTITION BY epoch, leader_identity_pubkey ORDER BY time) AS prev_assigned_cum,
    lag(blocks_produced_cum)       OVER (PARTITION BY epoch, leader_identity_pubkey ORDER BY time) AS prev_produced_cum
  FROM solana_block_production_raw
),
p AS (
  SELECT
    epoch,
    time,
    leader_identity_pubkey,
    leader_slots_assigned_cum - coalesce(prev_assigned_cum, leader_slots_assigned_cum) AS leader_slots_assigned_delta,
    blocks_produced_cum       - coalesce(prev_produced_cum, blocks_produced_cum)       AS blocks_produced_delta,
    (leader_slots_assigned_cum - coalesce(prev_assigned_cum, leader_slots_assigned_cum))
    - (blocks_produced_cum     - coalesce(prev_produced_cum, blocks_produced_cum))     AS slots_skipped_delta,
    CASE
      WHEN leader_slots_assigned_cum = 0 THEN NULL
      ELSE blocks_produced_cum::DOUBLE / leader_slots_assigned_cum
    END AS produce_rate_cum
  FROM p_raw
),
g AS (
  SELECT * FROM solana_gossip_nodes_current_state
)
SELECT
  v.time,
  v.vote_account_pubkey,
  v.node_identity_pubkey,
  v.vote_lag_slots,
  v.root_lag_slots,
  v.vote_root_gap,
  v.credits_delta,
  v.is_delinquent,
  v.activated_stake_sol,
  v.commission,
  p.epoch AS production_epoch,
  p.leader_slots_assigned_delta,
  p.blocks_produced_delta,
  p.slots_skipped_delta,
  p.produce_rate_cum,
  (g.node_identity_pubkey IS NOT NULL) AS gossip_present,
  g.version
FROM v
LEFT JOIN p
  ON p.leader_identity_pubkey = v.node_identity_pubkey
 AND p.time = v.time
LEFT JOIN g
  ON g.node_identity_pubkey = v.node_identity_pubkey;
```

### solana_leader_schedule_epoch
```sql
CREATE OR REPLACE VIEW solana_leader_schedule_epoch AS
SELECT
  epoch,
  node_pubkey AS leader_identity_pubkey,
  slot_count  AS leader_slots_assigned_epoch
FROM solana_leader_schedule_current;
```

### solana_leader_schedule_vs_production_current
```sql
CREATE OR REPLACE VIEW solana_leader_schedule_vs_production_current AS
WITH sched AS (
  SELECT * FROM solana_leader_schedule_epoch
),
prod AS (
  SELECT
    epoch,
    leader_identity_pubkey,
    leader_slots_assigned_cum,
    blocks_produced_cum,
    (leader_slots_assigned_cum - blocks_produced_cum) AS slots_skipped_cum,
    CASE
      WHEN leader_slots_assigned_cum = 0 THEN NULL
      ELSE blocks_produced_cum::DOUBLE / leader_slots_assigned_cum
    END AS produce_rate_cum
  FROM (
    SELECT
      epoch,
      leader_identity_pubkey,
      leader_slots_assigned_cum,
      blocks_produced_cum
    FROM solana_block_production_raw
    QUALIFY ROW_NUMBER() OVER (PARTITION BY epoch, leader_identity_pubkey ORDER BY time DESC) = 1
  )
)
SELECT
  s.epoch,
  s.leader_identity_pubkey,
  s.leader_slots_assigned_epoch,
  p.leader_slots_assigned_cum,
  p.blocks_produced_cum,
  p.slots_skipped_cum,
  p.produce_rate_cum
FROM sched s
LEFT JOIN prod p
  ON p.epoch = s.epoch
 AND p.leader_identity_pubkey = s.leader_identity_pubkey;
```

## DoubleZero-Solana Views

### solana_gossip_at_ip_now
```sql
CREATE OR REPLACE VIEW solana_gossip_at_ip_now AS
SELECT
  gn.pubkey AS node_pubkey,
  gn.gossip_ip,
  gn.version,
  gn.epoch,
  gn.as_of_ts AS valid_from,
  NULL AS valid_to
FROM solana_gossip_nodes_current gn
WHERE gn.gossip_ip IS NOT NULL;
```

### solana_validators_connected_now_connections
```sql
CREATE OR REPLACE VIEW solana_validators_connected_now_connections AS
SELECT DISTINCT
  va.vote_pubkey,
  va.node_pubkey,
  va.epoch,
  va.commission_percentage,
  va.activated_stake_lamports,
  CAST(va.activated_stake_lamports AS DOUBLE) / 1000000000.0 AS activated_stake_sol,
  u.pk AS dz_user_pk,
  u.owner_pk,
  u.client_ip,
  u.dz_ip,
  u.device_pk,
  NULL AS connected_from
FROM dz_users_current u
JOIN solana_gossip_nodes_current gn
  ON u.dz_ip = gn.gossip_ip
  AND gn.gossip_ip IS NOT NULL
JOIN solana_vote_accounts_current va
  ON gn.pubkey = va.node_pubkey
WHERE u.status = 'activated'
  AND va.activated_stake_lamports > 0;
```

### solana_validators_connected_now
```sql
CREATE OR REPLACE VIEW solana_validators_connected_now AS
SELECT
  vote_pubkey,
  max(node_pubkey) AS node_pubkey,
  max(epoch) AS epoch,
  max(commission_percentage) AS commission_percentage,
  max(CAST(activated_stake_lamports AS BIGINT)) AS activated_stake_lamports,
  max(activated_stake_sol) AS activated_stake_sol
FROM solana_validators_connected_now_connections
GROUP BY 1;
```

### solana_connected_stake_now_summary
```sql
CREATE OR REPLACE VIEW solana_connected_stake_now_summary AS
SELECT
  COUNT(*) AS connected_validator_count,
  SUM(CAST(activated_stake_lamports AS BIGINT)) AS total_stake_lamports,
  CAST(SUM(CAST(activated_stake_lamports AS BIGINT)) / 1000000000.0 AS DOUBLE) AS total_stake_sol
FROM solana_validators_connected_now;
```

### dz_device_connected_validators_now
```sql
CREATE OR REPLACE VIEW dz_device_connected_validators_now AS
WITH dv AS (
  SELECT
    device_pk,
    vote_pubkey,
    max(CAST(activated_stake_lamports AS BIGINT)) AS stake_lamports
  FROM solana_validators_connected_now_connections
  GROUP BY 1,2
)
SELECT
  device_pk,
  COUNT(*) AS connected_validators,
  SUM(stake_lamports) AS total_stake_lamports,
  CAST(SUM(stake_lamports) / 1000000000.0 AS DOUBLE) AS total_stake_sol
FROM dv
GROUP BY 1;
```

### solana_validator_dz_overlaps_strict
```sql
CREATE OR REPLACE VIEW solana_validator_dz_overlaps_strict AS
WITH u AS (
  SELECT
    pk AS dz_user_pk, owner_pk, client_ip, dz_ip, device_pk,
    valid_from AS u_from, valid_to AS u_to
  FROM dz_users_history
  WHERE op != 'D'
    AND status = 'activated'
    AND dz_ip IS NOT NULL
    AND owner_pk != 'DZfHfcCXTLwgZeCRKQ1FL1UuwAwFAZM93g86NMYpfYan'
),
gn AS (
  SELECT
    pubkey AS node_pubkey, gossip_ip,
    valid_from AS gn_from, valid_to AS gn_to
  FROM solana_gossip_nodes_history
  WHERE gossip_ip IS NOT NULL
),
va AS (
  SELECT
    vote_pubkey, node_pubkey, epoch,
    activated_stake_lamports,
    (activated_stake_lamports / 1000000000.0) AS activated_stake_sol,
    commission_percentage,
    valid_from AS va_from, valid_to AS va_to
  FROM solana_vote_accounts_history
  WHERE epoch_vote_account = true
    AND activated_stake_lamports > 0
)
SELECT
  va.vote_pubkey,
  va.node_pubkey,
  u.dz_user_pk,
  u.owner_pk,
  u.client_ip,
  u.device_pk,
  u.dz_ip,
  greatest(u.u_from, gn.gn_from, va.va_from) AS overlap_start,
  least(
    coalesce(u.u_to,  TIMESTAMP '9999-12-31'),
    coalesce(gn.gn_to, TIMESTAMP '9999-12-31'),
    coalesce(va.va_to, TIMESTAMP '9999-12-31')
  ) AS overlap_end,
  va.epoch,
  va.activated_stake_lamports,
  va.activated_stake_sol,
  va.commission_percentage
FROM u
JOIN gn ON u.dz_ip = gn.gossip_ip
JOIN va ON gn.node_pubkey = va.node_pubkey
WHERE
  greatest(u.u_from, gn.gn_from, va.va_from)
  <
  least(
    coalesce(u.u_to,  TIMESTAMP '9999-12-31'),
    coalesce(gn.gn_to, TIMESTAMP '9999-12-31'),
    coalesce(va.va_to, TIMESTAMP '9999-12-31')
  );
```

### solana_validator_dz_overlaps_windowed
```sql
CREATE OR REPLACE VIEW solana_validator_dz_overlaps_windowed AS
WITH u AS (
  SELECT
    pk AS dz_user_pk, owner_pk, client_ip, dz_ip, device_pk,
    valid_from AS u_from, valid_to AS u_to,
    coalesce(valid_to, CURRENT_TIMESTAMP) AS u_end_marker
  FROM dz_users_history
  WHERE op != 'D'
    AND status = 'activated'
    AND dz_ip IS NOT NULL
    AND owner_pk != 'DZfHfcCXTLwgZeCRKQ1FL1UuwAwFAZM93g86NMYpfYan'
),
gn AS (
  SELECT
    pubkey AS node_pubkey, gossip_ip,
    valid_from AS gn_from, valid_to AS gn_to
  FROM solana_gossip_nodes_history
  WHERE gossip_ip IS NOT NULL
),
va AS (
  SELECT
    vote_pubkey, node_pubkey, epoch,
    activated_stake_lamports,
    (activated_stake_lamports / 1000000000.0) AS activated_stake_sol,
    commission_percentage,
    valid_from AS va_from, valid_to AS va_to
  FROM solana_vote_accounts_history
  WHERE epoch_vote_account = true
    AND activated_stake_lamports > 0
)
SELECT
  va.vote_pubkey,
  va.node_pubkey,
  u.dz_user_pk,
  u.owner_pk,
  u.client_ip,
  u.device_pk,
  u.dz_ip,
  greatest(u.u_from, gn.gn_from, va.va_from) AS overlap_start,
  least(
    coalesce(u.u_to,  TIMESTAMP '9999-12-31'),
    coalesce(gn.gn_to, TIMESTAMP '9999-12-31'),
    coalesce(va.va_to, TIMESTAMP '9999-12-31')
  ) AS overlap_end,
  va.epoch,
  va.activated_stake_lamports,
  va.activated_stake_sol,
  va.commission_percentage
FROM u
JOIN gn
  ON u.dz_ip = gn.gossip_ip
  AND gn.gn_from <= u.u_end_marker + INTERVAL '1 hour'
  AND (gn.gn_to IS NULL OR gn.gn_to >= u.u_end_marker - INTERVAL '1 hour')
JOIN va ON gn.node_pubkey = va.node_pubkey
WHERE
  greatest(u.u_from, gn.gn_from, va.va_from)
  <
  least(
    coalesce(u.u_to,  TIMESTAMP '9999-12-31'),
    coalesce(gn.gn_to, TIMESTAMP '9999-12-31'),
    coalesce(va.va_to, TIMESTAMP '9999-12-31')
  );
```

### solana_validator_dz_connection_events
```sql
CREATE OR REPLACE VIEW solana_validator_dz_connection_events AS
SELECT
  vote_pubkey,
  node_pubkey,
  dz_user_pk,
  owner_pk,
  client_ip,
  dz_ip,
  device_pk,
  overlap_start AS event_time,
  'dz_connected' AS event_type,
  epoch,
  activated_stake_sol,
  commission_percentage,
  overlap_end AS event_end_marker
FROM solana_validator_dz_overlaps_windowed
UNION ALL
SELECT
  vote_pubkey,
  node_pubkey,
  dz_user_pk,
  owner_pk,
  client_ip,
  dz_ip,
  device_pk,
  overlap_end AS event_time,
  'dz_disconnected' AS event_type,
  epoch,
  activated_stake_sol,
  commission_percentage,
  overlap_start AS event_end_marker
FROM solana_validator_dz_overlaps_windowed
WHERE overlap_end < TIMESTAMP '9999-12-31';
```

### solana_validator_dz_first_connection_events
```sql
CREATE OR REPLACE VIEW solana_validator_dz_first_connection_events AS
WITH first_connections AS (
  SELECT
    vote_pubkey,
    MIN(event_time) AS first_connection_time
  FROM solana_validator_dz_connection_events
  WHERE event_type = 'dz_connected'
  GROUP BY vote_pubkey
)
SELECT
  e.vote_pubkey,
  e.node_pubkey,
  e.dz_user_pk,
  e.owner_pk,
  e.client_ip,
  e.dz_ip,
  e.device_pk,
  e.event_time,
  e.event_type,
  e.epoch,
  e.activated_stake_sol,
  e.commission_percentage,
  e.event_end_marker
FROM solana_validator_dz_connection_events e
JOIN first_connections fc
  ON e.vote_pubkey = fc.vote_pubkey
  AND e.event_time = fc.first_connection_time
WHERE e.event_type = 'dz_connected';
```

