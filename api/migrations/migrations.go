package migrations

import _ "embed"

//go:embed 001_initial.sql
var Initial string

//go:embed 002_history.sql
var History string

//go:embed 003_device_traffic.sql
var DeviceTraffic string

//go:embed 004_entity_confirmed.sql
var EntityConfirmed string

//go:embed 005_traffic_peak.sql
var TrafficPeak string

//go:embed 006_ping.sql
var Ping string

//go:embed 007_device_columns.sql
var DeviceColumns string
