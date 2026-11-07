package collector

import (
	"time"

	"signalatlas/api/internal/domain"
	"signalatlas/api/internal/history"
)

// DemoProfiles are the collectors a demo deployment uses.
func DemoProfiles() []domain.Profile {
	return []domain.Profile{
		{ID: "demo-gateway", Name: "Gateway", Adapter: "openwrt", Host: "192.168.1.1"},
		{ID: "demo-ap", Name: "Living room AP", Adapter: "asus", Host: "192.168.1.2", Parent: "demo-gateway"},
		{ID: "demo-esxi", Name: "Hypervisor", Adapter: "esxi", Host: "192.168.1.3", Parent: "demo-gateway"},
		{ID: "demo-ont", Name: "ONT", Adapter: "zte", Host: "192.168.1.254", Parent: "demo-gateway"},
	}
}

// DemoObservation fabricates one collection cycle, so a demo install has charts
// on the history page from the first minute.
func DemoObservation(profiles []domain.Profile, at time.Time) history.Observation {
	obs := history.Observation{Time: at, Devices: []domain.Device{}, Radios: []domain.Radio{}, Ping: []domain.PingResult{}}
	byMAC := map[string]domain.Device{}
	for _, p := range profiles {
		data := demoCollectAt(p, at)
		if list, ok := data["devices"].([]domain.Device); ok {
			for _, d := range list {
				byMAC[d.MAC] = d
			}
		}
		if list, ok := data["radios"].([]domain.Radio); ok {
			obs.Radios = append(obs.Radios, list...)
		}
		if point, ok := data["traffic"].(*domain.TrafficPoint); ok {
			obs.Traffic = point
		}
		if ping, ok := data["ping"].([]domain.PingResult); ok {
			obs.Ping = ping
		}
		profile := history.Profile{ID: p.ID, Name: p.Name, Adapter: p.Adapter, Status: "ok", CollectedAt: at}
		if cpu, ok := data["cpu_percent"].(float64); ok {
			profile.CPU = &cpu
		}
		if temp, ok := data["temperature"].(float64); ok {
			profile.Temperature = &temp
		}
		obs.Profiles = append(obs.Profiles, profile)
		if stations, ok := data["stations"].([]domain.Station); ok {
			for _, s := range stations {
				d := byMAC[s.MAC]
				d.MAC, d.APID, d.APName, d.Interface = s.MAC, p.ID, p.Name, s.Interface
				d.SSID, d.Band, d.RSSI, d.TXMbps, d.RXMbps, d.WidthMHz = s.SSID, s.Band, s.RSSI, s.TXMbps, s.RXMbps, s.WidthMHz
				d.WiFi, d.Online, d.SeenVia, d.CollectedAt = s.WiFi, true, "AP实时关联", at
				byMAC[s.MAC] = d
			}
		}
	}
	for _, d := range byMAC {
		obs.Devices = append(obs.Devices, d)
	}
	return obs
}
