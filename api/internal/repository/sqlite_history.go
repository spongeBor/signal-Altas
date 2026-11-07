package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"time"

	"signalatlas/api/internal/domain"
	"signalatlas/api/internal/history"
)

// Query mirrors the PostgreSQL projections: the UI expects the same field names
// for raw and hourly rows, so each kind is built explicitly here.
func (s *SQLite) Query(ctx context.Context, q history.Query) (history.Page, error) {
	out := history.Page{Kind: q.Kind, Resolution: q.Resolution, Items: []json.RawMessage{}}
	from, to := ms(q.From), ms(q.To)
	cursorAt, cursorKey := to, "\uffff"
	if q.Cursor != nil {
		cursorAt, cursorKey = ms(q.Cursor.Time), q.Cursor.Key
	}
	limit := q.Limit
	add := func(v any) {
		if b, err := json.Marshal(v); err == nil {
			out.Items = append(out.Items, b)
		}
	}
	switch q.Kind + "/" + q.Resolution {
	case "traffic/raw":
		rows, err := s.db.QueryContext(ctx, `SELECT at,down_mbps,up_mbps,down_peak,up_peak FROM traffic
          WHERE at>=? AND at<? AND at<? ORDER BY at DESC LIMIT ?`, from, to, cursorAt, limit)
		if err != nil {
			return out, errors.New("历史查询失败")
		}
		defer rows.Close()
		for rows.Next() {
			var at int64
			var down, up, dp, up2 sql.NullFloat64
			if err = rows.Scan(&at, &down, &up, &dp, &up2); err != nil {
				return out, err
			}
			add(map[string]any{"time": fromMs(at), "down_mbps": down.Float64, "up_mbps": up.Float64,
				"down_peak": nullFloat(dp), "up_peak": nullFloat(up2)})
		}
		return out, rows.Err()
	case "traffic/hour":
		rows, err := s.db.QueryContext(ctx, `SELECT hour,n,down_sum,up_sum,down_peak,up_peak FROM traffic_hourly
          WHERE hour>=? AND hour<? AND hour<? ORDER BY hour DESC LIMIT ?`, from, to, cursorAt, limit)
		if err != nil {
			return out, errors.New("历史查询失败")
		}
		defer rows.Close()
		for rows.Next() {
			var hour, n int64
			var ds, us, dp, up float64
			if err = rows.Scan(&hour, &n, &ds, &us, &dp, &up); err != nil {
				return out, err
			}
			add(map[string]any{"time": fromMs(hour), "down_mbps": ds / float64(n), "up_mbps": us / float64(n),
				"down_peak": dp, "up_peak": up, "samples": n})
		}
		return out, rows.Err()
	case "device_traffic/raw":
		rows, err := s.db.QueryContext(ctx, `SELECT at,mac,name,ip,down_bytes,up_bytes,down_mbps,up_mbps FROM device_traffic
          WHERE at>=? AND at<? AND (?='' OR mac=?) AND (at<? OR (at=? AND mac<?)) ORDER BY at DESC, mac DESC LIMIT ?`,
			from, to, q.MAC, q.MAC, cursorAt, cursorAt, cursorKey, limit)
		if err != nil {
			return out, errors.New("历史查询失败")
		}
		defer rows.Close()
		for rows.Next() {
			var at int64
			var mac, name string
			var ip sql.NullString
			var db, ub int64
			var dm, um float64
			if err = rows.Scan(&at, &mac, &name, &ip, &db, &ub, &dm, &um); err != nil {
				return out, err
			}
			add(map[string]any{"time": fromMs(at), "mac": mac, "name": name, "ip": ip.String,
				"down_bytes": db, "up_bytes": ub, "down_mbps": dm, "up_mbps": um})
		}
		return out, rows.Err()
	case "device_traffic/hour":
		rows, err := s.db.QueryContext(ctx, `SELECT hour,mac,name,down_bytes,up_bytes,down_peak,up_peak,samples FROM device_traffic_hourly
          WHERE hour>=? AND hour<? AND (?='' OR mac=?) AND (hour<? OR (hour=? AND mac<?)) ORDER BY hour DESC, mac DESC LIMIT ?`,
			from, to, q.MAC, q.MAC, cursorAt, cursorAt, cursorKey, limit)
		if err != nil {
			return out, errors.New("历史查询失败")
		}
		defer rows.Close()
		for rows.Next() {
			var hour int64
			var mac, name string
			var db, ub, samples int64
			var dp, up float64
			if err = rows.Scan(&hour, &mac, &name, &db, &ub, &dp, &up, &samples); err != nil {
				return out, err
			}
			add(map[string]any{"time": fromMs(hour), "mac": mac, "name": name, "down_bytes": db, "up_bytes": ub,
				"down_peak": dp, "up_peak": up, "samples": samples})
		}
		return out, rows.Err()
	case "devices/raw":
		rows, err := s.db.QueryContext(ctx, `SELECT at,mac,name,online,seen_via,ip,ap_id,band,rssi,tx_mbps,width_mhz,wifi,mlo,mlo_links
          FROM devices WHERE at>=? AND at<? AND (?='' OR mac=?) AND (at<? OR (at=? AND mac<?)) ORDER BY at DESC, mac DESC LIMIT ?`,
			from, to, q.MAC, q.MAC, cursorAt, cursorAt, cursorKey, limit)
		if err != nil {
			return out, errors.New("历史查询失败")
		}
		defer rows.Close()
		for rows.Next() {
			var at int64
			var mac, name, seen, ip, apid, band, wifi string
			var online, mlo, links int
			var rssi, tx sql.NullFloat64
			var width sql.NullInt64
			if err = rows.Scan(&at, &mac, &name, &online, &seen, &ip, &apid, &band, &rssi, &tx, &width, &wifi, &mlo, &links); err != nil {
				return out, err
			}
			add(map[string]any{"time": fromMs(at), "mac": mac, "name": name, "online": online == 1, "seen_via": seen,
				"ip": ip, "ap_id": apid, "band": band, "rssi": nullFloat(rssi), "tx_mbps": nullFloat(tx),
				"width_mhz": nullInt(width), "wifi": wifi, "mlo": mlo == 1, "mlo_links": links})
		}
		return out, rows.Err()
	case "devices/hour":
		rows, err := s.db.QueryContext(ctx, `SELECT hour,mac,name,n,confirmed,rssi_sum,rssi_n,rssi_min,rssi_max,tx_sum,tx_n,tx_peak,aps,bands
          FROM device_hourly WHERE hour>=? AND hour<? AND (?='' OR mac=?) AND (hour<? OR (hour=? AND mac<?)) ORDER BY hour DESC, mac DESC LIMIT ?`,
			from, to, q.MAC, q.MAC, cursorAt, cursorAt, cursorKey, limit)
		if err != nil {
			return out, errors.New("历史查询失败")
		}
		defer rows.Close()
		for rows.Next() {
			var hour, n, confirmed, rssiN, txN int64
			var mac, name, aps, bands string
			var rssiSum, txSum float64
			var rssiMin, rssiMax, txPeak sql.NullFloat64
			if err = rows.Scan(&hour, &mac, &name, &n, &confirmed, &rssiSum, &rssiN, &rssiMin, &rssiMax, &txSum, &txN, &txPeak, &aps, &bands); err != nil {
				return out, err
			}
			var rssi, tx any
			if rssiN > 0 {
				rssi = rssiSum / float64(rssiN)
			}
			if txN > 0 {
				tx = txSum / float64(txN)
			}
			var apList, bandList []string
			_ = json.Unmarshal([]byte(aps), &apList)
			_ = json.Unmarshal([]byte(bands), &bandList)
			add(map[string]any{"time": fromMs(hour), "mac": mac, "name": name, "samples": n, "confirmed_samples": confirmed,
				"rssi": rssi, "rssi_min": nullFloat(rssiMin), "rssi_max": nullFloat(rssiMax), "tx_mbps": tx,
				"tx_peak": nullFloat(txPeak), "aps": apList, "bands": bandList})
		}
		return out, rows.Err()
	case "ping/raw", "ping/hour":
		table, keyCol := "ping", "at"
		if q.Resolution == "hour" {
			table, keyCol = "ping_hourly", "hour"
		}
		rows, err := s.db.QueryContext(ctx, `SELECT `+keyCol+`,target,host,sent,recv,`+
			map[bool]string{true: "rtt_sum,rtt_n,rtt_peak", false: "rtt_avg,rtt_min,rtt_max"}[q.Resolution == "hour"]+`
          FROM `+table+` WHERE `+keyCol+`>=? AND `+keyCol+`<? AND (?='' OR target=?)
          AND (`+keyCol+`<? OR (`+keyCol+`=? AND target<?)) ORDER BY `+keyCol+` DESC, target DESC LIMIT ?`,
			from, to, q.Target, q.Target, cursorAt, cursorAt, cursorKey, limit)
		if err != nil {
			return out, errors.New("历史查询失败")
		}
		defer rows.Close()
		for rows.Next() {
			var at, sent, recv int64
			var target, host string
			var a, b, c sql.NullFloat64
			if q.Resolution == "hour" {
				var sum float64
				var n int64
				if err = rows.Scan(&at, &target, &host, &sent, &recv, &sum, &n, &a); err != nil {
					return out, err
				}
				var avg any
				if n > 0 {
					avg = sum / float64(n)
				}
				add(map[string]any{"time": fromMs(at), "target": target, "host": host, "sent": sent, "recv": recv,
					"loss": lossPct(sent, recv), "rtt_avg": avg, "rtt_peak": nullFloat(a)})
				continue
			}
			if err = rows.Scan(&at, &target, &host, &sent, &recv, &a, &b, &c); err != nil {
				return out, err
			}
			add(map[string]any{"time": fromMs(at), "target": target, "host": host, "sent": sent, "recv": recv,
				"loss": lossPct(sent, recv), "rtt_avg": nullFloat(a), "rtt_min": nullFloat(b), "rtt_max": nullFloat(c)})
		}
		return out, rows.Err()
	case "events/raw":
		rows, err := s.db.QueryContext(ctx, `SELECT id,at,kind,mac,name,profile_id,text FROM events
          WHERE at>=? AND at<? AND (?='' OR mac=?) AND (?='' OR profile_id=?)
          AND (at<? OR (at=? AND id<?)) ORDER BY at DESC, id DESC LIMIT ?`,
			from, to, q.MAC, q.MAC, q.ProfileID, q.ProfileID, cursorAt, cursorAt, cursorKey, limit)
		if err != nil {
			return out, errors.New("历史查询失败")
		}
		defer rows.Close()
		for rows.Next() {
			var id, kind, mac, name, pid, text string
			var at int64
			if err = rows.Scan(&id, &at, &kind, &mac, &name, &pid, &text); err != nil {
				return out, err
			}
			add(map[string]any{"id": id, "time": fromMs(at), "kind": kind, "mac": mac, "name": name, "profile_id": pid, "text": text})
		}
		return out, rows.Err()
	case "changes/raw":
		rows, err := s.db.QueryContext(ctx, `SELECT id,at,kind,subject,before_value,after_value FROM changes
          WHERE at>=? AND at<? AND (?='' OR subject=?) AND (at<? OR (at=? AND CAST(id AS TEXT)<?)) ORDER BY at DESC, id DESC LIMIT ?`,
			from, to, q.MAC, q.MAC, cursorAt, cursorAt, cursorKey, limit)
		if err != nil {
			return out, errors.New("历史查询失败")
		}
		defer rows.Close()
		for rows.Next() {
			var id, at int64
			var kind, subject string
			var before, after sql.NullString
			if err = rows.Scan(&id, &at, &kind, &subject, &before, &after); err != nil {
				return out, err
			}
			add(map[string]any{"id": id, "time": fromMs(at), "kind": kind, "subject": subject,
				"before": rawJSON(before.String), "after": rawJSON(after.String)})
		}
		return out, rows.Err()
	case "profiles/raw", "radios/raw":
		return s.queryCyclePayload(ctx, q, out)
	case "profiles/hour":
		rows, err := s.db.QueryContext(ctx, `SELECT hour,profile_id,name,n,ok,cpu_sum,cpu_n,cpu_peak,temp_sum,temp_n,temp_peak
          FROM profile_hourly WHERE hour>=? AND hour<? AND (?='' OR profile_id=?) AND (hour<? OR (hour=? AND profile_id<?))
          ORDER BY hour DESC, profile_id DESC LIMIT ?`,
			from, to, q.ProfileID, q.ProfileID, cursorAt, cursorAt, cursorKey, limit)
		if err != nil {
			return out, errors.New("历史查询失败")
		}
		defer rows.Close()
		for rows.Next() {
			var hour, n, ok, cn, tn int64
			var id, name string
			var cs, ts float64
			var cp, tp sql.NullFloat64
			if err = rows.Scan(&hour, &id, &name, &n, &ok, &cs, &cn, &cp, &ts, &tn, &tp); err != nil {
				return out, err
			}
			var cpu, temp any
			if cn > 0 {
				cpu = cs / float64(cn)
			}
			if tn > 0 {
				temp = ts / float64(tn)
			}
			add(map[string]any{"time": fromMs(hour), "id": id, "name": name, "samples": n, "ok_samples": ok,
				"cpu_percent": cpu, "cpu_peak": nullFloat(cp), "chip_temperature": temp, "temperature_peak": nullFloat(tp)})
		}
		return out, rows.Err()
	case "radios/hour":
		rows, err := s.db.QueryContext(ctx, `SELECT hour,entity,ap_id,interface,band,ssid,n,util_sum,util_n,util_peak,clients_sum,clients_peak
          FROM radio_hourly WHERE hour>=? AND hour<? AND (?='' OR ap_id=?) AND (hour<? OR (hour=? AND entity<?))
          ORDER BY hour DESC, entity DESC LIMIT ?`,
			from, to, q.ProfileID, q.ProfileID, cursorAt, cursorAt, cursorKey, limit)
		if err != nil {
			return out, errors.New("历史查询失败")
		}
		defer rows.Close()
		for rows.Next() {
			var hour, n, utilN, clientsSum, clientsPeak int64
			var entity, apid, iface, band, ssid string
			var utilSum float64
			var utilPeak sql.NullFloat64
			if err = rows.Scan(&hour, &entity, &apid, &iface, &band, &ssid, &n, &utilSum, &utilN, &utilPeak, &clientsSum, &clientsPeak); err != nil {
				return out, err
			}
			var util any
			if utilN > 0 {
				util = utilSum / float64(utilN)
			}
			add(map[string]any{"time": fromMs(hour), "ap_id": apid, "interface": iface, "band": band, "ssid": ssid,
				"samples": n, "utilization": util, "utilization_peak": nullFloat(utilPeak),
				"clients": float64(clientsSum) / float64(n), "clients_peak": clientsPeak})
		}
		return out, rows.Err()
	}
	return out, errors.New("历史查询类型或精度无效")
}

// queryCyclePayload expands the profiles or radios arrays stored with each cycle.
func (s *SQLite) queryCyclePayload(ctx context.Context, q history.Query, out history.Page) (history.Page, error) {
	wantRadios := q.Kind == "radios"
	rows, err := s.db.QueryContext(ctx, `SELECT at,profiles,radios FROM cycles WHERE at>=? AND at<? AND at<=? ORDER BY at DESC LIMIT ?`,
		ms(q.From), ms(q.To), ms(time.Now()), 2000)
	if err != nil {
		return out, errors.New("历史查询失败")
	}
	defer rows.Close()
	type cycle struct {
		at    int64
		items []any
		raws  []json.RawMessage
	}
	list := []cycle{}
	for rows.Next() {
		var at int64
		var profilesRaw, radiosRaw string
		if err = rows.Scan(&at, &profilesRaw, &radiosRaw); err != nil {
			return out, err
		}
		c := cycle{at: at}
		if wantRadios {
			var radios []domain.Radio
			_ = json.Unmarshal([]byte(radiosRaw), &radios)
			for _, r := range radios {
				c.items = append(c.items, r)
			}
		} else {
			var profiles []history.Profile
			_ = json.Unmarshal([]byte(profilesRaw), &profiles)
			for _, p := range profiles {
				c.items = append(c.items, p)
			}
		}
		list = append(list, c)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	for _, c := range list {
		for _, item := range c.items {
			if wantRadios {
				r, ok := item.(domain.Radio)
				if !ok || (q.ProfileID != "" && r.APID != q.ProfileID) {
					continue
				}
				if len(out.Items) >= q.Limit {
					return out, nil
				}
				out.Items = append(out.Items, mustJSON(map[string]any{
					"time": fromMs(c.at), "ap_id": r.APID, "ap_name": r.APName, "interface": r.Interface,
					"band": r.Band, "ssid": r.SSID, "channel": r.Channel, "width_mhz": r.WidthMHz,
					"utilization": r.Utilization, "clients": r.Clients, "bss": r.BSS,
				}))
				continue
			}
			p, ok := item.(history.Profile)
			if !ok || (q.ProfileID != "" && p.ID != q.ProfileID) {
				continue
			}
			if len(out.Items) >= q.Limit {
				return out, nil
			}
			b, _ := json.Marshal(p)
			var m map[string]any
			_ = json.Unmarshal(b, &m)
			m["time"] = fromMs(c.at)
			out.Items = append(out.Items, mustJSON(m))
		}
	}
	return out, nil
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func rawJSON(s string) json.RawMessage {
	if s == "" {
		return json.RawMessage("null")
	}
	return json.RawMessage(s)
}

func nullFloat(v sql.NullFloat64) any {
	if v.Valid {
		return v.Float64
	}
	return nil
}

func nullInt(v sql.NullInt64) any {
	if v.Valid {
		return v.Int64
	}
	return nil
}

func lossPct(sent, recv int64) float64 {
	if sent <= 0 {
		return 0
	}
	return float64(sent-recv) * 100 / float64(sent)
}

// Status reports the covered time range.
func (s *SQLite) Status(ctx context.Context) (history.Status, error) {
	st := history.Status{Enabled: true, DetailDays: s.detailDays, SummaryDays: s.summaryDays}
	var minA, maxA, minH, maxH sql.NullInt64
	_ = s.db.QueryRowContext(ctx, `SELECT (SELECT MIN(at) FROM devices), (SELECT MAX(at) FROM devices),
      (SELECT MIN(hour) FROM device_hourly), (SELECT MAX(hour) FROM device_hourly)`).Scan(&minA, &maxA, &minH, &maxH)
	if minA.Valid {
		t := fromMs(minA.Int64)
		st.Earliest = &t
	}
	if maxA.Valid {
		t := fromMs(maxA.Int64)
		st.Latest = &t
	}
	if minH.Valid && (!minA.Valid || minH.Int64 < minA.Int64) {
		t := fromMs(minH.Int64)
		st.Earliest = &t
	}
	if maxH.Valid && (!maxA.Valid || maxH.Int64 > maxA.Int64) {
		t := fromMs(maxH.Int64)
		st.Latest = &t
	}
	return st, nil
}

// Entities lists the known device identities for the history filters.
func (s *SQLite) Entities(ctx context.Context) ([]json.RawMessage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT mac,name,ip FROM entities ORDER BY name`)
	if err != nil {
		return nil, errors.New("设备台账读取失败")
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var mac, name, ip string
		if err = rows.Scan(&mac, &name, &ip); err != nil {
			return nil, err
		}
		out = append(out, mustJSON(map[string]any{"mac": mac, "name": name, "ip": ip}))
	}
	return out, rows.Err()
}

// Snapshot replays one cycle.
func (s *SQLite) Snapshot(ctx context.Context, at time.Time) (history.Snapshot, error) {
	snap := history.Snapshot{Devices: []domain.Device{}, Profiles: []history.Profile{}, Radios: []domain.Radio{}, Topology: json.RawMessage("null")}
	var atMs int64
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT at,profiles FROM cycles WHERE at<=? AND at>=?-300000 ORDER BY at DESC LIMIT 1`,
		ms(at), ms(at)).Scan(&atMs, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return snap, ErrNotFound
	}
	if err != nil {
		return snap, errors.New("状态回放读取失败")
	}
	snap.Time = fromMs(atMs)
	_ = json.Unmarshal([]byte(raw), &snap.Profiles)
	rows, err := s.db.QueryContext(ctx, `SELECT mac,name,online,seen_via,ip,ap_id,band,rssi,tx_mbps,width_mhz,wifi,mlo,mlo_links
      FROM devices WHERE at=? ORDER BY mac`, atMs)
	if err != nil {
		return snap, err
	}
	defer rows.Close()
	for rows.Next() {
		var d domain.Device
		var online, mlo, links int
		if err = rows.Scan(&d.MAC, &d.Name, &online, &d.SeenVia, &d.IP, &d.APID, &d.Band, &d.RSSI, &d.TXMbps, &d.WidthMHz, &d.WiFi, &mlo, &links); err != nil {
			return snap, err
		}
		d.Online = online == 1
		d.MLO = mlo == 1
		d.MLOLinks = links
		snap.Devices = append(snap.Devices, d)
	}
	rows.Close()
	var topology string
	err = s.db.QueryRowContext(ctx, `SELECT after_value FROM changes WHERE kind='topology' AND at<=? ORDER BY at DESC, id DESC LIMIT 1`, ms(at)).Scan(&topology)
	if err == nil {
		snap.Topology = json.RawMessage(topology)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return snap, err
	}
	return snap, nil
}

// Maintain deletes expired rows.
func (s *SQLite) Maintain(ctx context.Context, now time.Time) error {
	raw := now.AddDate(0, 0, -s.detailDays).UnixMilli()
	summary := now.AddDate(0, 0, -s.summaryDays).UnixMilli()
	for _, table := range []string{"devices", "device_traffic", "traffic", "events", "ping", "cycles"} {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM `+table+` WHERE at<?`, raw); err != nil {
			return errors.New("历史明细清理失败")
		}
	}
	for _, table := range []string{"device_hourly", "device_traffic_hourly", "profile_hourly", "radio_hourly", "traffic_hourly", "ping_hourly"} {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM `+table+` WHERE hour<?`, summary); err != nil {
			return errors.New("历史汇总清理失败")
		}
	}
	return nil
}

// Import seeds traffic points and events (used by the one-off migration tool).
func (s *SQLite) Import(ctx context.Context, seed history.Seed) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range seed.Traffic {
		if _, err = tx.ExecContext(ctx, `INSERT INTO traffic(at,down_mbps,up_mbps,down_peak,up_peak) VALUES(?,?,?,?,?) ON CONFLICT(at) DO NOTHING`,
			ms(t.Time), t.DownMbps, t.UpMbps, t.DownPeak, t.UpPeak); err != nil {
			return err
		}
	}
	for _, e := range seed.Events {
		he := history.Event{Time: e.Time, MAC: e.MAC, Name: e.Name, Text: e.Text, Kind: "association"}
		he.ID = eventID(he)
		if _, err = tx.ExecContext(ctx, `INSERT INTO events(id,at,kind,mac,name,profile_id,text) VALUES(?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`,
			he.ID, ms(e.Time), he.Kind, e.MAC, e.Name, he.ProfileID, e.Text); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Uptime aggregates the confirmed share of hourly device samples.
func (s *SQLite) Uptime(ctx context.Context, from, to time.Time) ([]history.Uptime, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT mac,COALESCE(SUM(n),0),COALESCE(SUM(confirmed),0) FROM device_hourly
      WHERE hour>=? AND hour<? GROUP BY mac ORDER BY mac`, hourMs(from), hourMs(to))
	if err != nil {
		return nil, errors.New("设备在线率读取失败")
	}
	defer rows.Close()
	out := []history.Uptime{}
	for rows.Next() {
		var u history.Uptime
		if err = rows.Scan(&u.MAC, &u.Samples, &u.Confirmed); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// Stats describes the file size and row counts.
func (s *SQLite) Stats(ctx context.Context) (history.Stats, error) {
	st := history.Stats{LargestTable: "history.db"}
	if info, err := os.Stat(s.path); err == nil {
		st.DatabaseBytes = info.Size()
		st.LargestBytes = info.Size()
	}
	_ = s.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM traffic),
      (SELECT COUNT(*) FROM traffic WHERE at > ?), (SELECT COUNT(*) FROM device_hourly)`,
		time.Now().Add(-24*time.Hour).UnixMilli()).Scan(&st.TrafficRows, &st.TrafficRows24h, &st.DeviceHourlyRows)
	return st, nil
}

// ForgetEntity removes a MAC from the ledger so a retired device stops showing
// up in lists; the historical samples themselves are kept.
func (s *SQLite) ForgetEntity(ctx context.Context, mac string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM entities WHERE mac=?`, mac)
	if err != nil {
		return errors.New("台账记录删除失败")
	}
	return nil
}

// LastConfirmed lets the engine restore "last seen online" after a restart.
func (s *SQLite) LastConfirmed(ctx context.Context) (map[string]time.Time, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT mac,last_confirmed_at FROM entities WHERE last_confirmed_at IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var mac string
		var at int64
		if err = rows.Scan(&mac, &at); err != nil {
			return nil, err
		}
		out[mac] = fromMs(at)
	}
	return out, rows.Err()
}

var _ = sort.Strings
