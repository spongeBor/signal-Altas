// Package wanmeter samples the WAN interface byte counters over SNMP and keeps a
// short-window peak. The regular collection cycle reads the same counters once
// per thirty seconds, so a fifteen second speed test is averaged down to roughly
// half its real rate; polling every couple of seconds keeps the peak the user
// actually reached.
package wanmeter

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// CounterSource reads the 64-bit octet counters of one interface.
type CounterSource interface {
	Resolve(name string) (int, error)
	Counters(index int) (in, out uint64, err error)
}

// Snapshot is one reading together with the peak of the configured window.
type Snapshot struct {
	Time         time.Time `json:"time"`
	DownMbps     float64   `json:"down_mbps"`
	UpMbps       float64   `json:"up_mbps"`
	PeakDownMbps float64   `json:"peak_down_mbps"`
	PeakUpMbps   float64   `json:"peak_up_mbps"`
	WindowSec    int       `json:"window_seconds"`
	Interface    string    `json:"interface"`
	Error        string    `json:"error,omitempty"`
}

type point struct {
	at   time.Time
	down float64
	up   float64
}

// Meter polls a counter source and keeps the maximum rate seen in the window.
type Meter struct {
	src      CounterSource
	ifName   string
	interval time.Duration
	window   time.Duration
	now      func() time.Time
	idle     time.Duration

	mu      sync.Mutex
	index   int
	ready   bool
	prevIn  uint64
	prevOut uint64
	prevAt  time.Time
	points  []point
	last    *Snapshot
}

func New(src CounterSource, ifName string, interval, window time.Duration) *Meter {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	if window < interval {
		window = 60 * time.Second
	}
	return &Meter{src: src, ifName: ifName, interval: interval, window: window, now: time.Now, idle: 5 * time.Second}
}

// Snapshot returns the latest reading, or nil before the first interval is known.
func (m *Meter) Snapshot() *Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.last == nil {
		return nil
	}
	copied := *m.last
	return &copied
}

// Run polls until ctx is cancelled.
func (m *Meter) Run(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	m.poll()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.poll()
		}
	}
}

func (m *Meter) poll() {
	if !m.ready {
		index, err := m.src.Resolve(m.ifName)
		if err != nil {
			m.fail(err)
			return
		}
		if m.index != index {
			// A recreated interface starts its counters from a different value;
			// treat the next sample as a new baseline instead of a huge burst.
			m.prevAt = time.Time{}
		}
		m.index, m.ready = index, true
	}
	in, out, err := m.src.Counters(m.index)
	if err != nil {
		// The interface may have been recreated by a PPPoE reconnect; resolve
		// the index again on the next tick instead of pinning the old one.
		m.ready = false
		m.fail(err)
		return
	}
	now := m.now()
	if m.prevAt.IsZero() || in < m.prevIn || out < m.prevOut {
		m.prevIn, m.prevOut, m.prevAt = in, out, now
		return
	}
	if in == m.prevIn && out == m.prevOut {
		// snmpd caches the interface table for a few seconds, so the counter can
		// sit still for several polls and then jump by the whole interval. Keep
		// the baseline until the counter really is idle; measuring the jump over
		// the elapsed time (not the poll interval) is what keeps the peak real.
		// The agent cache refreshes every few seconds, so wait well past that
		// before calling the link idle; a shorter window turns one delayed
		// refresh into a huge rate.
		idle := m.idle
		if idle < 2*m.interval {
			idle = 2 * m.interval
		}
		if now.Sub(m.prevAt) < idle {
			return
		}
		m.prevAt = now
		m.publish(0, 0, now)
		return
	}
	seconds := now.Sub(m.prevAt).Seconds()
	if seconds <= 0 {
		return
	}
	down := float64(in-m.prevIn) * 8 / seconds / 1e6
	up := float64(out-m.prevOut) * 8 / seconds / 1e6
	m.prevIn, m.prevOut, m.prevAt = in, out, now
	m.publish(down, up, now)
}

// publish records one reading and recomputes the window peak.
func (m *Meter) publish(down, up float64, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.points = append(m.points, point{at: now, down: down, up: up})
	cutoff := now.Add(-m.window)
	for len(m.points) > 1 && !m.points[0].at.After(cutoff) {
		m.points = m.points[1:]
	}
	peakDown, peakUp := 0.0, 0.0
	for _, p := range m.points {
		if p.down > peakDown {
			peakDown = p.down
		}
		if p.up > peakUp {
			peakUp = p.up
		}
	}
	m.last = &Snapshot{
		Time:         now,
		DownMbps:     down,
		UpMbps:       up,
		PeakDownMbps: peakDown,
		PeakUpMbps:   peakUp,
		WindowSec:    int(m.window.Seconds()),
		Interface:    m.ifName,
	}
}

// fail keeps the previous readings visible while recording why the latest poll
// did not produce one.
func (m *Meter) fail(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.last == nil {
		m.last = &Snapshot{Time: m.now(), Interface: m.ifName, WindowSec: int(m.window.Seconds())}
	}
	m.last.Error = fmt.Sprintf("SNMP 采样失败：%s", err)
}
