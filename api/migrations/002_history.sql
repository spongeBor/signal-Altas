CREATE TABLE IF NOT EXISTS signal_atlas.history_cycles (
 at timestamptz PRIMARY KEY, profiles jsonb NOT NULL, radios jsonb NOT NULL
);
CREATE TABLE IF NOT EXISTS signal_atlas.history_devices (
 at timestamptz NOT NULL, mac text NOT NULL, data jsonb NOT NULL, PRIMARY KEY(at,mac)
);
CREATE INDEX IF NOT EXISTS history_devices_mac_at ON signal_atlas.history_devices(mac,at DESC);
CREATE TABLE IF NOT EXISTS signal_atlas.history_entities (
 mac text PRIMARY KEY, name text NOT NULL, ip text NOT NULL, last_observed_at timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS signal_atlas.history_traffic (
 at timestamptz PRIMARY KEY, down_mbps double precision NOT NULL, up_mbps double precision NOT NULL
);
CREATE TABLE IF NOT EXISTS signal_atlas.history_events (
 id text PRIMARY KEY, at timestamptz NOT NULL, kind text NOT NULL, mac text NOT NULL DEFAULT '', name text NOT NULL DEFAULT '', profile_id text NOT NULL DEFAULT '', text text NOT NULL
);
CREATE INDEX IF NOT EXISTS history_events_at ON signal_atlas.history_events(at DESC,id DESC);
CREATE INDEX IF NOT EXISTS history_events_mac_at ON signal_atlas.history_events(mac,at DESC,id DESC);
CREATE TABLE IF NOT EXISTS signal_atlas.history_changes (
 id bigserial PRIMARY KEY, at timestamptz NOT NULL DEFAULT now(), kind text NOT NULL, subject text NOT NULL, before_value jsonb, after_value jsonb NOT NULL
);
CREATE INDEX IF NOT EXISTS history_changes_at ON signal_atlas.history_changes(at DESC,id DESC);
CREATE INDEX IF NOT EXISTS history_changes_subject ON signal_atlas.history_changes(subject,at DESC);
CREATE TABLE IF NOT EXISTS signal_atlas.history_traffic_hourly (
 hour timestamptz PRIMARY KEY, n bigint NOT NULL, down_sum double precision NOT NULL, up_sum double precision NOT NULL, down_peak double precision NOT NULL, up_peak double precision NOT NULL
);
CREATE TABLE IF NOT EXISTS signal_atlas.history_device_hourly (
 hour timestamptz NOT NULL, mac text NOT NULL, name text NOT NULL, n bigint NOT NULL, confirmed bigint NOT NULL,
 rssi_sum double precision NOT NULL, rssi_n bigint NOT NULL, rssi_min double precision, rssi_max double precision,
 tx_sum double precision NOT NULL, tx_n bigint NOT NULL, tx_peak double precision,
 aps text[] NOT NULL, bands text[] NOT NULL, PRIMARY KEY(hour,mac)
);
CREATE INDEX IF NOT EXISTS history_device_hourly_mac_hour ON signal_atlas.history_device_hourly(mac,hour DESC);
CREATE TABLE IF NOT EXISTS signal_atlas.history_profile_hourly (
 hour timestamptz NOT NULL, profile_id text NOT NULL, name text NOT NULL, n bigint NOT NULL, ok bigint NOT NULL,
 cpu_sum double precision NOT NULL,cpu_n bigint NOT NULL,cpu_peak double precision,
 temp_sum double precision NOT NULL,temp_n bigint NOT NULL,temp_peak double precision,PRIMARY KEY(hour,profile_id)
);
CREATE TABLE IF NOT EXISTS signal_atlas.history_radio_hourly (
 hour timestamptz NOT NULL, entity text NOT NULL, ap_id text NOT NULL, interface text NOT NULL, band text NOT NULL, ssid text NOT NULL,
 n bigint NOT NULL,util_sum double precision NOT NULL,util_n bigint NOT NULL,util_peak double precision,clients_sum bigint NOT NULL,clients_peak bigint NOT NULL,PRIMARY KEY(hour,entity)
);
INSERT INTO signal_atlas.schema_migrations(version) VALUES (2) ON CONFLICT DO NOTHING;
