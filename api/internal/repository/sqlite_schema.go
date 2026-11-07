package repository

// sqliteSchema mirrors the PostgreSQL tables with SQLite types: JSON is stored
// as text and the array columns (aps, bands) as JSON arrays. Times are Unix
// milliseconds so range queries stay index friendly.
const sqliteSchema = `
CREATE TABLE IF NOT EXISTS documents(key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS cycles(at INTEGER PRIMARY KEY, profiles TEXT NOT NULL, radios TEXT NOT NULL DEFAULT '[]');
CREATE TABLE IF NOT EXISTS devices(
  at INTEGER NOT NULL, mac TEXT NOT NULL, name TEXT NOT NULL DEFAULT '', online INTEGER NOT NULL DEFAULT 0,
  seen_via TEXT NOT NULL DEFAULT '', ip TEXT NOT NULL DEFAULT '', ap_id TEXT NOT NULL DEFAULT '',
  band TEXT NOT NULL DEFAULT '', rssi REAL, tx_mbps REAL, width_mhz INTEGER,
  wifi TEXT NOT NULL DEFAULT '', mlo INTEGER NOT NULL DEFAULT 0, mlo_links INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(at, mac));
CREATE INDEX IF NOT EXISTS devices_mac_at ON devices(mac, at DESC);
CREATE TABLE IF NOT EXISTS device_hourly(
  hour INTEGER NOT NULL, mac TEXT NOT NULL, name TEXT NOT NULL, n INTEGER NOT NULL, confirmed INTEGER NOT NULL,
  rssi_sum REAL NOT NULL, rssi_n INTEGER NOT NULL, rssi_min REAL, rssi_max REAL,
  tx_sum REAL NOT NULL, tx_n INTEGER NOT NULL, tx_peak REAL, aps TEXT NOT NULL, bands TEXT NOT NULL,
  PRIMARY KEY(hour, mac));
CREATE INDEX IF NOT EXISTS device_hourly_mac ON device_hourly(mac, hour DESC);
CREATE TABLE IF NOT EXISTS traffic(at INTEGER PRIMARY KEY, down_mbps REAL NOT NULL, up_mbps REAL NOT NULL,
  down_peak REAL, up_peak REAL);
CREATE TABLE IF NOT EXISTS traffic_hourly(hour INTEGER PRIMARY KEY, n INTEGER NOT NULL, down_sum REAL NOT NULL,
  up_sum REAL NOT NULL, down_peak REAL NOT NULL, up_peak REAL NOT NULL);
CREATE TABLE IF NOT EXISTS device_traffic(at INTEGER NOT NULL, mac TEXT NOT NULL, name TEXT NOT NULL, ip TEXT,
  down_bytes INTEGER NOT NULL, up_bytes INTEGER NOT NULL, down_mbps REAL NOT NULL, up_mbps REAL NOT NULL,
  PRIMARY KEY(at, mac));
CREATE INDEX IF NOT EXISTS device_traffic_mac_at ON device_traffic(mac, at DESC);
CREATE TABLE IF NOT EXISTS device_traffic_hourly(hour INTEGER NOT NULL, mac TEXT NOT NULL, name TEXT NOT NULL,
  down_bytes INTEGER NOT NULL, up_bytes INTEGER NOT NULL, down_peak REAL NOT NULL, up_peak REAL NOT NULL,
  samples INTEGER NOT NULL, PRIMARY KEY(hour, mac));
CREATE INDEX IF NOT EXISTS device_traffic_hourly_mac ON device_traffic_hourly(mac, hour DESC);
CREATE TABLE IF NOT EXISTS profile_hourly(hour INTEGER NOT NULL, profile_id TEXT NOT NULL, name TEXT NOT NULL,
  n INTEGER NOT NULL, ok INTEGER NOT NULL, cpu_sum REAL NOT NULL, cpu_n INTEGER NOT NULL, cpu_peak REAL,
  temp_sum REAL NOT NULL, temp_n INTEGER NOT NULL, temp_peak REAL, PRIMARY KEY(hour, profile_id));
CREATE TABLE IF NOT EXISTS radio_hourly(hour INTEGER NOT NULL, entity TEXT NOT NULL, ap_id TEXT NOT NULL,
  interface TEXT NOT NULL, band TEXT NOT NULL, ssid TEXT NOT NULL, n INTEGER NOT NULL,
  util_sum REAL NOT NULL, util_n INTEGER NOT NULL, util_peak REAL, clients_sum INTEGER NOT NULL,
  clients_peak INTEGER NOT NULL, PRIMARY KEY(hour, entity));
CREATE TABLE IF NOT EXISTS events(id TEXT PRIMARY KEY, at INTEGER NOT NULL, kind TEXT NOT NULL,
  mac TEXT NOT NULL DEFAULT '', name TEXT NOT NULL DEFAULT '', profile_id TEXT NOT NULL DEFAULT '',
  text TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS events_at ON events(at DESC);
CREATE INDEX IF NOT EXISTS events_mac ON events(mac, at DESC);
CREATE TABLE IF NOT EXISTS entities(mac TEXT PRIMARY KEY, name TEXT NOT NULL, ip TEXT NOT NULL,
  last_observed_at INTEGER NOT NULL, last_confirmed_at INTEGER);
CREATE TABLE IF NOT EXISTS changes(id INTEGER PRIMARY KEY AUTOINCREMENT, at INTEGER NOT NULL, kind TEXT NOT NULL,
  subject TEXT NOT NULL, before_value TEXT, after_value TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS changes_at ON changes(at DESC);
CREATE TABLE IF NOT EXISTS ping(at INTEGER NOT NULL, target TEXT NOT NULL, host TEXT NOT NULL,
  sent INTEGER NOT NULL, recv INTEGER NOT NULL, rtt_avg REAL, rtt_min REAL, rtt_max REAL,
  PRIMARY KEY(at, target));
CREATE TABLE IF NOT EXISTS ping_hourly(hour INTEGER NOT NULL, target TEXT NOT NULL, host TEXT NOT NULL,
  sent INTEGER NOT NULL, recv INTEGER NOT NULL, rtt_sum REAL NOT NULL, rtt_n INTEGER NOT NULL, rtt_peak REAL,
  PRIMARY KEY(hour, target));
`
