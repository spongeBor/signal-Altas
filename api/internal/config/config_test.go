package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMergesFileOverDefaultsAndKeepsEnvOnTop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`retention:
  detail_days: 30
snmp:
  interface: wan0
  interval_ms: 500
ping_targets:
  - {name: 网关, host: 10.0.0.1}
`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SIGNAL_ATLAS_HISTORY_DETAIL_DAYS", "45")
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Retention.DetailDays != 45 {
		t.Fatalf("detail days = %d, env must win", c.Retention.DetailDays)
	}
	if c.Retention.SummaryDays != 365 {
		t.Fatalf("summary days = %d, default expected", c.Retention.SummaryDays)
	}
	if c.SNMP.Interface != "wan0" || c.SNMP.IntervalMS != 500 {
		t.Fatalf("snmp = %+v", c.SNMP)
	}
	if len(c.PingTargets) != 1 || c.PingTargets[0].Host != "10.0.0.1" {
		t.Fatalf("ping targets = %+v", c.PingTargets)
	}
}

func TestLoadWithoutFileUsesDefaults(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if c.Retention.DetailDays != 60 || len(c.PingTargets) != 4 {
		t.Fatalf("unexpected defaults: %+v", c)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMissingFileFails(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatal("missing file must fail when a path was given")
	}
}

func TestValidateRejectsBadRetention(t *testing.T) {
	c := Defaults()
	c.Retention.DetailDays = 400
	c.Retention.SummaryDays = 365
	if err := c.Validate(); err == nil {
		t.Fatal("detail days above summary must be rejected")
	}
	c = Defaults()
	c.PingTargets = []Target{{Name: "", Host: ""}}
	if err := c.Validate(); err == nil {
		t.Fatal("empty ping target must be rejected")
	}
}
