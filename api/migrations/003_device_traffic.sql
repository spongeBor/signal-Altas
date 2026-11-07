CREATE TABLE IF NOT EXISTS signal_atlas.history_device_traffic (
 at timestamptz NOT NULL,
 mac text NOT NULL,
 name text NOT NULL,
 ip inet,
 down_bytes bigint NOT NULL,
 up_bytes bigint NOT NULL,
 down_mbps double precision NOT NULL,
 up_mbps double precision NOT NULL,
 PRIMARY KEY(at,mac)
);
CREATE INDEX IF NOT EXISTS history_device_traffic_mac_at ON signal_atlas.history_device_traffic(mac,at DESC);
CREATE TABLE IF NOT EXISTS signal_atlas.history_device_traffic_hourly (
 hour timestamptz NOT NULL,
 mac text NOT NULL,
 name text NOT NULL,
 down_bytes bigint NOT NULL,
 up_bytes bigint NOT NULL,
 down_peak double precision NOT NULL,
 up_peak double precision NOT NULL,
 samples bigint NOT NULL,
 PRIMARY KEY(hour,mac)
);
CREATE INDEX IF NOT EXISTS history_device_traffic_hourly_mac_hour ON signal_atlas.history_device_traffic_hourly(mac,hour DESC);
INSERT INTO signal_atlas.schema_migrations(version) VALUES (3) ON CONFLICT DO NOTHING;
