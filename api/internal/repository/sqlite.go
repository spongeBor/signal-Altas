package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"signalatlas/api/internal/domain"
	"signalatlas/api/internal/history"

	_ "modernc.org/sqlite"
)

// SQLite is the file-backed store used when no DATABASE_URL is configured. It
// keeps the same documents and history tables as PostgreSQL so the UI behaves
// identically, only without a database server to install.
type SQLite struct {
	db                      *sql.DB
	path                    string
	detailDays, summaryDays int
}

var _ history.Store = (*SQLite)(nil)

func NewSQLite(path string) (*SQLite, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, errors.New("无法创建历史数据库目录")
	}
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, errors.New("无法打开本地历史数据库")
	}
	db.SetMaxOpenConns(1)
	if err = db.Ping(); err != nil {
		db.Close()
		return nil, errors.New("本地历史数据库不可用：数据目录需要容器用户（10001）可写，可在宿主机执行 mkdir -p runtime/data && chown -R 10001:10001 runtime/data")
	}
	if _, err = db.Exec(sqliteSchema); err != nil {
		db.Close()
		return nil, errors.New("本地历史数据库建表失败")
	}
	return &SQLite{db: db, path: path, detailDays: 60, summaryDays: 365}, nil
}

func (s *SQLite) ConfigureRetention(detail, summary int) error {
	if detail < 1 || summary < detail {
		return errors.New("历史保留期限无效")
	}
	s.detailDays, s.summaryDays = detail, summary
	return nil
}

func (s *SQLite) Kind() string { return "sqlite" }
func (s *SQLite) Close()       { _ = s.db.Close() }

func (s *SQLite) Read(ctx context.Context, key string, out any) error {
	if !validKey.MatchString(key) {
		return errors.New("invalid document key")
	}
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM documents WHERE key=?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return errors.New("数据库读取失败")
	}
	return json.Unmarshal([]byte(raw), out)
}

func (s *SQLite) Write(ctx context.Context, key string, value any) error {
	if !validKey.MatchString(key) {
		return errors.New("invalid document key")
	}
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var old string
	_ = s.db.QueryRowContext(ctx, `SELECT value FROM documents WHERE key=?`, key).Scan(&old)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("数据库事务创建失败")
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO documents(key,value) VALUES(?,?)
      ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, string(b)); err != nil {
		return errors.New("数据库写入失败")
	}
	if key == "labels" || key == "topology" {
		if err = sqliteRecordChange(ctx, tx, key, []byte(old), b); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// sqliteRecordChange mirrors the PostgreSQL audit trail for renames and topology
// edits so the history page keeps its source of truth.
func sqliteRecordChange(ctx context.Context, tx *sql.Tx, key string, old, next []byte) error {
	if key == "topology" {
		if len(old) > 0 && string(old) == string(next) {
			return nil
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO changes(at,kind,subject,before_value,after_value) VALUES(?,?,?,?,?)`,
			time.Now().UnixMilli(), "topology", "topology", nullIfEmpty(old), string(next))
		return err
	}
	var before, after map[string]domain.Label
	if len(old) > 0 {
		_ = json.Unmarshal(old, &before)
	}
	if json.Unmarshal(next, &after) != nil {
		return errors.New("标签记录无效")
	}
	for mac, l := range after {
		v, ok := before[mac]
		if ok && v.Name == l.Name {
			continue
		}
		b, _ := json.Marshal(map[string]string{"name": v.Name})
		a, _ := json.Marshal(map[string]string{"name": l.Name})
		if _, err := tx.ExecContext(ctx, `INSERT INTO changes(at,kind,subject,before_value,after_value) VALUES(?,?,?,?,?)`,
			time.Now().UnixMilli(), "rename", mac, string(b), string(a)); err != nil {
			return err
		}
	}
	return nil
}

func nullIfEmpty(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

// ms converts a timestamp for storage; the schema uses Unix milliseconds.
func ms(t time.Time) int64 { return t.UnixMilli() }

func fromMs(v int64) time.Time { return time.UnixMilli(v).UTC() }

func hourMs(t time.Time) int64 { return t.UTC().Truncate(time.Hour).UnixMilli() }

// Append writes one observation, mirroring the PostgreSQL batches.
func (s *SQLite) Append(ctx context.Context, o history.Observation) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("历史事务创建失败")
	}
	defer tx.Rollback()
	previous := history.Observation{}
	var prevAt int64
	var profiles string
	err = tx.QueryRowContext(ctx, `SELECT at,profiles FROM cycles ORDER BY at DESC LIMIT 1`).Scan(&prevAt, &profiles)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return errors.New("历史基线读取失败")
	}
	if err == nil {
		previous.Time = fromMs(prevAt)
		_ = json.Unmarshal([]byte(profiles), &previous.Profiles)
		if !previous.Time.IsZero() && o.Time.Sub(previous.Time) <= 5*time.Minute {
			rows, qerr := tx.QueryContext(ctx, `SELECT mac,name,online,seen_via,ip,ap_id,band,rssi,tx_mbps,width_mhz,wifi,mlo,mlo_links
              FROM devices WHERE at=?`, prevAt)
			if qerr != nil {
				return qerr
			}
			for rows.Next() {
				var d domain.Device
				var online, mlo int
				if qerr = rows.Scan(&d.MAC, &d.Name, &online, &d.SeenVia, &d.IP, &d.APID, &d.Band, &d.RSSI, &d.TXMbps, &d.WidthMHz, &d.WiFi, &mlo, &d.MLOLinks); qerr != nil {
					rows.Close()
					return qerr
				}
				d.Online = online == 1
				d.MLO = mlo == 1
				previous.Devices = append(previous.Devices, d)
			}
			rows.Close()
		}
	}
	profilesJSON, _ := json.Marshal(o.Profiles)
	radiosJSON, _ := json.Marshal(o.Radios)
	res, err := tx.ExecContext(ctx, `INSERT INTO cycles(at,profiles,radios) VALUES(?,?,?) ON CONFLICT(at) DO NOTHING`, ms(o.Time), string(profilesJSON), string(radiosJSON))
	if err != nil {
		return errors.New("采集轮次保存失败")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return tx.Commit()
	}
	at, hour := ms(o.Time), hourMs(o.Time)
	for _, d := range o.Devices {
		if _, err = tx.ExecContext(ctx, `INSERT INTO devices(at,mac,name,online,seen_via,ip,ap_id,band,rssi,tx_mbps,width_mhz,wifi,mlo,mlo_links)
          VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(at,mac) DO NOTHING`,
			at, d.MAC, d.Name, boolInt(d.Online), d.SeenVia, d.IP, d.APID, d.Band, d.RSSI, d.TXMbps, d.WidthMHz, d.WiFi, boolInt(d.MLO), d.MLOLinks); err != nil {
			return err
		}
		var confirmed any
		if d.Online {
			confirmed = at
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO entities(mac,name,ip,last_observed_at,last_confirmed_at) VALUES(?,?,?,?,?)
          ON CONFLICT(mac) DO UPDATE SET name=excluded.name, ip=excluded.ip, last_observed_at=excluded.last_observed_at,
            last_confirmed_at=MAX(COALESCE(entities.last_confirmed_at,0), COALESCE(excluded.last_confirmed_at,0))
          WHERE entities.last_observed_at < excluded.last_observed_at`,
			d.MAC, d.Name, d.IP, at, confirmed); err != nil {
			return err
		}
		aps, bands := []string{}, []string{}
		if d.APID != "" {
			aps = append(aps, d.APID)
		}
		if d.Band != "" {
			bands = append(bands, d.Band)
		}
		apsJSON, _ := json.Marshal(aps)
		bandsJSON, _ := json.Marshal(bands)
		if _, err = tx.ExecContext(ctx, `INSERT INTO device_hourly(hour,mac,name,n,confirmed,rssi_sum,rssi_n,rssi_min,rssi_max,
            tx_sum,tx_n,tx_peak,aps,bands) VALUES(?,?,?,1,?,?,?,?,?,?,?,?,?,?)
          ON CONFLICT(hour,mac) DO UPDATE SET name=excluded.name, n=device_hourly.n+1, confirmed=device_hourly.confirmed+excluded.confirmed,
            rssi_sum=device_hourly.rssi_sum+excluded.rssi_sum, rssi_n=device_hourly.rssi_n+excluded.rssi_n,
            rssi_min=MIN(COALESCE(device_hourly.rssi_min,excluded.rssi_min), COALESCE(excluded.rssi_min,device_hourly.rssi_min)),
            rssi_max=MAX(COALESCE(device_hourly.rssi_max,excluded.rssi_max), COALESCE(excluded.rssi_max,device_hourly.rssi_max)),
            tx_sum=device_hourly.tx_sum+excluded.tx_sum, tx_n=device_hourly.tx_n+excluded.tx_n,
            tx_peak=MAX(COALESCE(device_hourly.tx_peak,0), COALESCE(excluded.tx_peak,0)),
            aps=excluded.aps, bands=excluded.bands`,
			hour, d.MAC, d.Name, boolInt(d.Online), floatOr(d.RSSI, 0), boolIntPtr(d.RSSI), d.RSSI, d.RSSI,
			floatOr(d.TXMbps, 0), boolIntPtr(d.TXMbps), d.TXMbps, string(apsJSON), string(bandsJSON)); err != nil {
			return err
		}
		if d.Traffic != nil {
			t := d.Traffic
			if _, err = tx.ExecContext(ctx, `INSERT INTO device_traffic(at,mac,name,ip,down_bytes,up_bytes,down_mbps,up_mbps)
              VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(at,mac) DO NOTHING`,
				ms(t.Time), d.MAC, d.Name, d.IP, t.DownBytes, t.UpBytes, t.DownMbps, t.UpMbps); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO device_traffic_hourly(hour,mac,name,down_bytes,up_bytes,down_peak,up_peak,samples)
              VALUES(?,?,?,?,?,?,?,1) ON CONFLICT(hour,mac) DO UPDATE SET name=excluded.name,
                down_bytes=device_traffic_hourly.down_bytes+excluded.down_bytes, up_bytes=device_traffic_hourly.up_bytes+excluded.up_bytes,
                down_peak=MAX(device_traffic_hourly.down_peak,excluded.down_peak), up_peak=MAX(device_traffic_hourly.up_peak,excluded.up_peak),
                samples=device_traffic_hourly.samples+1`,
				hour, d.MAC, d.Name, t.DownBytes, t.UpBytes, t.DownMbps, t.UpMbps); err != nil {
				return err
			}
		}
	}
	if o.Traffic != nil {
		t := *o.Traffic
		if _, err = tx.ExecContext(ctx, `INSERT INTO traffic(at,down_mbps,up_mbps,down_peak,up_peak) VALUES(?,?,?,?,?) ON CONFLICT(at) DO NOTHING`,
			ms(t.Time), t.DownMbps, t.UpMbps, t.DownPeak, t.UpPeak); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO traffic_hourly(hour,n,down_sum,up_sum,down_peak,up_peak) VALUES(?,1,?,?,?,?)
          ON CONFLICT(hour) DO UPDATE SET n=traffic_hourly.n+1, down_sum=traffic_hourly.down_sum+excluded.down_sum,
            up_sum=traffic_hourly.up_sum+excluded.up_sum, down_peak=MAX(traffic_hourly.down_peak,excluded.down_peak),
            up_peak=MAX(traffic_hourly.up_peak,excluded.up_peak)`,
			hour, t.DownMbps, t.UpMbps, coalesceF(t.DownPeak, t.DownMbps), coalesceF(t.UpPeak, t.UpMbps)); err != nil {
			return err
		}
	}
	for _, p := range o.Ping {
		var avg, min, max any
		if p.Recv > 0 {
			avg, min, max = p.Avg, p.Min, p.Max
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO ping(at,target,host,sent,recv,rtt_avg,rtt_min,rtt_max) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(at,target) DO NOTHING`,
			at, p.Target, p.Host, p.Sent, p.Recv, avg, min, max); err != nil {
			return err
		}
		var sum, n float64
		if avg != nil {
			sum, n = p.Avg, 1
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO ping_hourly(hour,target,host,sent,recv,rtt_sum,rtt_n,rtt_peak) VALUES(?,?,?,?,?,?,?,?)
          ON CONFLICT(hour,target) DO UPDATE SET host=excluded.host, sent=ping_hourly.sent+excluded.sent, recv=ping_hourly.recv+excluded.recv,
            rtt_sum=ping_hourly.rtt_sum+excluded.rtt_sum, rtt_n=ping_hourly.rtt_n+excluded.rtt_n,
            rtt_peak=MAX(COALESCE(ping_hourly.rtt_peak,0), COALESCE(excluded.rtt_peak,0))`,
			hour, p.Target, p.Host, p.Sent, p.Recv, sum, int(n), avg); err != nil {
			return err
		}
	}
	for _, p := range o.Profiles {
		var cpu, temp *float64
		if p.Status == "ok" {
			cpu, temp = p.CPU, p.Temperature
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO profile_hourly(hour,profile_id,name,n,ok,cpu_sum,cpu_n,cpu_peak,temp_sum,temp_n,temp_peak)
          VALUES(?,?,?,1,?,?,?,?,?,?,?) ON CONFLICT(hour,profile_id) DO UPDATE SET name=excluded.name, n=profile_hourly.n+1,
            ok=profile_hourly.ok+excluded.ok, cpu_sum=profile_hourly.cpu_sum+excluded.cpu_sum, cpu_n=profile_hourly.cpu_n+excluded.cpu_n,
            cpu_peak=MAX(COALESCE(profile_hourly.cpu_peak,0), COALESCE(excluded.cpu_peak,0)),
            temp_sum=profile_hourly.temp_sum+excluded.temp_sum, temp_n=profile_hourly.temp_n+excluded.temp_n,
            temp_peak=MAX(COALESCE(profile_hourly.temp_peak,0), COALESCE(excluded.temp_peak,0))`,
			hour, p.ID, p.Name, boolInt(p.Status == "ok"), coalesceF(cpu, 0), boolIntPtr(cpu), cpu,
			coalesceF(temp, 0), boolIntPtr(temp), temp); err != nil {
			return err
		}
	}
	for _, r := range o.Radios {
		entity := r.APID + "/" + r.Interface
		if _, err = tx.ExecContext(ctx, `INSERT INTO radio_hourly(hour,entity,ap_id,interface,band,ssid,n,util_sum,util_n,util_peak,clients_sum,clients_peak)
          VALUES(?,?,?,?,?,?,1,?,?,?,?,?) ON CONFLICT(hour,entity) DO UPDATE SET ssid=excluded.ssid, n=radio_hourly.n+1,
            util_sum=radio_hourly.util_sum+excluded.util_sum, util_n=radio_hourly.util_n+excluded.util_n,
            util_peak=MAX(COALESCE(radio_hourly.util_peak,0), COALESCE(excluded.util_peak,0)),
            clients_sum=radio_hourly.clients_sum+excluded.clients_sum, clients_peak=MAX(radio_hourly.clients_peak,excluded.clients_peak)`,
			hour, entity, r.APID, r.Interface, r.Band, r.SSID, coalesceF(r.Utilization, 0), boolIntPtr(r.Utilization), r.Utilization, r.Clients, r.Clients); err != nil {
			return err
		}
	}
	for _, e := range changesBetween(previous, o) {
		if e.ID == "" {
			e.ID = eventID(e)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO events(id,at,kind,mac,name,profile_id,text) VALUES(?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`,
			e.ID, ms(e.Time), e.Kind, e.MAC, e.Name, e.ProfileID, e.Text); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func boolIntPtr(v *float64) int {
	if v == nil {
		return 0
	}
	return 1
}

func floatOr(v *float64, fallback float64) float64 {
	if v == nil {
		return fallback
	}
	return *v
}

func coalesceF(v *float64, fallback float64) float64 {
	if v == nil {
		return fallback
	}
	return *v
}

var _ = strings.TrimSpace
