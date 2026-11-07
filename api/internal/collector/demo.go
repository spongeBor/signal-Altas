package collector

import (
	"context"
	"math"
	"math/rand"
	"strings"
	"time"

	"signalatlas/api/internal/domain"
)

// Demo data lets people try the dashboard without any devices: a profile with
// adapter "demo" fabricates plausible devices, radios, traffic and latency.
// Everything is generated locally and never leaves the process.

type demoDevice struct {
	Name     string
	Category string
	IP       string
	MAC      string
	Band     string
	RSSI     float64
	TX       float64
	Width    int
	WiFi     string
}

var demoWireless = []demoDevice{
	{"iPhone 15 Pro", "手机", "192.168.1.31", "02:00:00:00:00:31", "5 GHz", -52, 1200, 160, "Wi-Fi 7"},
	{"MacBook Pro", "电脑", "192.168.1.32", "02:00:00:00:00:32", "5 GHz", -58, 1733, 160, "Wi-Fi 6"},
	{"iPad Air", "平板", "192.168.1.33", "02:00:00:00:00:33", "5 GHz", -64, 866, 80, "Wi-Fi 5"},
	{"Living room TV", "观影设备", "192.168.1.34", "02:00:00:00:00:34", "5 GHz", -61, 780, 80, "Wi-Fi 5"},
	{"Switch OLED", "游戏设备", "192.168.1.35", "02:00:00:00:00:35", "5 GHz", -66, 400, 80, "Wi-Fi 6"},
	{"Kitchen speaker", "智能音箱", "192.168.1.36", "02:00:00:00:00:36", "2.4 GHz", -71, 72, 20, "Wi-Fi 4"},
	{"Bedroom speaker", "智能音箱", "192.168.1.37", "02:00:00:00:00:37", "2.4 GHz", -76, 65, 20, "Wi-Fi 4"},
	{"Doorbell", "摄像设备", "192.168.1.38", "02:00:00:00:00:38", "2.4 GHz", -73, 54, 20, "Wi-Fi 4"},
	{"Robot vacuum", "家电", "192.168.1.39", "02:00:00:00:00:39", "2.4 GHz", -68, 72, 20, "Wi-Fi 4"},
	{"Air purifier", "家电", "192.168.1.40", "02:00:00:00:00:40", "2.4 GHz", -70, 72, 20, "Wi-Fi 4"},
	{"Thermostat", "家电", "192.168.1.41", "02:00:00:00:00:41", "2.4 GHz", -74, 65, 20, "Wi-Fi 4"},
	{"Smart bulb", "智能照明", "192.168.1.42", "02:00:00:00:00:42", "2.4 GHz", -66, 72, 20, "Wi-Fi 4"},
}

var demoWired = []demoDevice{
	{"NAS", "服务器", "192.168.1.20", "02:00:00:00:01:20", "", 0, 0, 0, ""},
	{"Desktop PC", "电脑", "192.168.1.21", "02:00:00:00:01:21", "", 0, 0, 0, ""},
	{"Study TV", "观影设备", "192.168.1.22", "02:00:00:00:01:22", "", 0, 0, 0, ""},
	{"Printer", "办公设备", "192.168.1.23", "02:00:00:00:01:23", "", 0, 0, 0, ""},
}

func float(v float64) *float64 { return &v }

// wave gives a slow, deterministic variation so screenshots look alive.
func wave(t time.Time, period float64, phase float64) float64 {
	return math.Sin(2*math.Pi*float64(t.Unix())/period + phase)
}

func demoStation(d demoDevice, t time.Time, rng *rand.Rand) domain.Station {
	jitter := wave(t, 900, float64(rng.Intn(100)))
	return domain.Station{
		MAC: d.MAC, Interface: map[bool]string{true: "wl1", false: "wl0"}[d.Band == "5 GHz"],
		SSID: "home", Band: d.Band, RSSI: float(d.RSSI + 3*jitter), TXMbps: float(d.TX * (1 + 0.15*jitter)),
		RXMbps: float(d.TX * (1 + 0.1*jitter)), WidthMHz: &d.Width, WiFi: d.WiFi,
	}
}

func demoDevices(t time.Time, rng *rand.Rand) []domain.Device {
	out := []domain.Device{}
	for _, d := range demoWired {
		out = append(out, domain.Device{MAC: d.MAC, IP: d.IP, Name: d.Name, Category: d.Category,
			Online: true, SeenVia: "DHCP租约", Band: "有线", CollectedAt: t})
	}
	for _, d := range demoWireless {
		s := demoStation(d, t, rng)
		out = append(out, domain.Device{MAC: s.MAC, IP: d.IP, Name: d.Name, Category: d.Category,
			Online: d.Name != "Bedroom speaker", SeenVia: "AP实时关联", Band: s.Band, Interface: s.Interface,
			SSID: s.SSID, RSSI: s.RSSI, TXMbps: s.TXMbps, RXMbps: s.RXMbps, WidthMHz: s.WidthMHz,
			WiFi: s.WiFi, CollectedAt: t})
	}
	return out
}

func demoCollect(p domain.Profile) (map[string]any, error) {
	return demoCollectAt(p, time.Now()), nil
}

func demoCollectAt(p domain.Profile, now time.Time) map[string]any {
	rng := rand.New(rand.NewSource(now.UnixNano() / 3))
	data := map[string]any{"devices": []domain.Device{}, "stations": []domain.Station{}, "radios": []domain.Radio{}}
	role := strings.TrimPrefix(p.ID, "demo-")
	switch {
	case strings.HasPrefix(role, "ap"):
		stations := []domain.Station{}
		for _, d := range demoWireless {
			stations = append(stations, demoStation(d, now, rng))
		}
		radios := []domain.Radio{
			{APID: p.ID, APName: p.Name, Interface: "wl1", SSID: "home", Band: "5 GHz", Channel: float(44),
				WidthMHz: func() *int { v := 160; return &v }(), Utilization: float(18 + 10*wave(now, 300, 0)), Clients: 5},
			{APID: p.ID, APName: p.Name, Interface: "wl0", SSID: "home", Band: "2.4 GHz", Channel: float(6),
				WidthMHz: func() *int { v := 20; return &v }(), Utilization: float(30 + 12*wave(now, 420, 1)), Clients: 7},
		}
		data["stations"] = stations
		data["radios"] = radios
	case strings.HasPrefix(role, "esxi"):
		data["model"] = "VMware ESXi"
		data["vms"] = []map[string]any{
			{"name": "home-router", "power_state": "poweredOn", "cpu": 2, "memory_mb": 2048},
			{"name": "nas", "power_state": "poweredOn", "cpu": 4, "memory_mb": 8192},
			{"name": "build-agent", "power_state": "poweredOff", "cpu": 2, "memory_mb": 4096},
		}
	case strings.HasPrefix(role, "ont"):
		data["model"] = "Generic GPON ONT"
		data["firmware"] = "V1.0.0"
		data["uptime"] = 86400 * 12
		data["chip_temperature"] = 44.2
		data["optics"] = map[string]any{
			"measurement_source": "zte_qoe_v2",
			"rx_dbm":             -19.4 + wave(now, 1200, 2),
			"tx_dbm":             4.8,
			"temperature_c":      39 + 2*wave(now, 900, 0.4),
			"voltage_v":          3.31,
			"bias_ma":            11.6,
			"manufacturer":       "OEM",
			"part_number":        "GPON-OLT-C+",
			"revision":           "A1",
			"date_code":          "2401",
			"module_type":        "XG_BOSA",
			"ddm_supported":      true,
			"calibration":        "internal",
			"monitoring_type":    "DDM",
		}
		data["pon_state"] = "O5"
	default: // gateway
		data["devices"] = demoDevices(now, rng)
		down := 28 + 24*wave(now, 600, 0) + 60*math.Max(0, wave(now, 90, 1.7))
		if down < 1 {
			down = 1
		}
		up := 3.5 + 2*wave(now, 480, 2.2)
		if up < 0.2 {
			up = 0.2
		}
		downPeak := down * (1.4 + 0.5*rng.Float64())
		upPeak := up * (1.3 + 0.4*rng.Float64())
		data["traffic"] = &domain.TrafficPoint{Time: now, DownMbps: down, UpMbps: up, DownPeak: &downPeak, UpPeak: &upPeak}
		data["cpu_percent"] = 8 + 4*wave(now, 300, 0.5)
		data["temperature"] = 46 + 2*wave(now, 900, 0.8)
		data["ping"] = []domain.PingResult{
			{Target: "网关", Host: "192.168.1.1", Sent: 2, Recv: 2, Avg: 0.6 + 0.2*wave(now, 300, 0), Min: 0.4, Max: 1.2},
			{Target: "接入点", Host: "192.168.1.2", Sent: 2, Recv: 2, Avg: 1.8 + 0.5*wave(now, 400, 1), Min: 1.2, Max: 3.6},
			{Target: "公网·阿里", Host: "223.5.5.5", Sent: 2, Recv: int(2 - minInt(1, int(2*(0.2+0.2*rng.Float64())))), Avg: 8.4 + 2*wave(now, 500, 2)},
			{Target: "公网·Cloudflare", Host: "1.1.1.1", Sent: 2, Recv: 2, Avg: 12.6 + 3*wave(now, 700, 3)},
		}
	}
	return data
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var _ = context.Background
