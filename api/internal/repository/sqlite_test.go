package repository

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"signalatlas/api/internal/domain"
	"signalatlas/api/internal/history"
)

func f(v float64) *float64 { return &v }

func TestSQLiteStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := NewSQLite(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	at := time.Now().UTC().Truncate(time.Minute)
	if err = db.Append(ctx, history.Observation{
		Time:     at,
		Profiles: []history.Profile{{ID: "gateway", Name: "路由器", Status: "ok", CPU: f(12)}},
		Devices: []domain.Device{{
			MAC: "aa:bb:cc:dd:ee:01", Name: "手机", Online: true, IP: "10.0.0.5",
			APID: "ap", Band: "5 GHz", RSSI: f(-55), TXMbps: f(1200), WiFi: "Wi-Fi 7",
			Traffic: &domain.DeviceTraffic{Time: at, DownBytes: 1000, UpBytes: 100, DownMbps: 8, UpMbps: 1},
		}},
		Radios:  []domain.Radio{{APID: "ap", Interface: "wl1", Band: "5 GHz", SSID: "home", Utilization: f(20), Clients: 3}},
		Traffic: &domain.TrafficPoint{Time: at, DownMbps: 100, UpMbps: 5, DownPeak: f(110)},
		Ping:    []domain.PingResult{{Target: "公网", Host: "1.1.1.1", Sent: 2, Recv: 2, Avg: 9.5, Min: 9, Max: 10}},
	}); err != nil {
		t.Fatal(err)
	}

	from, to := at.Add(-time.Hour), at.Add(time.Hour)
	query := func(kind, resolution string) []map[string]any {
		page, err := db.Query(ctx, history.Query{Kind: kind, Resolution: resolution, From: from, To: to, Limit: 50})
		if err != nil {
			t.Fatalf("%s/%s: %v", kind, resolution, err)
		}
		out := []map[string]any{}
		for _, raw := range page.Items {
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatal(err)
			}
			out = append(out, m)
		}
		return out
	}

	if rows := query("devices", "raw"); len(rows) != 1 || rows[0]["name"] != "手机" || rows[0]["wifi"] != "Wi-Fi 7" {
		t.Fatalf("devices raw = %+v", rows)
	}
	if rows := query("devices", "hour"); len(rows) != 1 || rows[0]["confirmed_samples"] != float64(1) {
		t.Fatalf("devices hour = %+v", rows)
	}
	if rows := query("traffic", "raw"); len(rows) != 1 || rows[0]["down_peak"] != 110.0 {
		t.Fatalf("traffic raw = %+v", rows)
	}
	if rows := query("traffic", "hour"); len(rows) != 1 || rows[0]["down_mbps"] != 100.0 {
		t.Fatalf("traffic hour = %+v", rows)
	}
	if rows := query("device_traffic", "raw"); len(rows) != 1 || rows[0]["down_bytes"] != float64(1000) {
		t.Fatalf("device traffic = %+v", rows)
	}
	if rows := query("ping", "hour"); len(rows) != 1 || rows[0]["rtt_avg"] != 9.5 {
		t.Fatalf("ping hour = %+v", rows)
	}
	if rows := query("radios", "hour"); len(rows) != 1 || rows[0]["clients"] != 3.0 {
		t.Fatalf("radios hour = %+v", rows)
	}
	if rows := query("radios", "raw"); len(rows) != 1 || rows[0]["ssid"] != "home" {
		t.Fatalf("radios raw = %+v", rows)
	}
	if rows := query("profiles", "hour"); len(rows) != 1 {
		t.Fatalf("profiles hour = %+v", rows)
	}

	snap, err := db.Snapshot(ctx, at.Add(time.Second))
	if err != nil || len(snap.Devices) != 1 || snap.Devices[0].Name != "手机" {
		t.Fatalf("snapshot = %+v, %v", snap, err)
	}
	uptime, err := db.Uptime(ctx, from, to)
	if err != nil || len(uptime) != 1 || uptime[0].Confirmed != 1 {
		t.Fatalf("uptime = %+v, %v", uptime, err)
	}
	stats, err := db.Stats(ctx)
	if err != nil || stats.TrafficRows != 1 || stats.DatabaseBytes == 0 {
		t.Fatalf("stats = %+v, %v", stats, err)
	}
	if err = db.Write(ctx, "labels", map[string]domain.Label{"aa:bb:cc:dd:ee:01": {Name: "手机"}}); err != nil {
		t.Fatal(err)
	}
	var labels map[string]domain.Label
	if err = db.Read(ctx, "labels", &labels); err != nil || labels["aa:bb:cc:dd:ee:01"].Name != "手机" {
		t.Fatalf("labels = %+v, %v", labels, err)
	}
}
