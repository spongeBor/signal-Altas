ALTER TABLE signal_atlas.history_traffic ADD COLUMN IF NOT EXISTS down_peak_mbps double precision;
ALTER TABLE signal_atlas.history_traffic ADD COLUMN IF NOT EXISTS up_peak_mbps double precision;
INSERT INTO signal_atlas.schema_migrations(version) VALUES (5) ON CONFLICT DO NOTHING;
