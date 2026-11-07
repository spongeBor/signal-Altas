package domain

import "time"

type Profile struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Host              string `json:"host"`
	Username          string `json:"username"`
	Adapter           string `json:"adapter"`
	Parent            string `json:"parent"`
	CertificateSHA256 string `json:"certificate_sha256,omitempty"`
}
type ProfileInput struct {
	Profile
	Password      string `json:"password,omitempty"`
	ClearPassword bool   `json:"clear_password,omitempty"`
}
type ProfileView struct {
	Profile
	HasPassword bool `json:"has_password"`
}
type Result struct {
	Profile
	Status      string         `json:"status"`
	Error       string         `json:"error,omitempty"`
	CollectedAt time.Time      `json:"collected_at"`
	Duration    float64        `json:"duration"`
	Data        map[string]any `json:"data"`
}
type Radio struct {
	APID        string   `json:"ap_id"`
	APName      string   `json:"ap_name"`
	Interface   string   `json:"interface"`
	SSID        string   `json:"ssid"`
	Band        string   `json:"band"`
	Channel     *float64 `json:"channel"`
	WidthMHz    *int     `json:"width_mhz"`
	Utilization *float64 `json:"utilization"`
	Clients     int      `json:"clients"`
	BSS         []BSS    `json:"bss,omitempty"`
}

// BSS is one virtual AP on a radio; a physical radio can broadcast several
// SSIDs (for example the same name on 2.4 and 5 GHz plus an IoT-only one).
type BSS struct {
	SSID    string `json:"ssid"`
	Clients int    `json:"clients"`
}
type Station struct {
	MAC       string   `json:"mac"`
	APID      string   `json:"ap_id"`
	Interface string   `json:"interface"`
	SSID      string   `json:"ssid"`
	Band      string   `json:"band"`
	RSSI      *float64 `json:"rssi"`
	TXMbps    *float64 `json:"tx_mbps"`
	RXMbps    *float64 `json:"rx_mbps"`
	WidthMHz  *int     `json:"width_mhz"`
	TXFailed  *float64 `json:"tx_failed,omitempty"`
	WiFi      string   `json:"wifi,omitempty"`
	MLO       bool     `json:"mlo,omitempty"`
	MLOLinks  int      `json:"mlo_links,omitempty"`
}
type Device struct {
	MAC           string    `json:"mac"`
	IP            string    `json:"ip,omitempty"`
	Addresses     []string  `json:"addresses,omitempty"`
	Name          string    `json:"name"`
	Category      string    `json:"category,omitempty"`
	PhysicalID    string    `json:"physical_device_id,omitempty"`
	Online        bool      `json:"online"`
	SeenVia       string    `json:"seen_via"`
	NeighborState string    `json:"neighbor_state,omitempty"`
	APID          string    `json:"ap_id,omitempty"`
	APName        string    `json:"ap_name,omitempty"`
	Interface     string    `json:"interface,omitempty"`
	SSID          string    `json:"ssid,omitempty"`
	Band          string    `json:"band,omitempty"`
	RSSI          *float64  `json:"rssi"`
	TXMbps        *float64  `json:"tx_mbps"`
	RXMbps        *float64  `json:"rx_mbps"`
	WidthMHz      *int      `json:"width_mhz"`
	WiFi          string    `json:"wifi,omitempty"`
	MLO           bool      `json:"mlo,omitempty"`
	MLOLinks      int       `json:"mlo_links,omitempty"`
	CollectedAt   time.Time `json:"collected_at"`
	// LastConfirmedAt is the last time the device was actually seen online,
	// which lets a battery device that wakes twice a day read as intermittent
	// rather than unknown.
	LastConfirmedAt *time.Time     `json:"last_confirmed_at,omitempty"`
	LabelSource     string         `json:"label_source,omitempty"`
	Traffic         *DeviceTraffic `json:"traffic,omitempty"`
}

// DeviceTraffic is a sampled counter delta from the router's existing
// nft-qos-monitor rules. Bytes describe only the immediately preceding
// successful sampling interval; totals and longer ranges are derived in the
// history store so a router counter reset never becomes a false spike.
type DeviceTraffic struct {
	Time      time.Time `json:"time"`
	DownBytes uint64    `json:"down_bytes"`
	UpBytes   uint64    `json:"up_bytes"`
	DownMbps  float64   `json:"down_mbps"`
	UpMbps    float64   `json:"up_mbps"`
}
type Label struct {
	Name       string `json:"name"`
	IP         string `json:"ip,omitempty"`
	Category   string `json:"category,omitempty"`
	PhysicalID string `json:"physical_device_id,omitempty"`
}
type TrafficPoint struct {
	Time     time.Time `json:"time"`
	DownMbps float64   `json:"down_mbps"`
	UpMbps   float64   `json:"up_mbps"`
	// Peak values come from the 2 s SNMP sampler and are much closer to a real
	// speed-test peak than the 30 s averages.
	DownPeak *float64 `json:"down_peak,omitempty"`
	UpPeak   *float64 `json:"up_peak,omitempty"`
}
type PingResult struct {
	Target string  `json:"target"`
	Host   string  `json:"host"`
	Sent   int     `json:"sent"`
	Recv   int     `json:"recv"`
	Avg    float64 `json:"rtt_avg"`
	Min    float64 `json:"rtt_min"`
	Max    float64 `json:"rtt_max"`
}

type Event struct {
	Time time.Time `json:"time"`
	MAC  string    `json:"mac"`
	Name string    `json:"name"`
	Text string    `json:"text"`
}
type State struct {
	LastUpdated time.Time      `json:"last_updated"`
	Collecting  bool           `json:"collecting"`
	Profiles    []Result       `json:"profiles"`
	Devices     []Device       `json:"devices"`
	Radios      []Radio        `json:"radios"`
	History     []TrafficPoint `json:"history"`
	Events      []Event        `json:"events"`
	Traffic     *TrafficPoint  `json:"traffic"`
	Ping        []PingResult   `json:"ping"`
}
type Config struct {
	Profiles []Profile `json:"profiles"`
	Interval int       `json:"interval"`
}
type Candidate struct {
	Host       string   `json:"host"`
	Services   []string `json:"services"`
	Configured bool     `json:"configured"`
	// Kind is the adapter suggested by the banner/title heuristics; empty when
	// the scan could not tell what the host is.
	Kind  string `json:"kind,omitempty"`
	Title string `json:"title,omitempty"`
}
