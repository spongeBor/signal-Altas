package collector

import "testing"

func TestParseQoSCounters(t *testing.T) {
	raw := "chain download {\n  ip daddr 192.168.1.211 counter packets 69526 bytes 67691754 # handle 71\n  ip daddr 192.168.1.114 counter packets 3 bytes 42 # handle 57\n}"
	got := parseQoSCounters(raw)
	if got["192.168.1.211"] != 67691754 || got["192.168.1.114"] != 42 || len(got) != 2 {
		t.Fatalf("unexpected counters: %#v", got)
	}
}
