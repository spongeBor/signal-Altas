package collector

import (
	"context"
	"regexp"
	"strconv"

	"signalatlas/api/internal/domain"
)

// PingTarget is one vantage point in the latency sampler.
type PingTarget struct {
	Name string
	Host string
}

// DefaultPingTargets cover the three hops that explain a slow or flaky network:
// the gateway (line), the first access point (LAN), and two public resolvers (WAN).
var DefaultPingTargets = []PingTarget{
	{Name: "网关", Host: "192.168.1.1"},
	{Name: "接入点", Host: "192.168.1.2"},
	{Name: "公网·阿里", Host: "223.5.5.5"},
	{Name: "公网·Cloudflare", Host: "1.1.1.1"},
}

var (
	pingCounts = regexp.MustCompile(`(\d+) packets transmitted, (\d+) (?:packets )?received`)
	pingRTT    = regexp.MustCompile(`(?:round-trip|rtt) min/avg/max\s*=\s*([\d.]+)/([\d.]+)/([\d.]+)`)
)

// ParsePing reads the busybox/iputils summary lines.
func ParsePing(text string) domain.PingResult {
	r := domain.PingResult{}
	if m := pingCounts.FindStringSubmatch(text); len(m) == 3 {
		r.Sent, _ = strconv.Atoi(m[1])
		r.Recv, _ = strconv.Atoi(m[2])
	}
	if m := pingRTT.FindStringSubmatch(text); len(m) == 4 {
		r.Min, _ = strconv.ParseFloat(m[1], 64)
		r.Avg, _ = strconv.ParseFloat(m[2], 64)
		r.Max, _ = strconv.ParseFloat(m[3], 64)
	}
	return r
}

// PingTargets runs the probes from the router, which is the only vantage point
// that sees the ONT and the AP without crossing the Wi-Fi link twice.
func PingTargets(ctx context.Context, p domain.Profile, password, knownHosts string, targets []PingTarget) ([]domain.PingResult, error) {
	r, err := connectSSH(ctx, p, password, knownHosts)
	if err != nil {
		return nil, err
	}
	out := make([]domain.PingResult, 0, len(targets))
	for _, t := range targets {
		res := domain.PingResult{Target: t.Name, Host: t.Host, Sent: 2}
		text, err := r.run(ctx, "ping -c 2 -W 1 "+t.Host)
		if err == nil {
			parsed := ParsePing(text)
			parsed.Target, parsed.Host = t.Name, t.Host
			if parsed.Sent == 0 {
				parsed.Sent = 2
			}
			res = parsed
		}
		out = append(out, res)
	}
	return out, nil
}
