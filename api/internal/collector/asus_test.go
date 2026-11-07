package collector

import "testing"

func TestParseAPInterfacesKeepsEveryBand(t *testing.T) {
	raw := `phy#0
	Interface wl0.2
		ifindex 12
		wdev 0x5
		addr 60:cf:84:2f:b0:e0
		ssid home-2.4
		type AP
		channel 3 (2422 MHz), width: 40 MHz
	phy#0
	Interface wl0
		ifindex 10
		addr 60:cf:84:2f:b0:e1
		ssid home
		type AP
	phy#1
	Interface wl1.1
		ifindex 22
		addr 60:cf:84:2f:b0:e2
		ssid home
		type AP
		channel 36 (5180 MHz), width: 80 MHz
	phy#1
	Interface wl1
		ifindex 20
		addr 60:cf:84:2f:b0:e3
		ssid home
		type AP
	phy#2
	Interface eth0
		ifindex 3
		type Ethernet
`
	got := parseAPInterfaces(raw)
	want := []string{"wl0", "wl0.2", "wl1", "wl1.1"}
	if len(got) != len(want) {
		t.Fatalf("interfaces = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("interfaces = %v, want %v", got, want)
		}
	}
}

func TestPhysicalInterfaceStripsBSSIndex(t *testing.T) {
	cases := map[string]string{"wl0": "wl0", "wl0.1": "wl0", "wl1.4": "wl1", "wl2": "wl2", "": ""}
	for in, want := range cases {
		if got := physicalInterface(in); got != want {
			t.Errorf("physicalInterface(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseIWInfoReadsFrequency(t *testing.T) {
	channel, frequency, width := parseIWInfo("Interface wl1\n\tchannel 36 (5180 MHz), width: 80 MHz, center1: 5210 MHz\n")
	if channel == nil || *channel != 36 || frequency == nil || *frequency != 5180 || width == nil || *width != 80 {
		t.Fatalf("parsed channel=%v frequency=%v width=%v", channel, frequency, width)
	}
}

func TestBandOfSeparatesSixGHzAndFallsBackToChannel(t *testing.T) {
	channel := 5.0
	freq24 := 2437.0
	freq5 := 5180.0
	freq6 := 6135.0
	cases := []struct {
		text string
		freq *float64
		ch   *float64
		want string
	}{
		{"5 GHz", &freq24, &channel, "5 GHz"},
		{"", &freq6, &channel, "6 GHz"},
		{"", &freq5, &channel, "5 GHz"},
		{"", &freq24, &channel, "2.4 GHz"},
		{"", nil, &channel, "2.4 GHz"},
		{"", nil, nil, ""},
	}
	for _, c := range cases {
		if got := bandOf(c.text, c.freq, c.ch); got != c.want {
			t.Errorf("bandOf(%q, freq=%v) = %q, want %q", c.text, c.freq, got, c.want)
		}
	}
}

func TestVisibleSSIDSkipsBackhaulHex(t *testing.T) {
	if visibleSSID("9DB7D57371E8A6EAB38DCFC31C1A97CA") {
		t.Fatal("hex AiMesh backhaul SSID must not be shown as the radio name")
	}
	if !visibleSSID("home-2.4") {
		t.Fatal("normal SSID must be visible")
	}
}
