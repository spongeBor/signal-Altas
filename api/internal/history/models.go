// Package history defines the credential-free archive contract.
package history

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"regexp"
	"strings"
	"time"

	"signalatlas/api/internal/domain"
)

type Profile struct {
	OpticalSource      string    `json:"optical_source,omitempty"`
	RXPower            *float64  `json:"rx_power_dbm,omitempty"`
	TXPower            *float64  `json:"tx_power_dbm,omitempty"`
	OpticalTemperature *float64  `json:"optical_temperature_c,omitempty"`
	OpticalVoltage     *float64  `json:"optical_voltage_v,omitempty"`
	OpticalBias        *float64  `json:"optical_bias_ma,omitempty"`
	PONDown            *float64  `json:"pon_down_mbps,omitempty"`
	PONUp              *float64  `json:"pon_up_mbps,omitempty"`
	FECEnabled         *bool     `json:"fec_enabled,omitempty"`
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Adapter            string    `json:"adapter"`
	Status             string    `json:"status"`
	CollectedAt        time.Time `json:"collected_at"`
	CPU                *float64  `json:"cpu_percent,omitempty"`
	Temperature        *float64  `json:"chip_temperature,omitempty"`
	MemoryAvailable    *float64  `json:"memory_available_kib,omitempty"`
	WANUp              *bool     `json:"wan_up,omitempty"`
	PON                string    `json:"pon_state,omitempty"`
	LinkSpeeds         any       `json:"link_speeds,omitempty"`
}
type Observation struct {
	Time     time.Time            `json:"time"`
	Devices  []domain.Device      `json:"devices"`
	Profiles []Profile            `json:"profiles"`
	Radios   []domain.Radio       `json:"radios"`
	Traffic  *domain.TrafficPoint `json:"traffic,omitempty"`
	Ping     []domain.PingResult  `json:"ping,omitempty"`
}

// Observe copies only typed telemetry. Raw collector output, settings, account
// names, passwords, controller secrets and connection URLs never enter history.
func Observe(s domain.State) Observation {
	o := Observation{Time: s.LastUpdated, Devices: append([]domain.Device{}, s.Devices...), Radios: append([]domain.Radio{}, s.Radios...), Traffic: s.Traffic, Ping: append([]domain.PingResult{}, s.Ping...), Profiles: []Profile{}}
	for _, r := range s.Profiles {
		p := Profile{ID: r.ID, Name: r.Name, Adapter: r.Adapter, Status: r.Status, CollectedAt: r.CollectedAt}
		if r.Status == "ok" {
			if optics, ok := r.Data["optics"].(domain.OpticalModule); ok {
				p.OpticalSource = optics.MeasurementSource
				p.RXPower = number(optics.RXDBM)
				p.TXPower = number(optics.TXDBM)
				p.OpticalTemperature = number(optics.TemperatureC)
				p.OpticalVoltage = number(optics.VoltageV)
				p.OpticalBias = number(optics.BiasMA)
				p.PONDown = number(optics.DownMbps)
				p.PONUp = number(optics.UpMbps)
				if optics.FECEnabled != nil {
					enabled := *optics.FECEnabled
					p.FECEnabled = &enabled
				}
			}
			p.CPU = number(r.Data["cpu_percent"])
			p.Temperature = number(r.Data["chip_temperature"])
			if m, ok := r.Data["memory"].(map[string]uint64); ok {
				if n, ok := m["MemAvailable"]; ok {
					v := float64(n)
					p.MemoryAvailable = &v
				}
			}
			if v, ok := r.Data["wan_up"].(bool); ok {
				p.WANUp = &v
			}
			p.PON, _ = r.Data["pon_state"].(string)
			// Restrict the otherwise heterogeneous collector field to numeric arrays.
			switch v := r.Data["link_speeds"].(type) {
			case []int:
				p.LinkSpeeds = append([]int{}, v...)
			case []float64:
				p.LinkSpeeds = append([]float64{}, v...)
			}
		}
		o.Profiles = append(o.Profiles, p)
	}
	return o
}
func number(v any) *float64 {
	switch n := v.(type) {
	case *float64:
		if n != nil {
			x := *n
			return &x
		}
		return nil
	case float64:
		return &n
	case int:
		x := float64(n)
		return &x
	case uint64:
		x := float64(n)
		return &x
	}
	return nil
}

type Event struct {
	ID        string    `json:"id"`
	Time      time.Time `json:"time"`
	Kind      string    `json:"kind"`
	MAC       string    `json:"mac,omitempty"`
	Name      string    `json:"name,omitempty"`
	ProfileID string    `json:"profile_id,omitempty"`
	Text      string    `json:"text"`
}
type Seed struct {
	Traffic []domain.TrafficPoint `json:"traffic"`
	Events  []domain.Event        `json:"events"`
}
type Status struct {
	Enabled     bool       `json:"enabled"`
	DetailDays  int        `json:"detail_days"`
	SummaryDays int        `json:"summary_days"`
	Earliest    *time.Time `json:"earliest,omitempty"`
	Latest      *time.Time `json:"latest,omitempty"`
	LastSaved   *time.Time `json:"last_saved,omitempty"`
	Error       string     `json:"error,omitempty"`
}
type Cursor struct {
	Time time.Time `json:"t"`
	Key  string    `json:"k"`
}

func (c Cursor) Encode() string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

type Query struct {
	Kind                       string
	From, To                   time.Time
	MAC, ProfileID, Resolution string
	Target                     string
	Limit                      int
	Cursor                     *Cursor
}

func (q *Query) Validate(cursor string) error {
	switch q.Kind {
	case "traffic", "device_traffic", "devices", "profiles", "radios", "events", "changes", "ping":
	default:
		return errors.New("历史类型无效")
	}
	if q.From.IsZero() || q.To.IsZero() || !q.From.Before(q.To) || q.To.Sub(q.From) > 366*24*time.Hour {
		return errors.New("请选择有效日期范围，单次最多查询366天")
	}
	if q.Resolution == "" {
		q.Resolution = "raw"
	}
	if q.Resolution != "raw" && q.Resolution != "hour" {
		return errors.New("历史精度无效")
	}
	if q.Limit == 0 {
		q.Limit = 200
	}
	if q.Limit < 1 || q.Limit > 2000 {
		return errors.New("每页记录数应为1到2000")
	}
	if q.MAC != "" {
		m, e := net.ParseMAC(q.MAC)
		if e != nil || len(m) != 6 {
			return errors.New("设备MAC无效")
		}
		q.MAC = strings.ToLower(m.String())
	}
	if q.ProfileID != "" && !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`).MatchString(q.ProfileID) {
		return errors.New("采集设备ID无效")
	}
	if q.Target != "" && !regexp.MustCompile(`^[\p{L}0-9 .·:_-]{1,64}$`).MatchString(q.Target) {
		return errors.New("时延目标无效")
	}
	if cursor != "" {
		if len(cursor) > 512 {
			return errors.New("分页凭证无效")
		}
		b, e := base64.RawURLEncoding.DecodeString(cursor)
		var c Cursor
		if e != nil || json.Unmarshal(b, &c) != nil || c.Time.IsZero() || len(c.Key) > 128 {
			return errors.New("分页凭证无效")
		}
		q.Cursor = &c
	}
	if (q.Kind == "events" || q.Kind == "changes") && q.Resolution == "hour" {
		return errors.New("事件和修改记录不进行小时汇总")
	}
	return nil
}

type Page struct {
	Kind       string            `json:"kind"`
	Resolution string            `json:"resolution"`
	Items      []json.RawMessage `json:"items"`
	NextCursor string            `json:"next_cursor,omitempty"`
}
type Snapshot struct {
	Time     time.Time       `json:"time"`
	Devices  []domain.Device `json:"devices"`
	Profiles []Profile       `json:"profiles"`
	Radios   []domain.Radio  `json:"radios"`
	Topology json.RawMessage `json:"topology"`
}

// Uptime is one device's confirmed share of the hourly samples.
type Uptime struct {
	MAC       string `json:"mac"`
	Samples   int64  `json:"samples"`
	Confirmed int64  `json:"confirmed"`
}

// Stats describes the growth of the history database.
type Stats struct {
	DatabaseBytes    int64  `json:"database_bytes"`
	TrafficRows      int64  `json:"traffic_rows"`
	TrafficRows24h   int64  `json:"traffic_rows_24h"`
	DeviceHourlyRows int64  `json:"device_hourly_rows"`
	LargestTable     string `json:"largest_table"`
	LargestBytes     int64  `json:"largest_bytes"`
}

type Store interface {
	Append(context.Context, Observation) error
	Query(context.Context, Query) (Page, error)
	Status(context.Context) (Status, error)
	Entities(context.Context) ([]json.RawMessage, error)
	Snapshot(context.Context, time.Time) (Snapshot, error)
	Maintain(context.Context, time.Time) error
	Import(context.Context, Seed) error
	Uptime(ctx context.Context, from, to time.Time) ([]Uptime, error)
	Stats(ctx context.Context) (Stats, error)
	// ForgetEntity drops a ledger entry (a MAC that is no longer in use).
	ForgetEntity(ctx context.Context, mac string) error
}
