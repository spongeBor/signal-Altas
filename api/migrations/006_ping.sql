CREATE TABLE IF NOT EXISTS signal_atlas.history_ping (
 at timestamptz NOT NULL,
 target text NOT NULL,
 host text NOT NULL,
 sent integer NOT NULL,
 recv integer NOT NULL,
 rtt_avg double precision,
 rtt_min double precision,
 rtt_max double precision,
 PRIMARY KEY(at,target)
);
CREATE INDEX IF NOT EXISTS history_ping_at ON signal_atlas.history_ping(at DESC);
CREATE TABLE IF NOT EXISTS signal_atlas.history_ping_hourly (
 hour timestamptz NOT NULL,
 target text NOT NULL,
 host text NOT NULL,
 sent bigint NOT NULL,
 recv bigint NOT NULL,
 rtt_sum double precision NOT NULL,
 rtt_n bigint NOT NULL,
 rtt_peak double precision,
 PRIMARY KEY(hour,target)
);
INSERT INTO signal_atlas.schema_migrations(version) VALUES (6) ON CONFLICT DO NOTHING;
