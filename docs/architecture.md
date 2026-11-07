# Architecture

```
HTTP (Go) ── embedded Vue build (//go:embed)
   │
   ├── collector loop (every 30 s, read-only)
   │     ├── openwrt  – SSH: leases, neighbours, nft counters, WAN
   │     ├── asus     – SSH: wl/iw stations, radios, BSS
   │     ├── esxi     – HTTPS: VMs, vSwitches, NICs
   │     ├── zte      – Telnet: optical module, PON registers
   │     └── tcp      – port probe for switches
   │
   ├── history store
   │     ├── SQLite (default, single file)
   │     └── PostgreSQL (optional, DATABASE_URL)
   │
   └── documents (config, labels, topology) in the same store
```

- **One process**: the Go binary serves the API and the pre-built UI, so a deployment is
  a binary plus a data directory (or one container).
- **Read-only collection**: every adapter only reads; the app never writes device config.
- **Evidence-first**: each connection carries how it was learned (DHCP lease, neighbour
  table, AP association, API). The UI shows the evidence instead of guessing.
- **Retention**: raw detail (30 s samples) for `retention.detail_days`, hourly rollups for
  `retention.summary_days`. A maintenance pass prunes expired rows.
- **Setup**: `POST /api/v1/discover` scans a /24 for management ports, guesses the adapter
  from banners/certificates, and the UI offers one-click add; SSH host keys wait for an
  operator to approve the SHA256 fingerprint before the first collection.
