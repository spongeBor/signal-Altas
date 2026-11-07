package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// MigrateFromPostgres copies the documents and every history table from an
// existing PostgreSQL deployment into a fresh SQLite file. It is a one-off
// tool: the target must be empty (or absent), and the source is only read.
func MigrateFromPostgres(ctx context.Context, pgDSN, sqlitePath string, progress func(string)) (map[string]int64, error) {
	store, err := NewSQLite(sqlitePath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	conn, err := pgx.Connect(ctx, pgDSN)
	if err != nil {
		return nil, fmt.Errorf("连接 PostgreSQL 失败：%w", err)
	}
	defer conn.Close(ctx)
	if _, err = store.db.Exec(`PRAGMA synchronous=OFF; PRAGMA journal_mode=MEMORY;`); err != nil {
		return nil, err
	}
	defer store.db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL;`) //nolint:errcheck

	counts := map[string]int64{}
	migrate := func(name, query, insert string, scan func(pgx.Rows) ([]any, error)) error {
		tx, err := store.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback() //nolint:errcheck
		stmt, err := tx.PrepareContext(ctx, insert)
		if err != nil {
			return err
		}
		defer stmt.Close()
		rows, err := conn.Query(ctx, query)
		if err != nil {
			return fmt.Errorf("%s 读取失败：%w", name, err)
		}
		defer rows.Close()
		n := 0
		for rows.Next() {
			args, err := scan(rows)
			if err != nil {
				return err
			}
			if _, err = stmt.ExecContext(ctx, args...); err != nil {
				return fmt.Errorf("%s 写入失败：%w", name, err)
			}
			n++
			if n%20000 == 0 {
				if err = stmt.Close(); err != nil {
					return err
				}
				if err = tx.Commit(); err != nil {
					return err
				}
				tx, err = store.db.BeginTx(ctx, nil)
				if err != nil {
					return err
				}
				stmt, err = tx.PrepareContext(ctx, insert)
				if err != nil {
					return err
				}
			}
		}
		if err = rows.Err(); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		counts[name] = int64(n)
		if progress != nil {
			progress(fmt.Sprintf("%s: %d", name, n))
		}
		return nil
	}

	type pair struct {
		name, query, insert string
		scan                func(pgx.Rows) ([]any, error)
	}
	text := func(v *sql.NullString) string {
		if v != nil && v.Valid {
			return v.String
		}
		return ""
	}
	_ = text
	columns := []pair{
		{"documents", `SELECT key, value::text FROM signal_atlas.documents`,
			`INSERT OR REPLACE INTO documents(key,value) VALUES(?,?)`,
			func(r pgx.Rows) ([]any, error) {
				var k, v string
				if err := r.Scan(&k, &v); err != nil {
					return nil, err
				}
				return []any{k, v}, nil
			}},
		{"cycles", `SELECT at, profiles::text, radios::text FROM signal_atlas.history_cycles ORDER BY at`,
			`INSERT OR REPLACE INTO cycles(at,profiles,radios) VALUES(?,?,?)`,
			func(r pgx.Rows) ([]any, error) {
				var at time.Time
				var p, rd string
				if err := r.Scan(&at, &p, &rd); err != nil {
					return nil, err
				}
				return []any{ms(at), p, rd}, nil
			}},
		{"devices", `SELECT at,mac,name,online,seen_via,ip,ap_id,band,rssi,tx_mbps,width_mhz FROM signal_atlas.history_devices ORDER BY at`,
			`INSERT OR REPLACE INTO devices(at,mac,name,online,seen_via,ip,ap_id,band,rssi,tx_mbps,width_mhz) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			func(r pgx.Rows) ([]any, error) {
				var at time.Time
				var mac, name, seen, ip, apid, band string
				var online bool
				var rssi, tx *float64
				var width *int
				if err := r.Scan(&at, &mac, &name, &online, &seen, &ip, &apid, &band, &rssi, &tx, &width); err != nil {
					return nil, err
				}
				return []any{ms(at), mac, name, boolInt(online), seen, ip, apid, band, rssi, tx, width}, nil
			}},
		{"traffic", `SELECT at,down_mbps,up_mbps,down_peak_mbps,up_peak_mbps FROM signal_atlas.history_traffic ORDER BY at`,
			`INSERT OR REPLACE INTO traffic(at,down_mbps,up_mbps,down_peak,up_peak) VALUES(?,?,?,?,?)`,
			func(r pgx.Rows) ([]any, error) {
				var at time.Time
				var d, u float64
				var dp, up *float64
				if err := r.Scan(&at, &d, &u, &dp, &up); err != nil {
					return nil, err
				}
				return []any{ms(at), d, u, dp, up}, nil
			}},
		{"device_traffic", `SELECT at,mac,name,COALESCE(host(ip),''),down_bytes,up_bytes,down_mbps,up_mbps FROM signal_atlas.history_device_traffic ORDER BY at`,
			`INSERT OR REPLACE INTO device_traffic(at,mac,name,ip,down_bytes,up_bytes,down_mbps,up_mbps) VALUES(?,?,?,?,?,?,?,?)`,
			func(r pgx.Rows) ([]any, error) {
				var at time.Time
				var mac, name, ip string
				var db, ub int64
				var dm, um float64
				if err := r.Scan(&at, &mac, &name, &ip, &db, &ub, &dm, &um); err != nil {
					return nil, err
				}
				return []any{ms(at), mac, name, ip, db, ub, dm, um}, nil
			}},
		{"events", `SELECT id,at,kind,mac,name,profile_id,text FROM signal_atlas.history_events ORDER BY at`,
			`INSERT OR REPLACE INTO events(id,at,kind,mac,name,profile_id,text) VALUES(?,?,?,?,?,?,?)`,
			func(r pgx.Rows) ([]any, error) {
				var id, kind, mac, name, pid, txt string
				var at time.Time
				if err := r.Scan(&id, &at, &kind, &mac, &name, &pid, &txt); err != nil {
					return nil, err
				}
				return []any{id, ms(at), kind, mac, name, pid, txt}, nil
			}},
		{"changes", `SELECT id,at,kind,subject,before_value::text,after_value::text FROM signal_atlas.history_changes ORDER BY at`,
			`INSERT OR REPLACE INTO changes(id,at,kind,subject,before_value,after_value) VALUES(?,?,?,?,?,?)`,
			func(r pgx.Rows) ([]any, error) {
				var id int64
				var at time.Time
				var kind, subject string
				var before, after *string
				if err := r.Scan(&id, &at, &kind, &subject, &before, &after); err != nil {
					return nil, err
				}
				return []any{id, ms(at), kind, subject, before, after}, nil
			}},
		{"entities", `SELECT mac,name,ip,last_observed_at,last_confirmed_at FROM signal_atlas.history_entities`,
			`INSERT OR REPLACE INTO entities(mac,name,ip,last_observed_at,last_confirmed_at) VALUES(?,?,?,?,?)`,
			func(r pgx.Rows) ([]any, error) {
				var mac, name, ip string
				var observed time.Time
				var confirmed *time.Time
				if err := r.Scan(&mac, &name, &ip, &observed, &confirmed); err != nil {
					return nil, err
				}
				var c any
				if confirmed != nil {
					c = ms(*confirmed)
				}
				return []any{mac, name, ip, ms(observed), c}, nil
			}},
		{"traffic_hourly", `SELECT hour,n,down_sum,up_sum,down_peak,up_peak FROM signal_atlas.history_traffic_hourly ORDER BY hour`,
			`INSERT OR REPLACE INTO traffic_hourly(hour,n,down_sum,up_sum,down_peak,up_peak) VALUES(?,?,?,?,?,?)`,
			func(r pgx.Rows) ([]any, error) {
				var hour time.Time
				var n int64
				var ds, us, dp, up float64
				if err := r.Scan(&hour, &n, &ds, &us, &dp, &up); err != nil {
					return nil, err
				}
				return []any{ms(hour), n, ds, us, dp, up}, nil
			}},
		{"device_hourly", `SELECT hour,mac,name,n,confirmed,rssi_sum,rssi_n,rssi_min,rssi_max,tx_sum,tx_n,tx_peak,to_json(aps)::text,to_json(bands)::text FROM signal_atlas.history_device_hourly ORDER BY hour`,
			`INSERT OR REPLACE INTO device_hourly(hour,mac,name,n,confirmed,rssi_sum,rssi_n,rssi_min,rssi_max,tx_sum,tx_n,tx_peak,aps,bands) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			func(r pgx.Rows) ([]any, error) {
				var hour time.Time
				var mac, name, aps, bands string
				var n, confirmed, rssiN, txN int64
				var rssiSum, txSum float64
				var rssiMin, rssiMax, txPeak *float64
				if err := r.Scan(&hour, &mac, &name, &n, &confirmed, &rssiSum, &rssiN, &rssiMin, &rssiMax, &txSum, &txN, &txPeak, &aps, &bands); err != nil {
					return nil, err
				}
				return []any{ms(hour), mac, name, n, confirmed, rssiSum, rssiN, rssiMin, rssiMax, txSum, txN, txPeak, aps, bands}, nil
			}},
		{"device_traffic_hourly", `SELECT hour,mac,name,down_bytes,up_bytes,down_peak,up_peak,samples FROM signal_atlas.history_device_traffic_hourly ORDER BY hour`,
			`INSERT OR REPLACE INTO device_traffic_hourly(hour,mac,name,down_bytes,up_bytes,down_peak,up_peak,samples) VALUES(?,?,?,?,?,?,?,?)`,
			func(r pgx.Rows) ([]any, error) {
				var hour time.Time
				var mac, name string
				var db, ub, samples int64
				var dp, up float64
				if err := r.Scan(&hour, &mac, &name, &db, &ub, &dp, &up, &samples); err != nil {
					return nil, err
				}
				return []any{ms(hour), mac, name, db, ub, dp, up, samples}, nil
			}},
		{"profile_hourly", `SELECT hour,profile_id,name,n,ok,cpu_sum,cpu_n,cpu_peak,temp_sum,temp_n,temp_peak FROM signal_atlas.history_profile_hourly ORDER BY hour`,
			`INSERT OR REPLACE INTO profile_hourly(hour,profile_id,name,n,ok,cpu_sum,cpu_n,cpu_peak,temp_sum,temp_n,temp_peak) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			func(r pgx.Rows) ([]any, error) {
				var hour time.Time
				var id, name string
				var n, ok, cn, tn int64
				var cs, ts float64
				var cp, tp *float64
				if err := r.Scan(&hour, &id, &name, &n, &ok, &cs, &cn, &cp, &ts, &tn, &tp); err != nil {
					return nil, err
				}
				return []any{ms(hour), id, name, n, ok, cs, cn, cp, ts, tn, tp}, nil
			}},
		{"radio_hourly", `SELECT hour,entity,ap_id,interface,band,ssid,n,util_sum,util_n,util_peak,clients_sum,clients_peak FROM signal_atlas.history_radio_hourly ORDER BY hour`,
			`INSERT OR REPLACE INTO radio_hourly(hour,entity,ap_id,interface,band,ssid,n,util_sum,util_n,util_peak,clients_sum,clients_peak) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			func(r pgx.Rows) ([]any, error) {
				var hour time.Time
				var entity, apid, iface, band, ssid string
				var n, utilN, clientsSum, clientsPeak int64
				var utilSum float64
				var utilPeak *float64
				if err := r.Scan(&hour, &entity, &apid, &iface, &band, &ssid, &n, &utilSum, &utilN, &utilPeak, &clientsSum, &clientsPeak); err != nil {
					return nil, err
				}
				return []any{ms(hour), entity, apid, iface, band, ssid, n, utilSum, utilN, utilPeak, clientsSum, clientsPeak}, nil
			}},
		{"ping", `SELECT at,target,host,sent,recv,rtt_avg,rtt_min,rtt_max FROM signal_atlas.history_ping ORDER BY at`,
			`INSERT OR REPLACE INTO ping(at,target,host,sent,recv,rtt_avg,rtt_min,rtt_max) VALUES(?,?,?,?,?,?,?,?)`,
			func(r pgx.Rows) ([]any, error) {
				var at time.Time
				var target, host string
				var sent, recv int
				var avg, min, max *float64
				if err := r.Scan(&at, &target, &host, &sent, &recv, &avg, &min, &max); err != nil {
					return nil, err
				}
				return []any{ms(at), target, host, sent, recv, avg, min, max}, nil
			}},
		{"ping_hourly", `SELECT hour,target,host,sent,recv,rtt_sum,rtt_n,rtt_peak FROM signal_atlas.history_ping_hourly ORDER BY hour`,
			`INSERT OR REPLACE INTO ping_hourly(hour,target,host,sent,recv,rtt_sum,rtt_n,rtt_peak) VALUES(?,?,?,?,?,?,?,?)`,
			func(r pgx.Rows) ([]any, error) {
				var hour time.Time
				var target, host string
				var sent, recv, n int64
				var sum float64
				var peak *float64
				if err := r.Scan(&hour, &target, &host, &sent, &recv, &sum, &n, &peak); err != nil {
					return nil, err
				}
				return []any{ms(hour), target, host, sent, recv, sum, n, peak}, nil
			}},
	}
	for _, c := range columns {
		if err := migrate(c.name, c.query, c.insert, c.scan); err != nil {
			return counts, err
		}
	}
	return counts, nil
}
