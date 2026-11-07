package wanmeter

import (
	"errors"
	"testing"
	"time"
)

type fakeSource struct {
	index      int
	in, out    uint64
	resolveErr error
	counterErr error
}

func (f *fakeSource) Resolve(string) (int, error) {
	if f.resolveErr != nil {
		return 0, f.resolveErr
	}
	return f.index, nil
}

func (f *fakeSource) Counters(int) (uint64, uint64, error) {
	if f.counterErr != nil {
		return 0, 0, f.counterErr
	}
	return f.in, f.out, nil
}

func newTestMeter(src CounterSource, clock *time.Time) *Meter {
	m := New(src, "pppoe-wan", time.Second, 4*time.Second)
	m.now = func() time.Time { return *clock }
	m.idle = 2 * time.Second
	return m
}

func TestMeterKeepsTheShortWindowPeak(t *testing.T) {
	src := &fakeSource{index: 19, in: 1_000_000}
	clock := time.Date(2026, 11, 6, 9, 30, 0, 0, time.UTC)
	m := newTestMeter(src, &clock)

	m.poll() // baseline only
	if m.Snapshot() != nil {
		t.Fatal("first poll must only record the baseline")
	}

	clock = clock.Add(2 * time.Second)
	src.in += 100_000_000 // 100 MB in 2 s = 400 Mbps
	m.poll()
	snap := m.Snapshot()
	if snap == nil || snap.DownMbps < 399 || snap.DownMbps > 401 {
		t.Fatalf("down = %+v, want ~400 Mbps", snap)
	}
	if snap.PeakDownMbps < 399 {
		t.Fatalf("peak = %.1f, want the same 400 Mbps", snap.PeakDownMbps)
	}

	clock = clock.Add(2 * time.Second)
	m.poll() // idle interval
	snap = m.Snapshot()
	if snap.DownMbps != 0 {
		t.Fatalf("idle down = %.1f, want 0", snap.DownMbps)
	}
	if snap.PeakDownMbps < 399 {
		t.Fatalf("peak dropped on an idle interval: %.1f", snap.PeakDownMbps)
	}

	clock = clock.Add(2 * time.Second)
	src.in += 250_000_000 // 250 MB in 2 s = 1000 Mbps
	m.poll()
	if got := m.Snapshot().PeakDownMbps; got < 999 {
		t.Fatalf("peak = %.1f, want the 1000 Mbps burst", got)
	}

	clock = clock.Add(6 * time.Second)
	src.in += 0
	m.poll()
	snap = m.Snapshot()
	if snap.PeakDownMbps != 0 {
		t.Fatalf("peak = %.1f, want it to expire with the window", snap.PeakDownMbps)
	}
}

func TestMeterIgnoresCounterReset(t *testing.T) {
	src := &fakeSource{index: 19, in: 5_000_000}
	clock := time.Date(2026, 11, 6, 9, 30, 0, 0, time.UTC)
	m := newTestMeter(src, &clock)
	m.poll()

	clock = clock.Add(2 * time.Second)
	src.in = 10 // PPPoE reconnected and the counter restarted
	m.poll()
	if m.Snapshot() != nil {
		t.Fatal("a counter reset must not become a negative or huge rate")
	}

	clock = clock.Add(2 * time.Second)
	src.in += 25_000_000 // 25 MB in 2 s = 100 Mbps
	m.poll()
	snap := m.Snapshot()
	if snap == nil || snap.DownMbps < 99 || snap.DownMbps > 101 {
		t.Fatalf("down after reset = %+v, want ~100 Mbps", snap)
	}
}

func TestMeterReportsSNMPFailureWithoutLosingTheLastReading(t *testing.T) {
	src := &fakeSource{index: 19, in: 1_000_000}
	clock := time.Date(2026, 11, 6, 9, 30, 0, 0, time.UTC)
	m := newTestMeter(src, &clock)
	m.poll()
	clock = clock.Add(2 * time.Second)
	src.in += 50_000_000
	m.poll()

	src.counterErr = errors.New("timeout")
	clock = clock.Add(2 * time.Second)
	m.poll()
	snap := m.Snapshot()
	if snap == nil || snap.Error == "" {
		t.Fatal("failure must be surfaced on the snapshot")
	}
	if snap.DownMbps <= 0 {
		t.Fatal("the previous reading must stay visible")
	}
	if m.Snapshot().PeakDownMbps <= 0 {
		t.Fatal("peak must survive a transient failure")
	}
}

func TestMeterDoesNotDoubleRateAcrossALazyCounterRefresh(t *testing.T) {
	src := &fakeSource{index: 19, in: 1_000_000}
	clock := time.Date(2026, 11, 7, 0, 30, 0, 0, time.UTC)
	m := New(src, "pppoe-wan", 2*time.Second, 60*time.Second)
	m.now = func() time.Time { return clock }
	m.poll() // baseline

	// 1 Gbps for 3.5 s, but snmpd only refreshes the counter between polls.
	clock = clock.Add(2 * time.Second)
	m.poll()
	clock = clock.Add(2 * time.Second)
	src.in += 437_500_000 // 3.5 s at 1 Gbps
	m.poll()

	snap := m.Snapshot()
	if snap == nil {
		t.Fatal("no snapshot")
	}
	if snap.DownMbps > 1100 {
		t.Fatalf("down = %.0f Mbps, want ≈875 (not the doubled 1750)", snap.DownMbps)
	}
	if snap.PeakDownMbps > 1100 {
		t.Fatalf("peak = %.0f Mbps, want no artificial spike", snap.PeakDownMbps)
	}
}
