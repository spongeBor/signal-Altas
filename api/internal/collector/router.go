package collector

import (
	"context"
	"encoding/json"
	"net"
	"regexp"
	"signalatlas/api/internal/domain"
	"strconv"
	"strings"
	"time"
)

var qosCounterLine = regexp.MustCompile(`(?m)^\s*ip (?:daddr|saddr) (\S+) counter packets \d+ bytes (\d+)`)

// isPrivateIP reports whether the WAN address is a carrier-grade NAT or LAN
// address, which is worth calling out instead of showing it as a public IP.
func isPrivateIP(raw string) bool {
	ip := net.ParseIP(raw)
	if ip == nil || ip.To4() == nil {
		return false
	}
	v := ip.To4()
	switch {
	case v[0] == 10:
		return true
	case v[0] == 172 && v[1] >= 16 && v[1] <= 31:
		return true
	case v[0] == 192 && v[1] == 168:
		return true
	case v[0] == 100 && v[1] >= 64 && v[1] <= 127:
		return true
	case v[0] == 169 && v[1] == 254:
		return true
	}
	return false
}
func parseQoSCounters(raw string) map[string]uint64 {
	out := map[string]uint64{}
	for _, m := range qosCounterLine.FindAllStringSubmatch(raw, -1) {
		if bytes, err := strconv.ParseUint(m[2], 10, 64); err == nil {
			out[m[1]] = bytes
		}
	}
	return out
}

func collectRouter(ctx context.Context, p domain.Profile, password, keys string) (map[string]any, error) {
	r, err := connectSSH(ctx, p, password, keys)
	if err != nil {
		return nil, err
	}
	defer r.close()
	cmd := "ubus call system.board; printf '\\n__WAN__\\n'; ubus call network.interface.wan status; printf '\\n__NEIGH__\\n'; ip neigh show dev br-lan; printf '\\n__LEASE__\\n'; cat /tmp/dhcp.leases; printf '\\n__MEM__\\n'; cat /proc/meminfo; printf '\\n__UP__\\n'; cat /proc/uptime; printf '\\n__NET__\\n'; cat /proc/net/dev; printf '\\n__LINK__\\n'; cat /sys/class/net/eth0/speed; cat /sys/class/net/eth1/speed; printf '\\n__QOS_DOWN__\\n'; nft list chain inet nft-qos-monitor download 2>/dev/null; printf '\\n__QOS_UP__\\n'; nft list chain inet nft-qos-monitor upload 2>/dev/null"
	raw, err := r.run(ctx, cmd)
	if err != nil {
		return nil, err
	}
	marks := regexp.MustCompile(`\n__(\w+)__\n`).FindAllStringSubmatchIndex(raw, -1)
	parts := map[string]string{}
	boardRaw := raw
	for i, m := range marks {
		end := len(raw)
		if i+1 < len(marks) {
			end = marks[i+1][0]
		}
		parts[raw[m[2]:m[3]]] = raw[m[1]:end]
		if i == 0 {
			boardRaw = raw[:m[0]]
		}
	}
	board := map[string]any{}
	wan := map[string]any{}
	_ = json.Unmarshal([]byte(boardRaw), &board)
	_ = json.Unmarshal([]byte(parts["WAN"]), &wan)
	devices := map[string]*domain.Device{}
	for _, line := range strings.Split(parts["LEASE"], "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		name := f[3]
		if name == "*" {
			name = ""
		}
		mac := strings.ToLower(f[1])
		devices[mac] = &domain.Device{MAC: mac, IP: f[2], Name: name, SeenVia: "DHCP租约", Addresses: []string{f[2]}}
	}
	for _, line := range strings.Split(parts["NEIGH"], "\n") {
		m := regexp.MustCompile(`^(\S+).*?lladdr\s+(\S+)\s+(\w+)`).FindStringSubmatch(line)
		if len(m) < 4 {
			continue
		}
		ip, mac, status := m[1], strings.ToLower(m[2]), m[3]
		d := devices[mac]
		if d == nil {
			d = &domain.Device{MAC: mac}
			devices[mac] = d
		}
		found := false
		for _, a := range d.Addresses {
			if a == ip {
				found = true
			}
		}
		if !found {
			d.Addresses = append(d.Addresses, ip)
		}
		if d.IP == "" || !strings.Contains(ip, ":") {
			d.IP = ip
			d.NeighborState = status
		}
		if status == "REACHABLE" {
			d.Online = true
			d.SeenVia = "当前邻居REACHABLE"
		}
	}
	list := []domain.Device{}
	for _, d := range devices {
		list = append(list, *d)
	}
	mem := map[string]uint64{}
	for _, m := range regexp.MustCompile(`(MemTotal|MemAvailable):\s*(\d+)`).FindAllStringSubmatch(parts["MEM"], -1) {
		mem[m[1]], _ = strconv.ParseUint(m[2], 10, 64)
	}
	counters := map[string][]uint64{}
	for _, line := range strings.Split(parts["NET"], "\n") {
		f := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(f) != 2 {
			continue
		}
		nums := strings.Fields(f[1])
		if len(nums) < 16 {
			continue
		}
		values := []uint64{}
		for _, n := range nums {
			x, _ := strconv.ParseUint(n, 10, 64)
			values = append(values, x)
		}
		counters[strings.TrimSpace(f[0])] = values
	}
	qos := map[string]map[string]uint64{"down": parseQoSCounters(parts["QOS_DOWN"]), "up": parseQoSCounters(parts["QOS_UP"])}
	// A 600 ms window treated any sub-second burst (a provider refresh, the
	// collector's own command batch) as sustained load. Two seconds averages
	// those out while still showing real sustained CPU.
	const cpuWindow = 2 * time.Second
	cpu0, err := r.run(ctx, "head -n 1 /proc/stat")
	if err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(cpuWindow):
	}
	cpu1, err := r.run(ctx, "head -n 1 /proc/stat")
	if err != nil {
		return nil, err
	}
	parse := func(s string) []float64 {
		a := []float64{}
		f := strings.Fields(s)
		if len(f) == 0 {
			return a
		}
		if len(f) > 9 {
			f = f[:9]
		}
		for _, x := range f[1:] {
			v, _ := strconv.ParseFloat(x, 64)
			a = append(a, v)
		}
		return a
	}
	a, b := parse(cpu0), parse(cpu1)
	cpu := 0.0
	if len(a) >= 5 && len(a) == len(b) {
		total := 0.0
		for i := range a {
			total += b[i] - a[i]
		}
		if total > 0 {
			cpu = 100 * (total - (b[3] - a[3]) - (b[4] - a[4])) / total
		}
	}
	link := []float64{}
	for _, x := range strings.Fields(parts["LINK"]) {
		if n := number(x); n != nil {
			link = append(link, *n)
		}
	}
	firmware := ""
	if rel, ok := board["release"].(map[string]any); ok {
		firmware, _ = rel["description"].(string)
	}
	model, _ := board["model"].(string)
	if model == "" {
		model = "OpenWrt"
	}
	wanIP, wanCGNAT := "", false
	if list, ok := wan["ipv4-address"].([]any); ok && len(list) > 0 {
		if addr, ok := list[0].(map[string]any); ok {
			wanIP, _ = addr["address"].(string)
			wanCGNAT = isPrivateIP(wanIP)
		}
	}
	return map[string]any{"model": model, "firmware": firmware, "wan_up": wan["up"], "wan_uptime": wan["uptime"], "wan_device": wan["l3_device"], "wan_ip": wanIP, "wan_cgnat": wanCGNAT, "cpu_percent": cpu, "memory": mem, "uptime": value(`^([\d.]+)`, parts["UP"]), "devices": list, "counters": counters, "qos_counters": qos, "link_speeds": link, "stations": []domain.Station{}, "radios": []domain.Radio{}}, nil
}
