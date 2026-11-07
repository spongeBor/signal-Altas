package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5"
	"signalatlas/api/internal/domain"
	"signalatlas/api/internal/history"
)

var _ history.Store = (*Postgres)(nil)

func (p *Postgres) ConfigureRetention(detail, summary int) error {
	if detail < 1 || detail > 365 || summary < detail || summary > 3650 {
		return errors.New("保留期限无效：明细1到365天，汇总不少于明细且最多3650天")
	}
	p.detailDays = detail
	p.summaryDays = summary
	return nil
}
func jsonBytes(v any) []byte { b, _ := json.Marshal(v); return b }
func val(n *float64) float64 {
	if n == nil {
		return 0
	}
	return *n
}
func count(n *float64) int {
	if n == nil {
		return 0
	}
	return 1
}
func flag(b bool) int {
	if b {
		return 1
	}
	return 0
}
func hour(t time.Time) time.Time { return t.UTC().Truncate(time.Hour) }
func eventID(e history.Event) string {
	b := sha256.Sum256([]byte(e.Time.UTC().Format(time.RFC3339Nano) + "|" + e.Kind + "|" + e.MAC + "|" + e.ProfileID + "|" + e.Text))
	return hex.EncodeToString(b[:])
}
func queueEvent(b *pgx.Batch, e history.Event) {
	if e.ID == "" {
		e.ID = eventID(e)
	}
	b.Queue(`INSERT INTO signal_atlas.history_events(id,at,kind,mac,name,profile_id,text) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`, e.ID, e.Time, e.Kind, e.MAC, e.Name, e.ProfileID, e.Text)
}
func queueTraffic(b *pgx.Batch, t domain.TrafficPoint) {
	// The RETURNING CTE makes imports and retries idempotent for both levels.
	b.Queue(`WITH added AS (INSERT INTO signal_atlas.history_traffic(at,down_mbps,up_mbps,down_peak_mbps,up_peak_mbps) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING RETURNING *)
 INSERT INTO signal_atlas.history_traffic_hourly(hour,n,down_sum,up_sum,down_peak,up_peak) SELECT $6,1,down_mbps,up_mbps,COALESCE(down_peak_mbps,down_mbps),COALESCE(up_peak_mbps,up_mbps) FROM added
 ON CONFLICT(hour) DO UPDATE SET n=history_traffic_hourly.n+1,down_sum=history_traffic_hourly.down_sum+EXCLUDED.down_sum,up_sum=history_traffic_hourly.up_sum+EXCLUDED.up_sum,down_peak=GREATEST(history_traffic_hourly.down_peak,EXCLUDED.down_peak),up_peak=GREATEST(history_traffic_hourly.up_peak,EXCLUDED.up_peak)`, t.Time, t.DownMbps, t.UpMbps, t.DownPeak, t.UpPeak, hour(t.Time))
}
func queuePing(b *pgx.Batch, p domain.PingResult, at time.Time) {
	var avg, min, max *float64
	if p.Recv > 0 {
		a, lo, hi := p.Avg, p.Min, p.Max
		avg, min, max = &a, &lo, &hi
	}
	b.Queue(`WITH added AS (INSERT INTO signal_atlas.history_ping(at,target,host,sent,recv,rtt_avg,rtt_min,rtt_max) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING RETURNING *)
 INSERT INTO signal_atlas.history_ping_hourly(hour,target,host,sent,recv,rtt_sum,rtt_n,rtt_peak)
 SELECT $9,target,host,sent,recv,COALESCE(rtt_avg,0),CASE WHEN rtt_avg IS NULL THEN 0 ELSE 1 END,rtt_avg FROM added
 ON CONFLICT(hour,target) DO UPDATE SET host=EXCLUDED.host,sent=history_ping_hourly.sent+EXCLUDED.sent,recv=history_ping_hourly.recv+EXCLUDED.recv,
   rtt_sum=history_ping_hourly.rtt_sum+EXCLUDED.rtt_sum,rtt_n=history_ping_hourly.rtt_n+EXCLUDED.rtt_n,
   rtt_peak=GREATEST(COALESCE(history_ping_hourly.rtt_peak,0),COALESCE(EXCLUDED.rtt_peak,0))`,
		at, p.Target, p.Host, p.Sent, p.Recv, avg, min, max, hour(at))
}
func queueDeviceTraffic(b *pgx.Batch, d domain.Device) {
	if d.Traffic == nil {
		return
	}
	t := d.Traffic
	b.Queue(`WITH added AS (
 INSERT INTO signal_atlas.history_device_traffic(at,mac,name,ip,down_bytes,up_bytes,down_mbps,up_mbps)
 VALUES($1,$2,$3,NULLIF($4,'')::inet,$5,$6,$7,$8) ON CONFLICT DO NOTHING RETURNING *
)
INSERT INTO signal_atlas.history_device_traffic_hourly(hour,mac,name,down_bytes,up_bytes,down_peak,up_peak,samples)
SELECT $9,mac,name,down_bytes,up_bytes,down_mbps,up_mbps,1 FROM added
ON CONFLICT(hour,mac) DO UPDATE SET name=EXCLUDED.name,down_bytes=history_device_traffic_hourly.down_bytes+EXCLUDED.down_bytes,up_bytes=history_device_traffic_hourly.up_bytes+EXCLUDED.up_bytes,down_peak=GREATEST(history_device_traffic_hourly.down_peak,EXCLUDED.down_peak),up_peak=GREATEST(history_device_traffic_hourly.up_peak,EXCLUDED.up_peak),samples=history_device_traffic_hourly.samples+1`,
		t.Time, d.MAC, d.Name, d.IP, t.DownBytes, t.UpBytes, t.DownMbps, t.UpMbps, hour(t.Time))
}
func changesBetween(old, current history.Observation) []history.Event {
	out := []history.Event{}
	// A gap is a gap, not evidence of what happened while collection stopped.
	if old.Time.IsZero() || current.Time.Sub(old.Time) > 5*time.Minute || !current.Time.After(old.Time) {
		return out
	}
	devices := map[string]domain.Device{}
	for _, d := range old.Devices {
		devices[d.MAC] = d
	}
	for _, d := range current.Devices {
		o, ok := devices[d.MAC]
		if !ok {
			continue
		}
		e := history.Event{Time: current.Time, MAC: d.MAC, Name: d.Name}
		if o.Online && d.Online && o.APID != "" && d.APID != "" && o.APID != d.APID {
			e.Kind = "association"
			e.Text = o.APName + " → " + d.APName
			out = append(out, e)
		}
		if o.Online != d.Online {
			e.Kind = "connection"
			e.Text = "当前采集未能确认连接（不等同设备离线）"
			if d.Online {
				e.Text = "重新确认连接：" + d.SeenVia
			}
			out = append(out, e)
		}
	}
	profiles := map[string]history.Profile{}
	for _, r := range old.Profiles {
		profiles[r.ID] = r
	}
	for _, r := range current.Profiles {
		o, ok := profiles[r.ID]
		if !ok {
			continue
		}
		e := history.Event{Time: current.Time, ProfileID: r.ID, Name: r.Name}
		if o.Status != r.Status {
			e.Kind = "collector"
			e.Text = "采集状态：" + o.Status + " → " + r.Status
			out = append(out, e)
		}
		if o.Status == "ok" && r.Status == "ok" && o.WANUp != nil && r.WANUp != nil && *o.WANUp != *r.WANUp {
			e.Kind = "wan"
			e.Text = "WAN连接已断开"
			if *r.WANUp {
				e.Text = "WAN连接已恢复"
			}
			out = append(out, e)
		}
		if o.Status == "ok" && r.Status == "ok" && o.PON != "" && r.PON != "" && o.PON != r.PON {
			e.Kind = "pon"
			e.Text = "PON状态：" + o.PON + " → " + r.PON
			out = append(out, e)
		}
	}
	return out
}
func (p *Postgres) Append(ctx context.Context, o history.Observation) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return errors.New("历史事务创建失败")
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(673219855)"); err != nil {
		return err
	}
	previous := history.Observation{}
	var profiles, radios []byte
	err = tx.QueryRow(ctx, `SELECT at,profiles,radios FROM signal_atlas.history_cycles ORDER BY at DESC LIMIT 1`).Scan(&previous.Time, &profiles, &radios)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return errors.New("历史基线读取失败")
	}
	if !previous.Time.IsZero() && o.Time.Sub(previous.Time) <= 5*time.Minute {
		_ = json.Unmarshal(profiles, &previous.Profiles)
		rows, e := tx.Query(ctx, `SELECT mac,name,online,seen_via,ip,ap_id,band,rssi,tx_mbps,width_mhz FROM signal_atlas.history_devices WHERE at=$1`, previous.Time)
		if e != nil {
			return e
		}
		for rows.Next() {
			var d domain.Device
			if e = rows.Scan(&d.MAC, &d.Name, &d.Online, &d.SeenVia, &d.IP, &d.APID, &d.Band, &d.RSSI, &d.TXMbps, &d.WidthMHz); e != nil {
				rows.Close()
				return e
			}
			previous.Devices = append(previous.Devices, d)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	tag, err := tx.Exec(ctx, `INSERT INTO signal_atlas.history_cycles(at,profiles,radios) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, o.Time, jsonBytes(o.Profiles), jsonBytes(o.Radios))
	if err != nil {
		return errors.New("采集轮次保存失败")
	}
	if tag.RowsAffected() == 0 {
		return tx.Commit(ctx)
	}
	batch := &pgx.Batch{}
	for _, d := range o.Devices {
		batch.Queue(`INSERT INTO signal_atlas.history_devices(at,mac,name,online,seen_via,ip,ap_id,band,rssi,tx_mbps,width_mhz) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT DO NOTHING`,
			o.Time, d.MAC, d.Name, d.Online, d.SeenVia, d.IP, d.APID, d.Band, d.RSSI, d.TXMbps, d.WidthMHz)
		var confirmed *time.Time
		if d.Online {
			confirmed = &o.Time
		}
		batch.Queue(`INSERT INTO signal_atlas.history_entities(mac,name,ip,last_observed_at,last_confirmed_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(mac) DO UPDATE SET name=EXCLUDED.name,ip=EXCLUDED.ip,last_observed_at=EXCLUDED.last_observed_at,last_confirmed_at=GREATEST(history_entities.last_confirmed_at,EXCLUDED.last_confirmed_at) WHERE history_entities.last_observed_at<EXCLUDED.last_observed_at`, d.MAC, d.Name, d.IP, o.Time, confirmed)
		aps, bands := []string{}, []string{}
		if d.APID != "" {
			aps = append(aps, d.APID)
		}
		if d.Band != "" {
			bands = append(bands, d.Band)
		}
		batch.Queue(`INSERT INTO signal_atlas.history_device_hourly(hour,mac,name,n,confirmed,rssi_sum,rssi_n,rssi_min,rssi_max,tx_sum,tx_n,tx_peak,aps,bands) VALUES($1,$2,$3,1,$4,$5,$6,$7,$7,$8,$9,$10,$11,$12)
   ON CONFLICT(hour,mac) DO UPDATE SET name=EXCLUDED.name,n=history_device_hourly.n+1,confirmed=history_device_hourly.confirmed+EXCLUDED.confirmed,rssi_sum=history_device_hourly.rssi_sum+EXCLUDED.rssi_sum,rssi_n=history_device_hourly.rssi_n+EXCLUDED.rssi_n,rssi_min=LEAST(history_device_hourly.rssi_min,EXCLUDED.rssi_min),rssi_max=GREATEST(history_device_hourly.rssi_max,EXCLUDED.rssi_max),tx_sum=history_device_hourly.tx_sum+EXCLUDED.tx_sum,tx_n=history_device_hourly.tx_n+EXCLUDED.tx_n,tx_peak=GREATEST(history_device_hourly.tx_peak,EXCLUDED.tx_peak),aps=ARRAY(SELECT DISTINCT unnest(history_device_hourly.aps||EXCLUDED.aps)),bands=ARRAY(SELECT DISTINCT unnest(history_device_hourly.bands||EXCLUDED.bands))`, hour(o.Time), d.MAC, d.Name, flag(d.Online), val(d.RSSI), count(d.RSSI), d.RSSI, val(d.TXMbps), count(d.TXMbps), d.TXMbps, aps, bands)
	}
	for _, d := range o.Devices {
		queueDeviceTraffic(batch, d)
	}
	for _, r := range o.Profiles {
		batch.Queue(`INSERT INTO signal_atlas.history_profile_hourly(hour,profile_id,name,n,ok,cpu_sum,cpu_n,cpu_peak,temp_sum,temp_n,temp_peak) VALUES($1,$2,$3,1,$4,$5,$6,$7,$8,$9,$10)
   ON CONFLICT(hour,profile_id) DO UPDATE SET name=EXCLUDED.name,n=history_profile_hourly.n+1,ok=history_profile_hourly.ok+EXCLUDED.ok,cpu_sum=history_profile_hourly.cpu_sum+EXCLUDED.cpu_sum,cpu_n=history_profile_hourly.cpu_n+EXCLUDED.cpu_n,cpu_peak=GREATEST(history_profile_hourly.cpu_peak,EXCLUDED.cpu_peak),temp_sum=history_profile_hourly.temp_sum+EXCLUDED.temp_sum,temp_n=history_profile_hourly.temp_n+EXCLUDED.temp_n,temp_peak=GREATEST(history_profile_hourly.temp_peak,EXCLUDED.temp_peak)`, hour(o.Time), r.ID, r.Name, flag(r.Status == "ok"), val(r.CPU), count(r.CPU), r.CPU, val(r.Temperature), count(r.Temperature), r.Temperature)
	}
	for _, r := range o.Radios {
		key := r.APID + "/" + r.Interface
		batch.Queue(`INSERT INTO signal_atlas.history_radio_hourly(hour,entity,ap_id,interface,band,ssid,n,util_sum,util_n,util_peak,clients_sum,clients_peak) VALUES($1,$2,$3,$4,$5,$6,1,$7,$8,$9,$10,$10)
   ON CONFLICT(hour,entity) DO UPDATE SET ssid=EXCLUDED.ssid,n=history_radio_hourly.n+1,util_sum=history_radio_hourly.util_sum+EXCLUDED.util_sum,util_n=history_radio_hourly.util_n+EXCLUDED.util_n,util_peak=GREATEST(history_radio_hourly.util_peak,EXCLUDED.util_peak),clients_sum=history_radio_hourly.clients_sum+EXCLUDED.clients_sum,clients_peak=GREATEST(history_radio_hourly.clients_peak,EXCLUDED.clients_peak)`, hour(o.Time), key, r.APID, r.Interface, r.Band, r.SSID, val(r.Utilization), count(r.Utilization), r.Utilization, r.Clients)
	}
	if o.Traffic != nil {
		queueTraffic(batch, *o.Traffic)
	}
	for _, ping := range o.Ping {
		queuePing(batch, ping, o.Time)
	}
	for _, e := range changesBetween(previous, o) {
		queueEvent(batch, e)
	}
	br := tx.SendBatch(ctx, batch)
	if err = br.Close(); err != nil {
		return errors.New("历史明细或汇总保存失败")
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("历史提交失败")
	}
	return nil
}
func recordDocumentChange(ctx context.Context, tx pgx.Tx, key string, old, new []byte) error {
	if key != "labels" && key != "topology" {
		return nil
	}
	var a, b any
	_ = json.Unmarshal(old, &a)
	if err := json.Unmarshal(new, &b); err != nil {
		return err
	}
	if reflect.DeepEqual(a, b) {
		return nil
	}
	insert := func(kind, subject string, before, after any) error {
		_, err := tx.Exec(ctx, `INSERT INTO signal_atlas.history_changes(kind,subject,before_value,after_value) VALUES($1,$2,$3,$4)`, kind, subject, jsonBytes(before), jsonBytes(after))
		return err
	}
	if key == "topology" {
		before, after := topologyForArchive(old), topologyForArchive(new)
		if reflect.DeepEqual(before, after) {
			return nil
		}
		return insert("topology", "topology", before, after)
	}
	if a == nil {
		return insert("labels_baseline", "labels", nil, b)
	}
	var before, after map[string]domain.Label
	if json.Unmarshal(old, &before) != nil || json.Unmarshal(new, &after) != nil {
		return errors.New("标签记录无效")
	}
	for mac, l := range after {
		v, ok := before[mac]
		if !ok || v.Name != l.Name {
			if err := insert("rename", mac, map[string]string{"name": v.Name}, map[string]string{"name": l.Name}); err != nil {
				return err
			}
		}
	}
	return nil
}
func (p *Postgres) Import(ctx context.Context, s history.Seed) error {
	if len(s.Traffic) > 20000 || len(s.Events) > 2000 {
		return errors.New("导入记录过多")
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	b := &pgx.Batch{}
	for _, t := range s.Traffic {
		if t.Time.IsZero() || t.DownMbps < 0 || t.UpMbps < 0 {
			return errors.New("导入流量记录无效")
		}
		queueTraffic(b, t)
	}
	for _, e := range s.Events {
		if e.Time.IsZero() || len(e.Text) > 500 || len(e.Name) > 160 {
			return errors.New("导入事件无效")
		}
		queueEvent(b, history.Event{Time: e.Time, Kind: "association", MAC: e.MAC, Name: e.Name, Text: e.Text})
	}
	if err = tx.SendBatch(ctx, b).Close(); err != nil {
		return errors.New("历史导入失败")
	}
	return tx.Commit(ctx)
}

// ForgetEntity removes a MAC from the ledger so a retired device stops showing
// up in lists; the historical samples themselves are kept.
func (p *Postgres) ForgetEntity(ctx context.Context, mac string) error {
	if _, err := p.pool.Exec(ctx, `DELETE FROM signal_atlas.history_entities WHERE mac=$1`, mac); err != nil {
		return errors.New("台账记录删除失败")
	}
	return nil
}

// Uptime aggregates the hourly device samples into a confirmed ratio.
func (p *Postgres) Uptime(ctx context.Context, from, to time.Time) ([]history.Uptime, error) {
	rows, err := p.pool.Query(ctx, `SELECT mac, COALESCE(sum(n),0)::bigint, COALESCE(sum(confirmed),0)::bigint
      FROM signal_atlas.history_device_hourly WHERE hour >= $1 AND hour < $2 GROUP BY mac ORDER BY mac`, from, to)
	if err != nil {
		return nil, errors.New("设备在线率读取失败")
	}
	defer rows.Close()
	out := []history.Uptime{}
	for rows.Next() {
		var u history.Uptime
		if err = rows.Scan(&u.MAC, &u.Samples, &u.Confirmed); err != nil {
			return nil, errors.New("设备在线率解析失败")
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// Stats reports how much space the history occupies and how fast it grows.
func (p *Postgres) Stats(ctx context.Context) (history.Stats, error) {
	s := history.Stats{}
	err := p.pool.QueryRow(ctx, `SELECT pg_database_size(current_database()),
      (SELECT count(*) FROM signal_atlas.history_traffic),
      (SELECT count(*) FROM signal_atlas.history_traffic WHERE at > now() - interval '24 hours'),
      (SELECT count(*) FROM signal_atlas.history_device_hourly)`).Scan(&s.DatabaseBytes, &s.TrafficRows, &s.TrafficRows24h, &s.DeviceHourlyRows)
	if err != nil {
		return s, errors.New("数据库统计读取失败")
	}
	_ = p.pool.QueryRow(ctx, `SELECT c.relname, pg_total_relation_size(c.oid)
      FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
      WHERE n.nspname = 'signal_atlas' AND c.relkind = 'r'
      ORDER BY pg_total_relation_size(c.oid) DESC LIMIT 1`).Scan(&s.LargestTable, &s.LargestBytes)
	return s, nil
}

func (p *Postgres) Maintain(ctx context.Context, now time.Time) error {
	raw := now.AddDate(0, 0, -p.detailDays)
	summary := now.AddDate(0, 0, -p.summaryDays)
	// Bounded deletes let a delayed cleanup catch up without a huge transaction.
	for _, table := range []string{"history_devices", "history_cycles", "history_traffic", "history_device_traffic", "history_events", "history_ping"} {
		for i := 0; i < 20; i++ {
			tag, err := p.pool.Exec(ctx, fmt.Sprintf(`WITH old AS (SELECT ctid FROM signal_atlas.%s WHERE at<$1 LIMIT 5000) DELETE FROM signal_atlas.%s WHERE ctid IN(SELECT ctid FROM old)`, table, table), raw)
			if err != nil {
				return errors.New("历史明细清理失败")
			}
			if tag.RowsAffected() < 5000 {
				break
			}
		}
	}
	for _, table := range []string{"history_device_hourly", "history_device_traffic_hourly", "history_profile_hourly", "history_radio_hourly", "history_traffic_hourly", "history_ping_hourly"} {
		_, err := p.pool.Exec(ctx, "DELETE FROM signal_atlas."+table+" WHERE hour<$1", summary)
		if err != nil {
			return errors.New("历史汇总清理失败")
		}
	}
	return nil
}
func (p *Postgres) Status(ctx context.Context) (history.Status, error) {
	s := history.Status{Enabled: true, DetailDays: p.detailDays, SummaryDays: p.summaryDays}
	err := p.pool.QueryRow(ctx, `SELECT min(t),max(t) FROM(
 SELECT min(at) t FROM signal_atlas.history_cycles UNION ALL SELECT max(at) FROM signal_atlas.history_cycles
 UNION ALL SELECT min(at) FROM signal_atlas.history_traffic UNION ALL SELECT max(at) FROM signal_atlas.history_traffic
 UNION ALL SELECT min(hour) FROM signal_atlas.history_traffic_hourly UNION ALL SELECT max(hour) FROM signal_atlas.history_traffic_hourly

 UNION ALL SELECT min(hour) FROM signal_atlas.history_device_hourly UNION ALL SELECT max(hour) FROM signal_atlas.history_device_hourly
 UNION ALL SELECT min(at) FROM signal_atlas.history_device_traffic UNION ALL SELECT max(at) FROM signal_atlas.history_device_traffic
 UNION ALL SELECT min(hour) FROM signal_atlas.history_device_traffic_hourly UNION ALL SELECT max(hour) FROM signal_atlas.history_device_traffic_hourly
 UNION ALL SELECT min(hour) FROM signal_atlas.history_profile_hourly UNION ALL SELECT max(hour) FROM signal_atlas.history_profile_hourly
 UNION ALL SELECT min(hour) FROM signal_atlas.history_radio_hourly UNION ALL SELECT max(hour) FROM signal_atlas.history_radio_hourly) x`).Scan(&s.Earliest, &s.Latest)
	return s, err
}

// LastConfirmed returns the last time each device was confirmed online.
func (p *Postgres) LastConfirmed(ctx context.Context) (map[string]time.Time, error) {
	rows, err := p.pool.Query(ctx, `SELECT mac,last_confirmed_at FROM signal_atlas.history_entities WHERE last_confirmed_at IS NOT NULL`)
	if err != nil {
		return nil, errors.New("最近确认时间读取失败")
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var mac string
		var at time.Time
		if err := rows.Scan(&mac, &at); err != nil {
			return nil, errors.New("最近确认时间读取失败")
		}
		out[mac] = at
	}
	return out, rows.Err()
}

func (p *Postgres) Entities(ctx context.Context) ([]json.RawMessage, error) {
	out := []json.RawMessage{}
	rows, err := p.pool.Query(ctx, `SELECT jsonb_build_object('mac',mac,'name',name,'ip',ip,'last_observed_at',last_observed_at) FROM signal_atlas.history_entities ORDER BY name,mac LIMIT 10000`)
	if err != nil {
		return nil, errors.New("历史设备列表读取失败")
	}
	defer rows.Close()
	for rows.Next() {
		var b json.RawMessage
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
func (p *Postgres) Query(ctx context.Context, q history.Query) (history.Page, error) {
	out := history.Page{Kind: q.Kind, Resolution: q.Resolution, Items: []json.RawMessage{}}
	// All SQL fragments below are fixed identifiers; all user values are parameters.
	source, t, k, projection, filter, col := "", "at", "''::text", "", "TRUE", ""
	if q.Resolution == "hour" {
		t = "hour"
		switch q.Kind {
		case "traffic":
			source = "signal_atlas.history_traffic_hourly"
			projection = `jsonb_build_object('time',hour,'down_mbps',down_sum/n,'up_mbps',up_sum/n,'down_peak',down_peak,'up_peak',up_peak,'samples',n)`
		case "device_traffic":
			source = "signal_atlas.history_device_traffic_hourly"
			k = "mac"
			col = "mac"
			projection = `jsonb_build_object('time',hour,'mac',mac,'name',name,'down_bytes',down_bytes,'up_bytes',up_bytes,'down_peak',down_peak,'up_peak',up_peak,'samples',samples)`
		case "devices":
			source = "signal_atlas.history_device_hourly"
			k = "mac"
			col = "mac"
			projection = `jsonb_build_object('time',hour,'mac',mac,'name',name,'samples',n,'confirmed_samples',confirmed,'rssi',rssi_sum/NULLIF(rssi_n,0),'rssi_min',rssi_min,'rssi_max',rssi_max,'tx_mbps',tx_sum/NULLIF(tx_n,0),'tx_peak',tx_peak,'aps',aps,'bands',bands)`
		case "ping":
			source = "signal_atlas.history_ping_hourly"
			k = "target"
			col = "target"
			projection = `jsonb_build_object('time',hour,'target',target,'host',host,'sent',sent,'recv',recv,'loss',CASE WHEN sent>0 THEN (sent-recv)::double precision*100/sent ELSE 0 END,'rtt_avg',CASE WHEN rtt_n>0 THEN rtt_sum/rtt_n END,'rtt_peak',rtt_peak)`
		case "profiles":
			source = "signal_atlas.history_profile_hourly"
			k = "profile_id"
			col = "profile_id"
			projection = `jsonb_build_object('time',hour,'id',profile_id,'name',name,'samples',n,'ok_samples',ok,'cpu_percent',cpu_sum/NULLIF(cpu_n,0),'cpu_peak',cpu_peak,'chip_temperature',temp_sum/NULLIF(temp_n,0),'temperature_peak',temp_peak)`
		case "radios":
			source = "signal_atlas.history_radio_hourly"
			k = "entity"
			col = "ap_id"
			projection = `jsonb_build_object('time',hour,'ap_id',ap_id,'interface',interface,'band',band,'ssid',ssid,'samples',n,'utilization',util_sum/NULLIF(util_n,0),'utilization_peak',util_peak,'clients',clients_sum::double precision/n,'clients_peak',clients_peak)`
		}
	} else {
		switch q.Kind {
		case "traffic":
			source = "signal_atlas.history_traffic"
			projection = `jsonb_build_object('time',at,'down_mbps',down_mbps,'up_mbps',up_mbps,'down_peak',down_peak_mbps,'up_peak',up_peak_mbps)`
		case "device_traffic":
			source = "signal_atlas.history_device_traffic"
			k = "mac"
			col = "mac"
			projection = `jsonb_build_object('time',at,'mac',mac,'name',name,'ip',host(ip),'down_bytes',down_bytes,'up_bytes',up_bytes,'down_mbps',down_mbps,'up_mbps',up_mbps)`
		case "devices":
			source = "signal_atlas.history_devices"
			k = "mac"
			col = "mac"
			projection = `jsonb_build_object('time',at,'mac',mac,'name',name,'online',online,'seen_via',seen_via,'ip',ip,'ap_id',ap_id,'band',band,'rssi',rssi,'tx_mbps',tx_mbps,'width_mhz',width_mhz)`
		case "ping":
			source = "signal_atlas.history_ping"
			k = "target"
			col = "target"
			projection = `jsonb_build_object('time',at,'target',target,'host',host,'sent',sent,'recv',recv,'loss',CASE WHEN sent>0 THEN (sent-recv)::double precision*100/sent ELSE 0 END,'rtt_avg',rtt_avg,'rtt_min',rtt_min,'rtt_max',rtt_max)`
		case "profiles":
			source = "signal_atlas.history_cycles CROSS JOIN LATERAL jsonb_array_elements(profiles) p"
			k = "p->>'id'"
			col = "p->>'id'"
			projection = `p||jsonb_build_object('time',at)`
		case "radios":
			source = "signal_atlas.history_cycles CROSS JOIN LATERAL jsonb_array_elements(radios) r"
			k = "(r->>'ap_id')||'/'||(r->>'interface')"
			col = "r->>'ap_id'"
			projection = `r||jsonb_build_object('time',at)`
		case "events":
			source = "signal_atlas.history_events"
			k = "id"
			projection = `jsonb_build_object('id',id,'time',at,'kind',kind,'mac',mac,'name',name,'profile_id',profile_id,'text',text)`
			if q.MAC != "" {
				col = "mac"
			} else if q.ProfileID != "" {
				col = "profile_id"
			}
		case "changes":
			source = "signal_atlas.history_changes"
			k = "id::text"
			col = "subject"
			projection = `jsonb_build_object('id',id,'time',at,'kind',kind,'subject',subject,'before',before_value,'after',after_value)`
		}
	}
	if source == "" {
		return out, errors.New("历史查询类型或精度无效")
	}
	arg := ""
	if q.MAC != "" {
		arg = q.MAC
	} else if q.ProfileID != "" {
		arg = q.ProfileID
	} else if q.Target != "" {
		arg = q.Target
	}
	if arg != "" && col != "" {
		filter = col + "=$3"
	}
	if q.Kind == "events" && q.MAC != "" && q.ProfileID != "" {
		filter = "mac=$3 AND profile_id=$7"
	}
	cursorAt, cursorKey := q.To, "\uffff"
	if q.Cursor != nil {
		cursorAt, cursorKey = q.Cursor.Time, q.Cursor.Key
	}
	// Passing an unused $3 is still typed explicitly, including unfiltered requests.
	sql := fmt.Sprintf(`SELECT %s,%s,%s FROM %s WHERE %s>=$1 AND %s<$2 AND (%s) AND ($3::text IS NOT NULL) AND ($7::text IS NOT NULL) AND (%s,%s)<($4,$5) ORDER BY %s DESC,%s DESC LIMIT $6`, t, k, projection, source, t, t, filter, t, k, t, k)
	rows, err := p.pool.Query(ctx, sql, q.From, q.To, arg, cursorAt, cursorKey, q.Limit+1, q.ProfileID)
	if err != nil {
		return out, errors.New("历史查询失败")
	}
	defer rows.Close()
	var last history.Cursor
	for rows.Next() {
		var c history.Cursor
		var raw json.RawMessage
		if err = rows.Scan(&c.Time, &c.Key, &raw); err != nil {
			return out, err
		}
		if len(out.Items) == q.Limit {
			out.NextCursor = last.Encode()
			break
		}
		last = c
		out.Items = append(out.Items, raw)
	}
	return out, rows.Err()
}
func (p *Postgres) Snapshot(ctx context.Context, at time.Time) (history.Snapshot, error) {
	s := history.Snapshot{Devices: []domain.Device{}, Profiles: []history.Profile{}, Radios: []domain.Radio{}, Topology: json.RawMessage("null")}
	var a, b []byte
	err := p.pool.QueryRow(ctx, `SELECT at,profiles,radios FROM signal_atlas.history_cycles WHERE at<=$1 AND at>=$1-interval '5 minutes' ORDER BY at DESC LIMIT 1`, at).Scan(&s.Time, &a, &b)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, ErrNotFound
	}
	if err != nil {
		return s, errors.New("状态回放读取失败")
	}
	_ = json.Unmarshal(a, &s.Profiles)
	_ = json.Unmarshal(b, &s.Radios)
	rows, err := p.pool.Query(ctx, `SELECT mac,name,online,seen_via,ip,ap_id,band,rssi,tx_mbps,width_mhz FROM signal_atlas.history_devices WHERE at=$1 ORDER BY mac`, s.Time)
	if err != nil {
		return s, err
	}
	for rows.Next() {
		var d domain.Device
		if err = rows.Scan(&d.MAC, &d.Name, &d.Online, &d.SeenVia, &d.IP, &d.APID, &d.Band, &d.RSSI, &d.TXMbps, &d.WidthMHz); err != nil {
			rows.Close()
			return s, err
		}
		s.Devices = append(s.Devices, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return s, err
	}
	var topology []byte
	err = p.pool.QueryRow(ctx, `SELECT after_value FROM signal_atlas.history_changes WHERE kind='topology' AND at<=$1 ORDER BY at DESC,id DESC LIMIT 1`, s.Time).Scan(&topology)
	if err == nil {
		s.Topology = topology
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return s, err
	}
	return s, nil
}

// Archive the drawing and its provenance, not arbitrary nested device/config
// exports that may be attached to the current topology document.
func topologyForArchive(raw []byte) any {
	var root map[string]any
	if json.Unmarshal(raw, &root) != nil || root == nil {
		return nil
	}
	out := map[string]any{}
	for _, key := range []string{"recorded_at", "limitations"} {
		if v, ok := root[key]; ok {
			out[key] = v
		}
	}
	for key, fields := range map[string][]string{
		"nodes": {"id", "name", "host", "kind", "mac", "profile_id", "x", "y", "subtitle", "note", "observed_at"},
		"links": {"from", "to", "label", "evidence", "kind", "observed_at"},
	} {
		rows := []any{}
		if source, ok := root[key].([]any); ok {
			for _, row := range source {
				if m, ok := row.(map[string]any); ok {
					clean := map[string]any{}
					for _, field := range fields {
						if v, ok := m[field]; ok {
							clean[field] = v
						}
					}
					rows = append(rows, clean)
				}
			}
		}
		out[key] = rows
	}
	return out
}
