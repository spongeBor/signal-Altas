-- Track the last time a device was actually confirmed online, so battery
-- devices that only associate for a few seconds can be shown as intermittent
-- instead of "never seen".
ALTER TABLE signal_atlas.history_entities ADD COLUMN IF NOT EXISTS last_confirmed_at timestamptz;
