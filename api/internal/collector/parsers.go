package collector

import (
	"regexp"
	"signalatlas/api/internal/domain"
	"strconv"
	"strings"
)

func number(s string) *float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return nil
	}
	return &f
}
func integer(s string) *int {
	v, err := strconv.Atoi(s)
	if err != nil {
		return nil
	}
	return &v
}
func match(pattern, text string) string {
	x := regexp.MustCompile(pattern).FindStringSubmatch(text)
	if len(x) > 1 {
		return x[1]
	}
	return ""
}
func value(pattern, text string) *float64 { return number(match(pattern, text)) }
func ParseStatus(text string) domain.Radio {
	r := domain.Radio{SSID: match(`SSID:\s*"([^"\n]*)"`, text), Channel: value(`Chanspec:\s*.*?channel\s+(\d+)`, text), WidthMHz: integer(match(`Chanspec:[^\n]*?\s(20|40|80|160)MHz`, text)), Utilization: value(`QBSS[^\n]*\((\d+)\s*%\)`, text)}
	if strings.Contains(text, "2.4GHz") {
		r.Band = "2.4 GHz"
	} else if strings.Contains(text, "6GHz") {
		r.Band = "6 GHz"
	} else if strings.Contains(text, "5GHz") {
		r.Band = "5 GHz"
	}
	return r
}
func ParseIWStations(text string) []domain.Station {
	rows := []domain.Station{}
	for _, b := range regexp.MustCompile(`(?m)^Station `).Split(text, -1)[1:] {
		f := strings.Fields(b)
		if len(f) == 0 {
			continue
		}
		// Some Broadcom firmware prints only the bandwidth on the bitrate line
		// ("tx bitrate: 160MHz") instead of a rate, so a bare number is not a
		// rate unless it carries the MBit/s unit.
		rows = append(rows, domain.Station{MAC: strings.ToLower(f[0]), RSSI: value(`(?m)^\s*signal:\s*(-?\d+)`, b), TXMbps: value(`tx bitrate:\s*([\d.]+)\s*[GM]?Bit\/s`, b), RXMbps: value(`rx bitrate:\s*([\d.]+)\s*[GM]?Bit\/s`, b), WidthMHz: integer(match(`(?:tx|rx) bitrate:[^\n]*?\b(20|40|80|160|320)MHz`, b)), TXFailed: value(`tx failed:\s*(\d+)`, b)})
	}
	return rows
}

// wlStaBlock marks one station in `wl sta_info all` output.
var wlStaBlock = regexp.MustCompile(`(?m)^\[VER \d+\] STA ([0-9A-Fa-f:]{17}):`)

// ParseWLStationsAll reads Broadcom's bulk `wl sta_info all` output. This is the
// only reliable rate source on firmware whose iw station dump prints bandwidth
// instead of a bitrate.
func ParseWLStationsAll(text string) map[string]domain.Station {
	out := map[string]domain.Station{}
	marks := wlStaBlock.FindAllStringSubmatchIndex(text, -1)
	for i, m := range marks {
		mac := strings.ToLower(text[m[2]:m[3]])
		end := len(text)
		if i+1 < len(marks) {
			end = marks[i+1][0]
		}
		block := text[m[1]:end]
		station := ParseWLStation(block)
		station.MAC = mac
		if station.WidthMHz == nil {
			station.WidthMHz = integer(match(`chanspec \d+/(\d+)`, block))
		}
		if station.RSSI == nil {
			station.RSSI = value(`(?i)(?:per antenna rssi of last rx data frame|smoothed rssi):\s*(-?\d+)`, block)
		}
		// A Wi-Fi 7 multi-link client is listed under its per-link MAC, but its
		// DHCP/ARP identity is the MLD address; index both so callers can match a
		// station to the device that owns the IP.
		mld := strings.ToLower(match(`(?i)MLO: peer mld address\s*:\s*([0-9A-Fa-f:]{17})`, block))
		if mld != "" {
			alias := station
			alias.MAC = mld
			out[mac] = alias
			out[mld] = alias
			continue
		}
		out[mac] = station
	}
	return out
}

func withMAC(s domain.Station, mac string) domain.Station {
	s.MAC = mac
	return s
}

// wifiGeneration maps the association capability flags to the 802.11 family.
// EHT is checked before HE and VHT because those substrings appear inside it.
func wifiGeneration(text string) string {
	switch {
	case strings.Contains(text, "EHT_CAP"):
		return "Wi-Fi 7"
	case strings.Contains(text, "HE_CAP"):
		return "Wi-Fi 6"
	case strings.Contains(text, "VHT_CAP"):
		return "Wi-Fi 5"
	case strings.Contains(text, "N_CAP"), strings.Contains(text, "HT_CAP"):
		return "Wi-Fi 4"
	}
	return ""
}

// mloLinks counts the active multi-link legs from the tx nrate block. The rx
// block repeats the same legs, so only the tx section is counted; a leg with
// only "auto" carries no rate and is not in use.
func mloLinks(text string) int {
	i := strings.Index(text, "MLO: tx nrate:")
	if i < 0 {
		return 0
	}
	section := text[i:]
	if j := strings.Index(section, "MLO: rx nrate:"); j >= 0 {
		section = section[:j]
	}
	links := 0
	for _, line := range strings.Split(section, "\n") {
		at := strings.Index(line, "link_id[")
		if at < 0 {
			continue
		}
		rest := strings.TrimSpace(line[strings.Index(line[at:], ":")+at+1:])
		if rest != "" && rest != "auto" {
			links++
		}
	}
	return links
}

func ParseWLStation(text string) domain.Station {
	r := domain.Station{WidthMHz: integer(match(`(?i)link bandwidth\s*=\s*(20|40|80|160)\s*MHZ`, text)), WiFi: wifiGeneration(text)}
	x := regexp.MustCompile(`(?m)^\s*(-\d+)\s*$`).FindAllStringSubmatch(strings.ReplaceAll(text, "\r", ""), -1)
	if len(x) > 0 {
		r.RSSI = number(x[len(x)-1][1])
	}
	rate := func(dir string) *float64 {
		x := regexp.MustCompile(`(?i)rate of last ` + dir + ` pkt:\s*([\d.]+)\s*(kbps|Mbps)?`).FindStringSubmatch(text)
		if len(x) < 2 {
			return nil
		}
		f := number(x[1])
		if f != nil && (len(x) < 3 || !strings.EqualFold(x[2], "Mbps")) {
			*f /= 1000
		}
		return f
	}
	r.TXMbps = rate("tx")
	r.RXMbps = rate("rx")
	r.MLO = strings.Contains(text, "MLO_CAP")
	r.MLOLinks = mloLinks(text)
	return r
}
