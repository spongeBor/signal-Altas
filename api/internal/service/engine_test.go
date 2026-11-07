package service

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"signalatlas/api/internal/domain"
	"signalatlas/api/internal/repository"
	"strings"
	"testing"
	"time"
)

func engineForTest(t *testing.T) *Engine {
	t.Helper()
	dir := t.TempDir()
	r, err := repository.NewFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	e, err := New(context.Background(), r, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func TestSecretsNeverPersistOrExport(t *testing.T) {
	e := engineForTest(t)
	p := domain.Profile{ID: "ap-main", Name: "AP", Host: "192.168.1.2", Username: "admin", Adapter: "asus"}
	secret := "test-credential-never-store"
	if err := e.SetConfig(context.Background(), []domain.ProfileInput{{Profile: p, Password: secret}}, 30); err != nil {
		t.Fatal(err)
	}
	bytes, _ := os.ReadFile(filepath.Join(e.dataDir, "config.json"))
	export, _ := json.Marshal(e.Export())
	view, _ := json.Marshal(e.View())
	for _, v := range [][]byte{bytes, export, view} {
		if strings.Contains(string(v), secret) {
			t.Fatal("credential escaped memory")
		}
	}
	restarted, err := New(context.Background(), e.repo, e.dataDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(restarted.secrets) != 0 {
		t.Fatal("credential survived restart")
	}
	if err = e.SetConfig(context.Background(), []domain.ProfileInput{{Profile: p}}, 30); err != nil || e.secrets[p.ID] != secret {
		t.Fatal("blank password failed to retain current credential")
	}
	if err = e.SetConfig(context.Background(), []domain.ProfileInput{{Profile: p, ClearPassword: true}}, 30); err != nil || e.secrets[p.ID] != "" {
		t.Fatal("clear password failed")
	}
}
func TestInvalidProfilesAndTopology(t *testing.T) {
	p := domain.Profile{ID: "a", Name: "AP", Host: "8.8.8.8", Adapter: "asus"}
	if ValidateProfiles([]domain.Profile{p}) == nil {
		t.Fatal("public collector host accepted")
	}
	p.Host = "192.168.1.2"
	if ValidateProfiles([]domain.Profile{p, p}) == nil {
		t.Fatal("duplicate profile accepted")
	}
	e := engineForTest(t)
	if e.SetTopology(context.Background(), json.RawMessage(`{"nodes":[{"id":"a"}],"links":[{"from":"a","to":"missing"}]}`)) == nil {
		t.Fatal("dangling topology edge accepted")
	}
}
func TestDeviceTrafficBaselineNeverInvitesSpikes(t *testing.T) {
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	prev := deviceCounter{at: at, ip: "192.168.1.20", down: 1_000_000, up: 500_000}
	// Ten seconds later 12.5 MB down and 2.5 MB up is 10 Mbps and 2 Mbps.
	next := deviceCounter{at: at.Add(10 * time.Second), ip: prev.ip, down: 13_500_000, up: 3_000_000}
	sample, ok := deviceTrafficDelta(prev, true, next)
	if !ok {
		t.Fatal("valid interval rejected")
	}
	if sample.DownBytes != 12_500_000 || sample.UpBytes != 2_500_000 {
		t.Fatalf("byte delta wrong: %+v", sample)
	}
	if math.Abs(sample.DownMbps-10) > 0.001 || math.Abs(sample.UpMbps-2) > 0.001 {
		t.Fatalf("rate wrong: %+v", sample)
	}
	for name, tc := range map[string]struct {
		prev  deviceCounter
		found bool
		next  deviceCounter
	}{
		"first sample has no baseline": {prev, false, next},
		"address changed":              {prev, true, deviceCounter{at: next.at, ip: "192.168.1.21", down: next.down, up: next.up}},
		"clock did not advance":        {prev, true, deviceCounter{at: prev.at, ip: prev.ip, down: next.down, up: next.up}},
		"download counter reset":       {prev, true, deviceCounter{at: next.at, ip: prev.ip, down: prev.down - 1, up: next.up}},
		"upload counter reset":         {prev, true, deviceCounter{at: next.at, ip: prev.ip, down: next.down, up: prev.up - 1}},
	} {
		if _, ok := deviceTrafficDelta(tc.prev, tc.found, tc.next); ok {
			t.Fatalf("%s produced a sample", name)
		}
	}
}
func TestCounterRateRefusesBackToBackCycles(t *testing.T) {
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	// A cycle that repeats one second later would divide a whole 30 second
	// window of bytes by one second and report roughly thirty times the real
	// rate. That is the shape of the fake peaks seen in the archive.
	for name, tc := range map[string]struct {
		prev  time.Time
		found bool
		next  time.Time
		want  bool
	}{
		"no baseline yet":       {at, false, at.Add(time.Second), false},
		"one second apart":      {at, true, at.Add(time.Second), true},
		"just under the floor":  {at, true, at.Add(minCounterWindow - time.Millisecond), true},
		"exactly the floor":     {at, true, at.Add(minCounterWindow), false},
		"normal thirty second":  {at, true, at.Add(30 * time.Second), false},
		"same instant":          {at, true, at, false},
		"clock moved backwards": {at, true, at.Add(-time.Minute), false},
	} {
		if got := tooSoon(tc.prev, tc.found, tc.next); got != tc.want {
			t.Fatalf("%s: tooSoon = %v, want %v", name, got, tc.want)
		}
	}
}
