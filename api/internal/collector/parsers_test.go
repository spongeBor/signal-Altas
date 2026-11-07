package collector

import "testing"

func TestParseWirelessNegotiation(t *testing.T) {
	r := ParseStatus("SSID: \"home\"\nChanspec: 5GHz channel 36 80MHz (0xe02a)\nQBSS channel utilization: 90 (35 %)\n")
	if r.SSID != "home" || r.Band != "5 GHz" || r.Channel == nil || *r.Channel != 36 || r.WidthMHz == nil || *r.WidthMHz != 80 {
		t.Fatalf("wrong radio: %+v", r)
	}
	s := ParseWLStation("link bandwidth = 80 MHz\nrate of last tx pkt: 1201000 kbps\nrate of last rx pkt: 866.7 Mbps\n-47\n")
	if s.RSSI == nil || *s.RSSI != -47 || s.WidthMHz == nil || *s.WidthMHz != 80 || s.TXMbps == nil || *s.TXMbps != 1201 || s.RXMbps == nil || *s.RXMbps != 866.7 {
		t.Fatalf("wrong station: %+v", s)
	}
}
func TestIWStationBlocksAndMissingMetrics(t *testing.T) {
	rows := ParseIWStations("Station AA:BB:CC:DD:EE:FF (on wlan1)\n signal: -63 [-63] dBm\n tx bitrate: 1201.0 MBit/s 80MHz\n rx bitrate: 960.8 MBit/s\n tx failed: 12\nStation 11:22:33:44:55:66 (on wlan1)\n inactive time: 2 ms\n")
	if len(rows) != 2 || rows[0].MAC != "aa:bb:cc:dd:ee:ff" || rows[0].RSSI == nil || *rows[0].RSSI != -63 || rows[1].RSSI != nil || rows[1].TXMbps != nil {
		t.Fatalf("unexpected station blocks: %+v", rows)
	}
	if ParseStatus("").WidthMHz != nil || ParseWLStation("not available").RSSI != nil {
		t.Fatal("missing values must remain unknown")
	}
}

func TestParseIWStationsIgnoresBandwidthOnlyBitrate(t *testing.T) {
	rows := ParseIWStations("Station ea:10:c4:6d:a5:50 (on wl1.1)\n\tsignal:  \t-62 [-63] dBm\n\ttx bitrate:\t 160MHz\n\trx bitrate:\t 160MHz\n\ttx failed:\t44\n")
	if len(rows) != 1 {
		t.Fatalf("rows = %d", len(rows))
	}
	if rows[0].TXMbps != nil || rows[0].RXMbps != nil {
		t.Fatalf("a bandwidth string must not become a rate: %+v", rows[0])
	}
	if rows[0].WidthMHz == nil || *rows[0].WidthMHz != 160 {
		t.Fatalf("width = %v, want 160", rows[0].WidthMHz)
	}
}

func TestParseWLStationsAllReadsBroadcomRates(t *testing.T) {
	raw := `[VER 8] STA EA:10:C4:6D:A5:50:
	 chanspec 44/160 (0xea32)
	 idle 0 seconds
	 rate of last tx pkt: 1814790 kbps - 1088880 kbps
	 rate of last rx pkt: 1729410 kbps
	 per antenna rssi of last rx data frame: -65 -65 -67 -66
[VER 8] STA B8:A0:B8:14:2C:39:
	 chanspec 44/80 (0xe22a)
	 rate of last tx pkt: 1201000 kbps
	 rate of last rx pkt: 866700 kbps
	 per antenna rssi of last rx data frame: -66 -62
`
	rows := ParseWLStationsAll(raw)
	iphone, ok := rows["ea:10:c4:6d:a5:50"]
	if !ok {
		t.Fatal("missing iphone station")
	}
	if iphone.TXMbps == nil || *iphone.TXMbps < 1800 || *iphone.TXMbps > 1820 {
		t.Fatalf("tx = %v, want ~1814.79 Mbps", iphone.TXMbps)
	}
	if iphone.WidthMHz == nil || *iphone.WidthMHz != 160 {
		t.Fatalf("width = %v", iphone.WidthMHz)
	}
	if iphone.RSSI == nil || *iphone.RSSI != -65 {
		t.Fatalf("rssi = %v", iphone.RSSI)
	}
	if rows["b8:a0:b8:14:2c:39"].TXMbps == nil || *rows["b8:a0:b8:14:2c:39"].TXMbps < 1200 || *rows["b8:a0:b8:14:2c:39"].TXMbps > 1202 {
		t.Fatalf("second station tx = %v", rows["b8:a0:b8:14:2c:39"].TXMbps)
	}
}

func TestParseWLStationsAllMapsMLOLinkToMLD(t *testing.T) {
	raw := `[VER 8] STA 82:D3:3D:91:15:8E:
	 chanspec 44/160 (0xea32)
	 state: AUTHENTICATED ASSOCIATED AUTHORIZED
	 flags 0xc0d1e13b: BRCM WME PS N_CAP EHT_CAP MLO_CAP
	 MLO: peer mld address : 1C:3C:78:20:A8:30
	 rate of last tx pkt: 2401910 kbps - 1441170 kbps
	 rate of last rx pkt: 286760 kbps
	 smoothed rssi: -36
`
	rows := ParseWLStationsAll(raw)
	link, ok := rows["82:d3:3d:91:15:8e"]
	if !ok || link.MAC != "1c:3c:78:20:a8:30" {
		t.Fatalf("link station must be reported under its MLD: %+v", link)
	}
	mld, ok := rows["1c:3c:78:20:a8:30"]
	if !ok {
		t.Fatal("MLD address must be a lookup key")
	}
	if mld.TXMbps == nil || *mld.TXMbps < 2401 || *mld.TXMbps > 2402 {
		t.Fatalf("tx = %v, want ~2401.91 Mbps", mld.TXMbps)
	}
	if mld.WidthMHz == nil || *mld.WidthMHz != 160 {
		t.Fatalf("width = %v", mld.WidthMHz)
	}
	if mld.WiFi != "Wi-Fi 7" {
		t.Fatalf("wifi = %q, want Wi-Fi 7", mld.WiFi)
	}
}

func TestParseWLStationDetectsMLOLinks(t *testing.T) {
	text := `[VER 8] STA 82:D3:3D:91:15:8E:
	 flags 0xc0d1e13b: BRCM WME PS N_CAP EHT_CAP MLO_CAP
	 MLO: tx nrate:
	 link_id[0]:	eht mcs 11 Nss 2 Tx Exp 0 bw160 ldpc 2xLTF GI 0.8us auto
	 link_id[1]:	eht mcs 9 Nss 2 Tx Exp 0 bw20 2xLTF GI 0.8us auto
	 link_id[2]:	auto
	 MLO: rx nrate:
	 link_id[0]:	legacy rate 24 Mbps stf mode 0 auto
`
	s := ParseWLStation(text)
	if !s.MLO {
		t.Fatal("MLO_CAP not detected")
	}
	if s.MLOLinks != 2 {
		t.Fatalf("links = %d, want 2", s.MLOLinks)
	}
}
