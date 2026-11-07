package history

import (
	"encoding/json"
	"signalatlas/api/internal/domain"
	"strings"
	"testing"
	"time"
)

func TestObservationOnlyContainsTelemetry(t *testing.T) {
	r := domain.Result{Profile: domain.Profile{ID: "ont", Username: "secret-account"}, Status: "ok", Data: map[string]any{"cpu_percent": 12.5, "chip_temperature": 44.0, "tables": map[string]any{"password": "never-store"}, "settings": "controller-secret", "raw": "private-command-output", "link_speeds": []int{2500}}}
	b, err := json.Marshal(Observe(domain.State{Profiles: []domain.Result{r}}))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"secret-account", "never-store", "controller-secret", "private-command-output", "tables", "settings"} {
		if strings.Contains(string(b), s) {
			t.Fatalf("sensitive field included: %s", s)
		}
	}
	if !strings.Contains(string(b), "12.5") {
		t.Fatal("allowed telemetry missing")
	}
}
func TestQueryBoundsAndCursor(t *testing.T) {
	q := Query{Kind: "devices", From: time.Now().Add(-time.Hour), To: time.Now(), MAC: "AA:BB:CC:DD:EE:FF"}
	if err := q.Validate(""); err != nil {
		t.Fatal(err)
	}
	if q.MAC != "aa:bb:cc:dd:ee:ff" {
		t.Fatal("MAC not normalized")
	}
	c := Cursor{Time: q.From, Key: q.MAC}
	if err := q.Validate(c.Encode()); err != nil || q.Cursor.Key != q.MAC {
		t.Fatal("cursor roundtrip failed")
	}
	for _, bad := range []Query{{Kind: "DROP TABLE", From: q.From, To: q.To}, {Kind: "events", From: q.From, To: q.To, Resolution: "hour"}, {Kind: "traffic", From: q.To, To: q.From}, {Kind: "devices", From: q.From, To: q.To, Limit: 2001}, {Kind: "devices", From: q.From, To: q.To, MAC: "bad"}} {
		if bad.Validate("") == nil {
			t.Fatalf("invalid query accepted: %+v", bad)
		}
	}
	if q.Validate("not-a-cursor") == nil {
		t.Fatal("bad cursor accepted")
	}
}

func TestPointerValuedTemperatureIsCopiedAndMissingStaysUnknown(t *testing.T) {
	temperature := 44.0
	state := domain.State{Profiles: []domain.Result{{Profile: domain.Profile{ID: "ont"}, Status: "ok", Data: map[string]any{"chip_temperature": &temperature}}}}
	observed := Observe(state)
	temperature = 99
	if observed.Profiles[0].Temperature == nil || *observed.Profiles[0].Temperature != 44 {
		t.Fatal("pointer-valued temperature was lost or aliased")
	}
	state.Profiles[0].Data["chip_temperature"] = (*float64)(nil)
	if Observe(state).Profiles[0].Temperature != nil {
		t.Fatal("missing temperature became zero")
	}
}

func TestOpticalTelemetryHistoryIsCopiedAndCredentialFree(t *testing.T) {
	v := 39.5
	o := domain.OpticalModule{MeasurementSource: "zte_qoe_v2", TemperatureC: &v, Manufacturer: "not-archived", PartNumber: "metadata-not-archived"}
	state := domain.State{Profiles: []domain.Result{{Profile: domain.Profile{ID: "ont"}, Status: "ok", Data: map[string]any{"optics": o}}}}
	observed := Observe(state)
	v = 100
	if observed.Profiles[0].OpticalSource != "zte_qoe_v2" {
		t.Fatal("optical source not retained")
	}
	if observed.Profiles[0].OpticalTemperature == nil || *observed.Profiles[0].OpticalTemperature != 39.5 {
		t.Fatal("optics lost or aliased")
	}
	b, _ := json.Marshal(observed)
	if strings.Contains(string(b), "not-archived") {
		t.Fatal("non-telemetry optical metadata archived")
	}
	state.Profiles[0].Status = "error"
	if Observe(state).Profiles[0].OpticalTemperature != nil {
		t.Fatal("failed collector archived stale optics")
	}
}

func TestDeviceTrafficQueryAcceptsHourlyAndMAC(t *testing.T) {
	q := Query{Kind: "device_traffic", From: time.Now().Add(-24 * time.Hour), To: time.Now(), Resolution: "hour", MAC: "AA:BB:CC:DD:EE:FF", Limit: 2000}
	if err := q.Validate(""); err != nil {
		t.Fatal(err)
	}
	if q.MAC != "aa:bb:cc:dd:ee:ff" {
		t.Fatal("MAC not normalized")
	}
	if q.Resolution != "hour" {
		t.Fatal("hourly resolution lost")
	}
}

func TestDeviceTrafficIsArchivedPerInterval(t *testing.T) {
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	state := domain.State{Devices: []domain.Device{{MAC: "aa:bb:cc:dd:ee:ff", Name: "Phone", Traffic: &domain.DeviceTraffic{Time: at, DownBytes: 2048, UpBytes: 1024, DownMbps: 1.5, UpMbps: 0.5}}}}
	observed := Observe(state)
	if len(observed.Devices) != 1 || observed.Devices[0].Traffic == nil {
		t.Fatal("device traffic was dropped from history")
	}
	if observed.Devices[0].Traffic.DownBytes != 2048 || observed.Devices[0].Traffic.UpMbps != 0.5 {
		t.Fatal("device traffic value changed")
	}
	if state.Devices[0].Traffic.DownBytes != 2048 {
		t.Fatal("observation mutated the live device")
	}
}
