CREATE SCHEMA IF NOT EXISTS signal_atlas;
CREATE TABLE IF NOT EXISTS signal_atlas.schema_migrations (
    version integer PRIMARY KEY,
    applied_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS signal_atlas.documents (
    key text PRIMARY KEY,
    value jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO signal_atlas.schema_migrations(version) VALUES (1) ON CONFLICT DO NOTHING;
