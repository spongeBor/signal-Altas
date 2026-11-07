package collector

import (
	"context"
	"net"
	"regexp"
	"signalatlas/api/internal/domain"
	"sort"
	"strings"
)

var safeInterface = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)
var macPattern = regexp.MustCompile(`(?i)assoclist\s+([\da-f:]{17})`)
var staMACPattern = regexp.MustCompile(`^[0-9a-f]{2}(:[0-9a-f]{2}){5}$`)
var hexSSIDPattern = regexp.MustCompile(`(?i)^[0-9a-f]{16,64}$`)
var iwChannelPattern = regexp.MustCompile(`channel\s+(\d+)\s*\((\d+)\s*MHz\)`)
var iwChannelOnlyPattern = regexp.MustCompile(`channel\s+(\d+)`)

// physicalInterface maps a Broadcom BSS interface (wl0.1, wl1.2 …) to the radio
// it belongs to (wl0, wl1 …). AiMesh APs expose several BSS per radio: the
// front SSIDs, guest networks and a hidden backhaul, and only summing all of
// them gives the real client count and band.
func physicalInterface(name string) string {
	if i := strings.IndexByte(name, '.'); i > 0 {
		return name[:i]
	}
	return name
}

// parseAPInterfaces returns every AP-mode wireless interface listed by `iw dev`,
// sorted so that each physical radio comes before its BSS (wl0, wl0.1, wl0.2 …).
func parseAPInterfaces(out string) []string {
	res := []string{}
	seen := map[string]bool{}
	blocks := regexp.MustCompile(`(?m)^\s*Interface `).Split(out, -1)
	for _, b := range blocks[1:] {
		f := strings.Fields(b)
		if len(f) == 0 || !safeInterface.MatchString(f[0]) {
			continue
		}
		if !regexp.MustCompile(`\btype AP\b`).MatchString(b) {
			continue
		}
		if seen[f[0]] {
			continue
		}
		seen[f[0]] = true
		res = append(res, f[0])
	}
	sort.Strings(res)
	return res
}

// parseIWInfo pulls the channel, centre frequency and width out of `iw dev X info`.
func parseIWInfo(text string) (channel, frequency *float64, width *int) {
	if m := iwChannelPattern.FindStringSubmatch(text); len(m) == 3 {
		channel = number(m[1])
		frequency = number(m[2])
	} else if m := iwChannelOnlyPattern.FindStringSubmatch(text); len(m) == 2 {
		channel = number(m[1])
	}
	width = integer(match(`width:\s*(\d+)\s*MHz`, text))
	return channel, frequency, width
}

// bandOf prefers the band already read from `wl status`, then the centre
// frequency, and only falls back to channel numbering when both are missing.
// Channel numbers cannot tell 6 GHz from 2.4 GHz/5 GHz, so frequency wins.
func bandOf(text string, frequency, channel *float64) string {
	switch text {
	case "2.4 GHz", "5 GHz", "6 GHz":
		return text
	}
	if frequency != nil {
		switch {
		case *frequency >= 5925:
			return "6 GHz"
		case *frequency >= 2500:
			return "5 GHz"
		case *frequency > 0:
			return "2.4 GHz"
		}
	}
	if channel != nil {
		switch {
		case *channel <= 14:
			return "2.4 GHz"
		case *channel < 200:
			return "5 GHz"
		}
	}
	return ""
}

// visibleSSID reports whether an SSID is a readable name rather than the hex
// blob AiMesh uses for its hidden backhaul.
// betterRate reports whether a is a higher negotiated rate than b.
func betterRate(a, b *float64) bool {
	if a == nil {
		return false
	}
	return b == nil || *a > *b
}

func visibleSSID(ssid string) bool {
	return ssid != "" && !hexSSIDPattern.MatchString(ssid)
}

type asusRadioGroup struct {
	physical string
	radio    domain.Radio
	hasBase  bool
	band     string
	ssid     string
	ssidHits int
	clients  int
	bss      map[string]int
}

func collectASUS(ctx context.Context, p domain.Profile, password, keys string) (map[string]any, error) {
	r, err := connectSSH(ctx, p, password, keys)
	if err != nil {
		return nil, err
	}
	defer r.close()
	model, err := r.run(ctx, "nvram get productid")
	if err != nil {
		return nil, err
	}
	meta, err := r.run(ctx, "nvram get firmver; nvram get buildno; cat /proc/uptime; nvram get lan_hwaddr")
	if err != nil {
		return nil, err
	}
	dev, err := r.run(ctx, "iw dev")
	if err != nil {
		dev = ""
	}
	ifaces := parseAPInterfaces(dev)
	// `iw dev` is authoritative but firmware specific; make sure the physical
	// radios from nvram are present even when the netlink listing misses them.
	optional := map[string]bool{}
	if raw, err := r.run(ctx, "nvram get wl0_ifname; nvram get wl1_ifname; nvram get wl2_ifname"); err == nil {
		known := map[string]bool{}
		for _, x := range ifaces {
			known[x] = true
		}
		for _, x := range strings.Fields(raw) {
			if !safeInterface.MatchString(x) || known[x] {
				continue
			}
			known[x] = true
			optional[x] = true
			ifaces = append(ifaces, x)
		}
		sort.Strings(ifaces)
	}
	if len(ifaces) > 16 {
		ifaces = ifaces[:16]
	}

	groups := map[string]*asusRadioGroup{}
	order := []string{}
	stations := []domain.Station{}
	seen := map[string]int{}
	for _, iface := range ifaces {
		status, err := r.run(ctx, "wl -i "+iface+" status")
		if err != nil {
			if optional[iface] {
				continue
			}
			return nil, err
		}
		radio := ParseStatus(status)
		radio.Interface = iface
		info, err := r.run(ctx, "iw dev "+iface+" info")
		if err != nil {
			info = ""
		}
		channel, frequency, width := parseIWInfo(info)
		if channel != nil {
			radio.Channel = channel
		}
		if width != nil {
			radio.WidthMHz = width
		}
		if band := bandOf(radio.Band, frequency, channel); band != "" {
			radio.Band = band
		}
		if radio.SSID == "" {
			radio.SSID = match(`\bssid\s+([^\n]+)`, info)
		}
		dump, err := r.run(ctx, "iw dev "+iface+" station dump")
		if err != nil {
			dump = ""
		}
		st := ParseIWStations(dump)
		// Broadcom firmware may print only the bandwidth on iw's bitrate line,
		// so take the negotiated rate from `wl sta_info` whenever it is present.
		if bulk, err := r.run(ctx, "wl -i "+iface+" sta_info all"); err == nil {
			rates := ParseWLStationsAll(bulk)
			for i := range st {
				w, ok := rates[st[i].MAC]
				if !ok {
					continue
				}
				// Multi-link clients are listed under their per-link MAC but own
				// the IP through their MLD address, so report the MLD here.
				if w.MAC != "" {
					st[i].MAC = w.MAC
				}
				if w.TXMbps != nil {
					st[i].TXMbps = w.TXMbps
				}
				if w.RXMbps != nil {
					st[i].RXMbps = w.RXMbps
				}
				if st[i].WidthMHz == nil && w.WidthMHz != nil {
					st[i].WidthMHz = w.WidthMHz
				}
				if st[i].RSSI == nil && w.RSSI != nil {
					st[i].RSSI = w.RSSI
				}
				if st[i].WiFi == "" && w.WiFi != "" {
					st[i].WiFi = w.WiFi
				}
				if w.MLO {
					st[i].MLO = true
					st[i].MLOLinks = w.MLOLinks
				}
			}
		}
		if len(st) == 0 {
			if assoc, err := r.run(ctx, "wl -i "+iface+" assoclist"); err == nil {
				for _, m := range macPattern.FindAllStringSubmatch(assoc, 48) {
					mac := strings.ToLower(m[1])
					if !staMACPattern.MatchString(mac) {
						continue
					}
					detail, err := r.run(ctx, "wl -i "+iface+" sta_info "+mac+"; wl -i "+iface+" rssi "+mac)
					if err != nil {
						continue
					}
					s := ParseWLStation(detail)
					s.MAC = mac
					st = append(st, s)
				}
			}
		}

		physical := physicalInterface(iface)
		g, ok := groups[physical]
		if !ok {
			g = &asusRadioGroup{physical: physical}
			groups[physical] = g
			order = append(order, physical)
		}
		if iface == physical || !g.hasBase {
			g.radio = radio
			g.hasBase = true
		}
		if g.band == "" && radio.Band != "" {
			g.band = radio.Band
		}
		if visibleSSID(radio.SSID) {
			if g.bss == nil {
				g.bss = map[string]int{}
			}
			g.bss[radio.SSID] += len(st)
			if g.ssid == "" || len(st) > g.ssidHits {
				g.ssid = radio.SSID
				g.ssidHits = len(st)
			}
		}
		for _, s := range st {
			s.APID = p.ID
			s.Interface = iface
			s.Band = radio.Band
			s.SSID = radio.SSID
			if index, ok := seen[s.MAC]; ok {
				// The same multi-link client can appear on two radios; keep the
				// faster link so the device shows its real negotiated rate.
				if betterRate(s.TXMbps, stations[index].TXMbps) {
					stations[index] = s
				}
				continue
			}
			seen[s.MAC] = len(stations)
			stations = append(stations, s)
			g.clients++
		}
	}

	radios := []domain.Radio{}
	for _, physical := range order {
		g := groups[physical]
		radio := g.radio
		radio.Interface = physical
		radio.APID = p.ID
		radio.APName = p.Name
		if g.band != "" {
			radio.Band = g.band
		}
		if g.ssid != "" {
			radio.SSID = g.ssid
		}
		if len(g.bss) > 0 {
			for ssid, n := range g.bss {
				radio.BSS = append(radio.BSS, domain.BSS{SSID: ssid, Clients: n})
			}
			sort.Slice(radio.BSS, func(i, j int) bool { return radio.BSS[i].Clients > radio.BSS[j].Clients })
		} else if hexSSIDPattern.MatchString(radio.SSID) {
			radio.SSID = ""
		}
		radio.Clients = g.clients
		radios = append(radios, radio)
	}

	settings, err := r.run(ctx, "nvram get wl0_user_rssi; nvram get wl1_user_rssi; nvram get wl0_atf; nvram get wl1_atf; nvram get location_code")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(settings), "\n")
	v := func(i int) string {
		if i < len(lines) {
			return lines[i]
		}
		return ""
	}
	ml := strings.Split(strings.TrimSpace(meta), "\n")
	firmware := ""
	var up *float64
	if len(ml) >= 3 {
		firmware = ml[0] + " / " + ml[1]
		up = value(`^([\d.]+)`, ml[2])
	}
	self := []domain.Device{}
	if len(ml) >= 4 {
		if mac, err := net.ParseMAC(strings.TrimSpace(ml[3])); err == nil && len(mac) == 6 {
			self = append(self, domain.Device{MAC: strings.ToLower(mac.String()), IP: p.Host, Name: p.Name, Online: true, SeenVia: "SSH管理口实时读取"})
		}
	}
	return map[string]any{"model": strings.TrimSpace(model), "firmware": firmware, "uptime": up, "radios": radios, "stations": stations, "devices": self, "settings": map[string]any{"roaming_thresholds": []string{v(0), v(1)}, "airtime_fairness": []string{v(2), v(3)}, "region_code": v(4)}}, nil
}
