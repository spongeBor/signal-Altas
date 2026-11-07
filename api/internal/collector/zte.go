package collector

import (
	"bytes"
	"context"
	"errors"
	"html"
	"net"
	"regexp"
	"signalatlas/api/internal/domain"
	"strconv"
	"strings"
	"time"
)

type telnet struct {
	conn    net.Conn
	pending []byte
}

func (t *telnet) send(s string) error {
	_ = t.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := t.conn.Write([]byte(s + "\r\n"))
	return err
}
func (t *telnet) read(ctx context.Context, pattern *regexp.Regexp) (string, error) {
	var out bytes.Buffer
	buf := make([]byte, 8192)
	end := time.Now().Add(10 * time.Second)
	for time.Now().Before(end) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		_ = t.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, err := t.conn.Read(buf)
		if err != nil {
			if e, ok := err.(net.Error); ok && e.Timeout() {
				continue
			}
			return "", errors.New("Telnet连接关闭或读取失败")
		}
		t.pending = append(t.pending, buf[:n]...)
		for len(t.pending) > 0 {
			if t.pending[0] != 255 {
				out.WriteByte(t.pending[0])
				t.pending = t.pending[1:]
				continue
			}
			if len(t.pending) < 2 {
				break
			}
			op := t.pending[1]
			if op == 251 || op == 252 || op == 253 || op == 254 {
				if len(t.pending) < 3 {
					break
				}
				if op == 251 || op == 253 {
					reply := byte(254)
					if op == 253 {
						reply = 252
					}
					_, _ = t.conn.Write([]byte{255, reply, t.pending[2]})
				}
				t.pending = t.pending[3:]
			} else if op == 250 {
				i := bytes.Index(t.pending, []byte{255, 240})
				if i < 0 {
					break
				}
				t.pending = t.pending[i+2:]
			} else if op == 255 {
				out.WriteByte(255)
				t.pending = t.pending[2:]
			} else {
				t.pending = t.pending[2:]
			}
		}
		if out.Len() > 1<<20 {
			return "", errors.New("Telnet输出超限")
		}
		s := out.String()
		if pattern.MatchString(s) {
			return s, nil
		}
	}
	return "", errors.New("等待Telnet设备响应超时")
}

var prompt = regexp.MustCompile(`(?im)(?:^|[\r\n])[^\r\n]{0,80}[#$] ?$`)

func (t *telnet) run(ctx context.Context, s string) (string, error) {
	if err := t.send(s); err != nil {
		return "", err
	}
	return t.read(ctx, prompt)
}
func collectZTE(ctx context.Context, p domain.Profile, password string) (map[string]any, error) {
	conn, err := (&net.Dialer{Timeout: 6 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(p.Host, "23"))
	if err != nil {
		return nil, errors.New("无法连接光猫Telnet管理口")
	}
	defer conn.Close()
	t := &telnet{conn: conn}
	if _, err = t.read(ctx, regexp.MustCompile(`(?i)(login|username):\s*$`)); err != nil {
		return nil, err
	}
	if err = t.send(p.Username); err != nil {
		return nil, err
	}
	if _, err = t.read(ctx, regexp.MustCompile(`(?i)password:\s*$`)); err != nil {
		return nil, err
	}
	if err = t.send(password); err != nil {
		return nil, err
	}
	if _, err = t.read(ctx, prompt); err != nil {
		return nil, errors.New("光猫登录失败或未进入管理shell")
	}
	id, err := t.run(ctx, "id")
	if err != nil {
		return nil, err
	}
	if !strings.Contains(id, "uid=0") {
		return nil, errors.New("当前只读采集需要已授权的root管理会话")
	}
	state, err := t.run(ctx, "gpontest -gstate")
	if err != nil {
		return nil, err
	}
	temp, err := t.run(ctx, "cat /proc/tempsensor")
	if err != nil {
		return nil, err
	}
	up, err := t.run(ctx, "cat /proc/uptime")
	if err != nil {
		return nil, err
	}
	fec, err := t.run(ctx, "gpontest -gfeccounters")
	if err != nil {
		return nil, err
	}
	allowed := []struct {
		name string
		keys []string
	}{{"DevInfo", []string{"ModelName", "HardwareVer", "SoftwareVer"}}, {"WANC", []string{"Enable", "ConnType", "VLANID", "Priority", "StrServList"}}, {"TelnetCfg", []string{"Lan_Enable", "Wan_Enable"}}, {"SambaCfg", []string{"EnableSmb"}}, {"MgtServer", []string{"Tr069Enable"}}}
	tables := map[string][]map[string]string{}
	for _, table := range allowed {
		raw, err := t.run(ctx, "sendcmd 1 DB p "+table.name)
		if err != nil {
			return nil, err
		}
		rows := []map[string]string{}
		for _, m := range regexp.MustCompile(`(?s)<Row\b[^>]*>(.*?)</Row>`).FindAllStringSubmatch(raw, -1) {
			row := map[string]string{}
			for _, f := range regexp.MustCompile(`<DM\s+name="([^"]+)"\s+val="([^"]*)"`).FindAllStringSubmatch(m[1], -1) {
				for _, k := range table.keys {
					if k == f[1] {
						row[k] = html.UnescapeString(f[2])
					}
				}
			}
			rows = append(rows, row)
		}
		tables[table.name] = rows
	}
	counters := map[string]uint64{}
	for _, f := range regexp.MustCompile(`FEC\s+(\w+)\s*:\s*(\d+)`).FindAllStringSubmatch(fec, -1) {
		counters[f[1]], _ = strconv.ParseUint(f[2], 10, 64)
	}
	model := "ZTE ONT"
	if len(tables["DevInfo"]) > 0 && tables["DevInfo"][0]["ModelName"] != "" {
		model = tables["DevInfo"][0]["ModelName"]
	}
	firmware := ""
	if len(tables["DevInfo"]) > 0 {
		firmware = tables["DevInfo"][0]["SoftwareVer"]
	}
	optics := collectOpticalModule(ctx, t)
	return map[string]any{"model": model, "firmware": firmware, "optics": optics, "pon_state": match(`state is \[(\w+)\]`, state), "chip_temperature": value(`temper value:\s*(\d+)`, temp), "uptime": value(`(?m)^([\d.]+) `, up), "tables": tables, "fec": counters, "devices": []domain.Device{}, "stations": []domain.Station{}, "radios": []domain.Radio{}}, nil
}
