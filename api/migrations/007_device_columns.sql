DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'signal_atlas' AND table_name = 'history_devices' AND column_name = 'data'
  ) THEN
    ALTER TABLE signal_atlas.history_devices RENAME TO history_devices_json;
    CREATE TABLE signal_atlas.history_devices (
      at timestamptz NOT NULL,
      mac text NOT NULL,
      name text NOT NULL DEFAULT '',
      online boolean NOT NULL DEFAULT false,
      seen_via text NOT NULL DEFAULT '',
      ip text NOT NULL DEFAULT '',
      ap_id text NOT NULL DEFAULT '',
      band text NOT NULL DEFAULT '',
      rssi double precision,
      tx_mbps double precision,
      width_mhz integer,
      PRIMARY KEY (at, mac)
    );
    INSERT INTO signal_atlas.history_devices(at,mac,name,online,seen_via,ip,ap_id,band,rssi,tx_mbps,width_mhz)
    SELECT at, mac,
      COALESCE(data->>'name',''),
      COALESCE((data->>'online')::boolean,false),
      COALESCE(data->>'seen_via',''),
      COALESCE(data->>'ip',''),
      COALESCE(data->>'ap_id',''),
      COALESCE(data->>'band',''),
      NULLIF(data->>'rssi','')::double precision,
      NULLIF(data->>'tx_mbps','')::double precision,
      NULLIF(data->>'width_mhz','')::integer
    FROM signal_atlas.history_devices_json;
    DROP TABLE signal_atlas.history_devices_json;
    CREATE INDEX IF NOT EXISTS history_devices_mac_at ON signal_atlas.history_devices(mac, at DESC);
  END IF;
END $$;
INSERT INTO signal_atlas.schema_migrations(version) VALUES (7) ON CONFLICT DO NOTHING;
