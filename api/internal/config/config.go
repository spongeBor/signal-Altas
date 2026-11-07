// Package config loads the deployment settings that used to be scattered over
// environment variables: listening address, storage, retention, SNMP polling and
// the latency probes. Environment variables still win, so existing deployments
// keep working while config.yaml becomes the documented default.
package config

import (
	"errors"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

type Target struct {
	Name string `yaml:"name"`
	Host string `yaml:"host"`
}

type Retention struct {
	DetailDays  int `yaml:"detail_days"`
	SummaryDays int `yaml:"summary_days"`
}

type SNMP struct {
	Host          string `yaml:"host"`
	Interface     string `yaml:"interface"`
	IntervalMS    int    `yaml:"interval_ms"`
	WindowSeconds int    `yaml:"window_seconds"`
}

type File struct {
	Listen          string    `yaml:"listen"`
	LANCIDR         string    `yaml:"lan_cidr"`
	IntervalSeconds int       `yaml:"interval_seconds"`
	Retention       Retention `yaml:"retention"`
	SNMP            SNMP      `yaml:"snmp"`
	PingTargets     []Target  `yaml:"ping_targets"`
}

// Defaults mirror the values that were previously hard-coded.
func Defaults() File {
	return File{
		IntervalSeconds: 30,
		Retention:       Retention{DetailDays: 60, SummaryDays: 365},
		SNMP:            SNMP{Interface: "pppoe-wan", IntervalMS: 1000, WindowSeconds: 60},
		PingTargets: []Target{
			{Name: "网关", Host: "192.168.1.1"},
			{Name: "接入点", Host: "192.168.1.2"},
			{Name: "公网·阿里", Host: "223.5.5.5"},
			{Name: "公网·Cloudflare", Host: "1.1.1.1"},
		},
	}
}

// Load reads path (when given) on top of the defaults, then applies the
// environment overrides that are still supported.
func Load(path string) (File, error) {
	c := Defaults()
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return c, errors.New("无法读取配置文件 " + path)
		}
		if err = yaml.Unmarshal(b, &c); err != nil {
			return c, errors.New("配置文件格式无效（需要 YAML）")
		}
	}
	applyEnv(&c)
	return c, nil
}

func applyEnv(c *File) {
	if v := os.Getenv("SIGNAL_ATLAS_HISTORY_DETAIL_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.Retention.DetailDays = n
		}
	}
	if v := os.Getenv("SIGNAL_ATLAS_HISTORY_SUMMARY_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.Retention.SummaryDays = n
		}
	}
	if v := os.Getenv("SIGNAL_ATLAS_SNMP_HOST"); v != "" {
		c.SNMP.Host = v
	}
	if v := os.Getenv("SIGNAL_ATLAS_SNMP_INTERFACE"); v != "" {
		c.SNMP.Interface = v
	}
	if v := os.Getenv("SIGNAL_ATLAS_SNMP_INTERVAL_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.SNMP.IntervalMS = n
		}
	}
	if v := os.Getenv("SIGNAL_ATLAS_SNMP_WINDOW_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.SNMP.WindowSeconds = n
		}
	}
	if v := os.Getenv("SIGNAL_ATLAS_ADDR"); v != "" {
		c.Listen = v
	}
}

// Validate rejects values that would silently disable a feature.
func (c File) Validate() error {
	if c.Retention.DetailDays < 1 || c.Retention.DetailDays > 3650 {
		return errors.New("retention.detail_days 应在 1–3650 之间")
	}
	if c.Retention.SummaryDays < c.Retention.DetailDays {
		return errors.New("retention.summary_days 不能小于 detail_days")
	}
	if c.IntervalSeconds < 5 || c.IntervalSeconds > 3600 {
		return errors.New("interval_seconds 应在 5–3600 之间")
	}
	if c.SNMP.IntervalMS < 200 || c.SNMP.WindowSeconds < 5 {
		return errors.New("snmp.interval_ms 至少 200，window_seconds 至少 5")
	}
	for _, t := range c.PingTargets {
		if t.Host == "" || t.Name == "" {
			return errors.New("ping_targets 每项都需要 name 和 host")
		}
	}
	return nil
}
