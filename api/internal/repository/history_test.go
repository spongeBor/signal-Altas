package repository

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"signalatlas/api/internal/domain"
	"signalatlas/api/internal/history"
)

func TestCollectionGapsNeverBecomeDisconnectEvents(t *testing.T) {
	now := time.Now()
	old := history.Observation{Time: now, Devices: []domain.Device{{MAC: "aa:bb:cc:dd:ee:ff", Online: true, APID: "a"}}}
	next := history.Observation{Time: now.Add(10 * time.Minute), Devices: []domain.Device{{MAC: "aa:bb:cc:dd:ee:ff", Online: false}}}
	if len(changesBetween(old, next)) != 0 {
		t.Fatal("collection gap became a device event")
	}
	next.Time = now.Add(time.Minute)
	events := changesBetween(old, next)
	if len(events) != 1 || strings.Contains(events[0].Text, "设备已离线") {
		t.Fatal("absence confused with offline evidence")
	}
}
func TestTopologyArchiveExcludesAttachedRawConfiguration(t *testing.T) {
	raw := []byte(`{"recorded_at":"2026-01-01","nodes":[{"id":"a","name":"AP","password":"not-for-history","x":1,"y":2}],"links":[],"raw_device_export":{"secret":"never-store"}}`)
	b, _ := json.Marshal(topologyForArchive(raw))
	if strings.Contains(string(b), "not-for-history") || strings.Contains(string(b), "never-store") {
		t.Fatal("attached configuration entered history")
	}
	if !strings.Contains(string(b), "recorded_at") {
		t.Fatal("provenance lost")
	}
}
func TestPostgresArchiveLifecycle(t *testing.T) {
	dsn := os.Getenv("SIGNAL_ATLAS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated PostgreSQL test database")
	}
	if !strings.Contains(dsn, "signal_atlas_test_") {
		t.Fatal("refusing a non-test database")
	}
	ctx := context.Background()
	p, err := NewPostgres(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	// This suite exclusively owns its disposable database.
	_, err = p.pool.Exec(ctx, `TRUNCATE signal_atlas.documents,signal_atlas.history_cycles,signal_atlas.history_devices,signal_atlas.history_entities,signal_atlas.history_traffic,signal_atlas.history_events,signal_atlas.history_changes,signal_atlas.history_traffic_hourly,signal_atlas.history_device_hourly,signal_atlas.history_profile_hourly,signal_atlas.history_radio_hourly,signal_atlas.history_device_traffic,signal_atlas.history_device_traffic_hourly RESTART IDENTITY`)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Second).Add(-10 * time.Minute)
	rss := -60.0
	cpu := 15.0
	util := 30.0
	mac := "aa:bb:cc:dd:ee:ff"
	obs := history.Observation{Time: at, Devices: []domain.Device{{MAC: mac, Name: "old name", IP: "192.168.1.123", Online: true, APID: "ap-a", APName: "A", RSSI: &rss, CollectedAt: at, Traffic: &domain.DeviceTraffic{Time: at, DownBytes: 1_500_000, UpBytes: 300_000, DownMbps: 4, UpMbps: 0.8}}}, Profiles: []history.Profile{{ID: "gateway", Name: "Router", Status: "ok", CPU: &cpu, CollectedAt: at}}, Radios: []domain.Radio{{APID: "ap-a", Interface: "wl1", Utilization: &util, Clients: 2}}, Traffic: &domain.TrafficPoint{Time: at, DownMbps: 100, UpMbps: 10}}
	if err = p.Append(ctx, obs); err != nil {
		t.Fatal(err)
	}
	if err = p.Append(ctx, obs); err != nil {
		t.Fatal(err)
	}
	for _, resolution := range []string{"raw", "hour"} {
		for _, kind := range []string{"traffic", "device_traffic", "devices", "profiles", "radios"} {
			q := history.Query{Kind: kind, From: at.Add(-time.Hour), To: at.Add(time.Hour), Resolution: resolution, Limit: 10}
			if err = q.Validate(""); err != nil {
				t.Fatal(err)
			}
			page, e := p.Query(ctx, q)
			if e != nil || len(page.Items) != 1 {
				t.Fatalf("%s/%s: %v %+v", kind, resolution, e, page)
			}
			if resolution == "hour" {
				var row map[string]any
				_ = json.Unmarshal(page.Items[0], &row)
				if row["samples"] != float64(1) {
					t.Fatal("retry double-counted hourly metrics")
				}
			}
		}
	}
	// Device traffic keeps the interval byte delta, the address, and honours the MAC filter.
	dq := history.Query{Kind: "device_traffic", From: at.Add(-time.Hour), To: at.Add(time.Hour), MAC: mac, Resolution: "raw", Limit: 10}
	if err = dq.Validate(""); err != nil {
		t.Fatal(err)
	}
	trafficPage, err := p.Query(ctx, dq)
	if err != nil || len(trafficPage.Items) != 1 {
		t.Fatalf("device traffic query: %v %+v", err, trafficPage)
	}
	var trafficRow map[string]any
	_ = json.Unmarshal(trafficPage.Items[0], &trafficRow)
	if trafficRow["down_bytes"] != float64(1_500_000) || trafficRow["up_bytes"] != float64(300_000) || trafficRow["ip"] != "192.168.1.123" {
		t.Fatalf("device traffic row wrong: %+v", trafficRow)
	}
	dq.MAC = "aa:bb:cc:dd:ee:00"
	_ = dq.Validate("")
	if page, e := p.Query(ctx, dq); e != nil || len(page.Items) != 0 {
		t.Fatal("device traffic ignored the MAC filter", e)
	}
	labels := map[string]domain.Label{mac: {Name: "old name"}}
	if err = p.Write(ctx, "labels", labels); err != nil {
		t.Fatal(err)
	}
	labels[mac] = domain.Label{Name: "new name"}
	if err = p.Write(ctx, "labels", labels); err != nil {
		t.Fatal(err)
	}
	if err = p.Write(ctx, "labels", labels); err != nil {
		t.Fatal(err)
	}
	// Enforce a change-log failure and verify the document transaction rolls back.
	if _, err = p.pool.Exec(ctx, `ALTER TABLE signal_atlas.history_changes ADD CONSTRAINT reject_test_name CHECK(after_value->>'name' IS DISTINCT FROM 'must-rollback')`); err != nil {
		t.Fatal(err)
	}
	labels[mac] = domain.Label{Name: "must-rollback"}
	if p.Write(ctx, "labels", labels) == nil {
		t.Fatal("audit failure did not abort document write")
	}
	var saved map[string]domain.Label
	if err = p.Read(ctx, "labels", &saved); err != nil || saved[mac].Name != "new name" {
		t.Fatal("document escaped audit transaction")
	}
	_, _ = p.pool.Exec(ctx, `ALTER TABLE signal_atlas.history_changes DROP CONSTRAINT reject_test_name`)
	obs.Time = at.Add(time.Minute)
	obs.Devices[0].APID = "ap-b"
	obs.Devices[0].APName = "B"
	obs.Traffic = nil
	if err = p.Append(ctx, obs); err != nil {
		t.Fatal(err)
	}
	q := history.Query{Kind: "devices", From: at.Add(-time.Minute), To: at.Add(time.Hour), MAC: mac, Resolution: "raw", Limit: 1}
	_ = q.Validate("")
	first, err := p.Query(ctx, q)
	if err != nil || first.NextCursor == "" {
		t.Fatalf("pagination missing: %v", err)
	}
	if err = q.Validate(first.NextCursor); err != nil {
		t.Fatal(err)
	}
	second, err := p.Query(ctx, q)
	if err != nil || len(second.Items) != 1 || string(second.Items[0]) == string(first.Items[0]) {
		t.Fatal("pagination repeated or lost point")
	}
	q.Kind = "events"
	q.Limit = 100
	q.Cursor = nil
	events, err := p.Query(ctx, q)
	if err != nil || len(events.Items) != 1 {
		t.Fatalf("roaming event: %v %+v", err, events)
	}
	// A new repository instance can read old records without process memory.
	restarted, err := NewPostgres(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := restarted.Snapshot(ctx, obs.Time)
	restarted.Close()
	if err != nil || len(snap.Devices) != 1 || snap.Devices[0].APID != "ap-b" {
		t.Fatal("restart replay failed", err)
	}
	if _, err = p.Snapshot(ctx, obs.Time.Add(time.Hour)); err != ErrNotFound {
		t.Fatal("a gap was silently filled with stale data")
	}
	if err = p.Import(ctx, history.Seed{Traffic: []domain.TrafficPoint{{Time: at, DownMbps: 100, UpMbps: 10}}}); err != nil {
		t.Fatal(err)
	}
	if err = p.Maintain(ctx, at.AddDate(0, 0, 91)); err != nil {
		t.Fatal(err)
	}
	q = history.Query{Kind: "devices", From: at.Add(-time.Hour), To: at.Add(time.Hour), Resolution: "raw", Limit: 10}
	page, err := p.Query(ctx, q)
	if err != nil || len(page.Items) != 0 {
		t.Fatal("raw retention failed", err)
	}
	q.Resolution = "hour"
	page, err = p.Query(ctx, q)
	if err != nil || len(page.Items) != 1 {
		t.Fatal("hourly history disappeared", err)
	}
	q.Kind = "device_traffic"
	q.Resolution = "raw"
	if page, err = p.Query(ctx, q); err != nil || len(page.Items) != 0 {
		t.Fatal("device traffic raw retention failed", err)
	}
	q.Resolution = "hour"
	if page, err = p.Query(ctx, q); err != nil || len(page.Items) != 1 {
		t.Fatal("device traffic hourly history disappeared", err)
	}
	q.Kind = "changes"
	q.Resolution = "raw"
	q.From = time.Now().Add(-time.Hour)
	q.To = time.Now().Add(time.Hour)
	page, err = p.Query(ctx, q)
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("audit retention or no-op change failed: %v %+v", err, page)
	}
	if err = p.Maintain(ctx, at.AddDate(0, 0, 366)); err != nil {
		t.Fatal(err)
	}
	q.Kind = "traffic"
	q.Resolution = "hour"
	q.From = at.Add(-time.Hour)
	q.To = at.Add(time.Hour)
	page, err = p.Query(ctx, q)
	if err != nil || len(page.Items) != 0 {
		t.Fatal("hourly retention failed", err)
	}
}
