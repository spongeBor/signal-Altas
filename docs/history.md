# History model

Two resolutions share one store:

| Resolution | Written by | Kept for |
|---|---|---|
| raw (30 s samples) | every collection cycle | `retention.detail_days` (default 60) |
| hour (rollups) | same cycle, aggregated in place | `retention.summary_days` (default 365) |

Kinds:

- `traffic` — WAN rate, including the 1-second peak from SNMP when configured
- `device_traffic` — per-device bytes and rate from the router's counter chain
- `devices` — per-MAC snapshot (online, AP, band, RSSI, rate, Wi-Fi generation, MLO)
- `profiles` — router/AP state (CPU, memory, temperature, radio utilisation)
- `radios` — per-radio wireless environment (channel, width, utilisation, clients)
- `ping` — latency probes run on the router (gateway, AP, public resolvers)
- `events` — joins, leaves, AP switches, WAN address changes, collector state changes
- `changes` — renames and topology edits, kept separately from device identity

Every row carries a UTC timestamp; the UI converts to local time. Missing collection
windows stay empty instead of being interpolated.
