package service

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"signalatlas/api/internal/collector"
	"signalatlas/api/internal/domain"
	"signalatlas/api/internal/history"
	"signalatlas/api/internal/repository"
	"sort"
	"strings"
	"sync"
	"time"
)

type Engine struct {
	mu                  sync.RWMutex
	repo                repository.Documents
	dataDir, knownHosts string
	config              domain.Config
	secrets             map[string]string
	secretsStore        *secretStore
	labels              map[string]domain.Label
	topology            json.RawMessage
	state               domain.State
	wake                chan struct{}
	previous            map[string]domain.Device
	confirmed           map[string]time.Time
	lastCounters        map[string]counter
	lastDeviceCounters  map[string]deviceCounter
	archive             history.Store
	archiveSaved        *time.Time
	archiveError        string
	lastMaintenance     time.Time
	wanPeak             func() (down, up float64, ok bool)
	wanIP               string
	pingTargets         []collector.PingTarget
	lanCIDR             string
	startedAt           time.Time
}
type counter struct {
	at     time.Time
	rx, tx uint64
}
type deviceCounter struct {
	at       time.Time
	ip       string
	down, up uint64
}

// minCounterWindow is the shortest interval a byte-counter difference may be
// turned into a rate over. Collection runs on a 30 second cadence, so a shorter
// gap means two cycles landed back to back (retry, manual refresh) rather than
// a new measurement. Dividing a whole window of bytes by such a gap reports an
// inflated rate and shows up later as a fake traffic spike.
const minCounterWindow = 5 * time.Second

// deviceTrafficDelta turns two router counter readings into one interval
// sample. It refuses to produce a value when a baseline is missing, the
// address changed, the clock moved backwards, or a counter reset, so a
// restart never becomes a false spike.
func deviceTrafficDelta(prev deviceCounter, found bool, next deviceCounter) (domain.DeviceTraffic, bool) {
	if !found || prev.ip != next.ip || !next.at.After(prev.at) || next.down < prev.down || next.up < prev.up {
		return domain.DeviceTraffic{}, false
	}
	seconds := next.at.Sub(prev.at).Seconds()
	if seconds <= 0 {
		return domain.DeviceTraffic{}, false
	}
	return domain.DeviceTraffic{
		Time:      next.at,
		DownBytes: next.down - prev.down,
		UpBytes:   next.up - prev.up,
		DownMbps:  float64(next.down-prev.down) * 8 / seconds / 1e6,
		UpMbps:    float64(next.up-prev.up) * 8 / seconds / 1e6,
	}, true
}

// tooSoon reports whether two counter readings sit so close together that a
// rate derived from them would describe the collection loop rather than the
// traffic. Callers keep the older baseline in that case so the bytes are
// counted over the real elapsed window instead of being dropped or inflated.
func tooSoon(prev time.Time, found bool, next time.Time) bool {
	return found && next.After(prev) && next.Sub(prev) < minCounterWindow
}

func load[T any](ctx context.Context, r repository.Documents, dir, key string, fallback T) (T, error) {
	v := fallback
	err := r.Read(ctx, key, &v)
	if err == nil {
		return v, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return v, fmt.Errorf("读取%s失败：%w", key, err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, key+".json")); err == nil {
		if err = json.Unmarshal(b, &v); err != nil {
			return v, fmt.Errorf("本地%s文档格式无效", key)
		}
	}
	if err = r.Write(ctx, key, v); err != nil {
		return v, err
	}
	return v, nil
}
func New(ctx context.Context, r repository.Documents, dir, knownHosts string) (*Engine, error) {
	e := &Engine{repo: r, dataDir: dir, knownHosts: knownHosts, startedAt: time.Now(), secrets: map[string]string{}, wake: make(chan struct{}, 1), previous: map[string]domain.Device{}, confirmed: map[string]time.Time{}, lastCounters: map[string]counter{}, lastDeviceCounters: map[string]deviceCounter{}}
	if store, ok := r.(interface {
		LastConfirmed(context.Context) (map[string]time.Time, error)
	}); ok {
		if times, err := store.LastConfirmed(ctx); err == nil {
			e.confirmed = times
		}
	}
	var err error
	if err = e.restoreSecrets(); err != nil {
		return nil, err
	}
	if e.config, err = load(ctx, r, dir, "config", domain.Config{Profiles: []domain.Profile{}, Interval: 30}); err != nil {
		return nil, err
	}
	if err = ValidateProfiles(e.config.Profiles); err != nil {
		return nil, err
	}
	if e.config.Interval < 20 {
		e.config.Interval = 30
	}
	if e.labels, err = load(ctx, r, dir, "labels", map[string]domain.Label{}); err != nil {
		return nil, err
	}
	if e.topology, err = load(ctx, r, dir, "topology", json.RawMessage("null")); err != nil {
		return nil, err
	}
	if e.labels == nil {
		e.labels = map[string]domain.Label{}
	}
	e.state = domain.State{Profiles: []domain.Result{}, Devices: []domain.Device{}, Radios: []domain.Radio{}, History: []domain.TrafficPoint{}, Events: []domain.Event{}}
	e.archive, _ = r.(history.Store)
	return e, nil
}

// restoreSecrets keeps the old in-memory-only behaviour unless the deployment
// supplies a key. With a key, the encrypted file on the private data volume
// restores passwords after a container rebuild instead of silently starting
// with none, and a file without its key is reported rather than ignored.
func (e *Engine) restoreSecrets() error {
	path := filepath.Join(e.dataDir, "secrets.enc")
	key := os.Getenv("SIGNAL_ATLAS_SECRET_KEY")
	if key == "" {
		if _, err := os.Stat(path); err == nil {
			return errors.New("检测到加密凭据文件，但未配置SIGNAL_ATLAS_SECRET_KEY")
		}
		return nil
	}
	store, err := newSecretStore(path, key)
	if err != nil {
		return err
	}
	secrets, err := store.load()
	if err != nil {
		return err
	}
	e.secretsStore = store
	e.secrets = secrets
	return nil
}

func (e *Engine) saveSecrets(snapshot map[string]string) error {
	if e.secretsStore == nil {
		return nil
	}
	if err := e.secretsStore.save(snapshot); err != nil {
		return fmt.Errorf("凭据已生效但未能保存，重启后需重新输入：%w", err)
	}
	return nil
}

func ValidateProfiles(profiles []domain.Profile) error {
	if len(profiles) > 16 {
		return errors.New("最多支持16个采集连接")
	}
	ids := map[string]bool{}
	for _, p := range profiles {
		if !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`).MatchString(p.ID) || ids[p.ID] {
			return errors.New("设备ID无效或重复")
		}
		ids[p.ID] = true
		if len(p.Name) == 0 || len(p.Name) > 120 || len(p.Username) > 100 || len(p.Parent) > 64 {
			return errors.New("设备名称或管理用户名无效")
		}
		ip := net.ParseIP(p.Host)
		if ip == nil || !ip.IsPrivate() || ip.IsLoopback() {
			return errors.New("管理地址必须是内网IP地址")
		}
		switch p.Adapter {
		case "openwrt", "asus", "zte", "esxi", "tcp", "demo":
		default:
			return errors.New("不支持的设备适配器")
		}
		if p.CertificateSHA256 != "" && !regexp.MustCompile(`^(?:[0-9a-fA-F]{64}|[0-9a-fA-F]{2}(?::[0-9a-fA-F]{2}){31})$`).MatchString(p.CertificateSHA256) {
			return errors.New("证书SHA256指纹应为64位十六进制字符")
		}
	}
	return nil
}
func (e *Engine) Run(ctx context.Context) {
	for {
		e.Cycle(ctx)
		e.mu.RLock()
		interval := e.config.Interval
		e.mu.RUnlock()
		timer := time.NewTimer(time.Duration(interval) * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-e.wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}
func (e *Engine) Refresh() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

// wanAddress returns the PPPoE address reported by the gateway this cycle.
func wanAddress(results []domain.Result) string {
	for _, r := range results {
		if r.Adapter != "openwrt" || r.Status != "ok" {
			continue
		}
		if v, ok := r.Data["wan_ip"].(string); ok {
			return v
		}
	}
	return ""
}

// samplePing probes the ONT, AP and public resolvers from the router, reusing
// the SSH session the collector already keeps open.
func (e *Engine) samplePing(ctx context.Context, results []domain.Result, secrets map[string]string) []domain.PingResult {
	for _, r := range results {
		if r.Status == "ok" {
			if ping, ok := r.Data["ping"].([]domain.PingResult); ok && len(ping) > 0 {
				return ping
			}
		}
	}
	for _, r := range results {
		if r.Adapter != "openwrt" || r.Status != "ok" {
			continue
		}
		pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		e.mu.RLock()
		targets := e.pingTargets
		e.mu.RUnlock()
		if len(targets) == 0 {
			targets = collector.DefaultPingTargets
		}
		if ping, err := collector.PingTargets(pingCtx, r.Profile, secrets[r.ID], e.knownHosts, targets); err == nil {
			return ping
		}
		return nil
	}
	return nil
}
func (e *Engine) Cycle(parent context.Context) {
	e.mu.Lock()
	if e.state.Collecting {
		e.mu.Unlock()
		return
	}
	e.state.Collecting = true
	profiles := append([]domain.Profile{}, e.config.Profiles...)
	secrets := map[string]string{}
	for k, v := range e.secrets {
		secrets[k] = v
	}
	labels := map[string]domain.Label{}
	for k, v := range e.labels {
		labels[k] = v
	}
	e.mu.Unlock()
	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()
	results := make([]domain.Result, len(profiles))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, p := range profiles {
		wg.Add(1)
		go func(i int, p domain.Profile) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[i] = domain.Result{Profile: p, Status: "error", Error: "本轮采集超时", CollectedAt: time.Now(), Data: map[string]any{}}
				return
			}
			defer func() { <-sem }()
			results[i] = collector.Collect(ctx, p, secrets[p.ID], e.knownHosts)
		}(i, p)
	}
	wg.Wait()
	ping := e.samplePing(ctx, results, secrets)
	devices := map[string]domain.Device{}
	radios := []domain.Radio{}
	at := time.Now()
	for _, p := range results {
		if p.Status != "ok" {
			continue
		}
		if list, ok := p.Data["devices"].([]domain.Device); ok {
			for _, d := range list {
				d.CollectedAt = p.CollectedAt
				devices[d.MAC] = d
			}
		}
		if list, ok := p.Data["radios"].([]domain.Radio); ok {
			radios = append(radios, list...)
		}
	}
	for _, p := range results {
		if p.Status != "ok" {
			continue
		}
		if list, ok := p.Data["stations"].([]domain.Station); ok {
			for _, s := range list {
				d := devices[s.MAC]
				d.MAC = s.MAC
				d.APID = p.ID
				d.APName = p.Name
				d.Interface = s.Interface
				d.SSID = s.SSID
				d.Band = s.Band
				d.RSSI = s.RSSI
				d.TXMbps = s.TXMbps
				d.RXMbps = s.RXMbps
				d.WidthMHz = s.WidthMHz
				d.WiFi = s.WiFi
				d.MLO = s.MLO
				d.MLOLinks = s.MLOLinks
				d.Online = true
				d.SeenVia = "AP实时关联"
				d.CollectedAt = p.CollectedAt
				devices[s.MAC] = d
			}
		}
	}
	for mac, l := range labels {
		d, ok := devices[mac]
		if !ok {
			d = domain.Device{MAC: mac, IP: l.IP, SeenVia: "台账记录（本轮未确认连接）"}
		}
		if l.Name != "" {
			d.Name = l.Name
			d.LabelSource = "本地台账"
		}
		d.Category = l.Category
		d.PhysicalID = l.PhysicalID
		devices[mac] = d
	}
	// A tcp probe only proves that the management page answers; mark the
	// matching ledger device online so the switch is not shown as offline just
	// because it has no DHCP lease or wireless association.
	for _, r := range results {
		if r.Adapter != "tcp" || r.Status != "ok" {
			continue
		}
		for mac, d := range devices {
			if d.IP != r.Profile.Host || d.Online {
				continue
			}
			d.Online = true
			d.SeenVia = "管理口应答"
			d.CollectedAt = r.CollectedAt
			devices[mac] = d
		}
	}
	// Reuse the router's existing nft-qos-monitor counters. The collector never
	// creates rules or clears counters: it only reads the one rule per known IP
	// that the router already maintains.
	e.mu.Lock()
	for _, r := range results {
		if r.Adapter != "openwrt" || r.Status != "ok" {
			continue
		}
		qos, ok := r.Data["qos_counters"].(map[string]map[string]uint64)
		if !ok {
			continue
		}
		for mac, d := range devices {
			if d.IP == "" {
				continue
			}
			down, downOK := qos["down"][d.IP]
			up, upOK := qos["up"][d.IP]
			if !downOK && !upOK {
				continue
			}
			key := r.ID + "/" + mac
			prev, found := e.lastDeviceCounters[key]
			next := deviceCounter{at: r.CollectedAt, ip: d.IP, down: down, up: up}
			if next.ip == prev.ip && tooSoon(prev.at, found, next.at) {
				continue
			}
			e.lastDeviceCounters[key] = next
			sample, valid := deviceTrafficDelta(prev, found, next)
			if !valid {
				continue
			}
			d.Traffic = &sample
			devices[mac] = d
		}
		break
	}
	list := []domain.Device{}
	for _, d := range devices {
		if d.Name == "" {
			d.Name = "未命名设备 · " + d.MAC[max(0, len(d.MAC)-5):]
		}
		list = append(list, d)
	}
	for i := range list {
		if list[i].Online {
			e.confirmed[list[i].MAC] = at
		}
		if seen, ok := e.confirmed[list[i].MAC]; ok {
			moment := seen
			list[i].LastConfirmedAt = &moment
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Online != list[j].Online {
			return list[i].Online
		}
		return list[i].Name < list[j].Name
	})
	events := append([]domain.Event{}, e.state.Events...)
	if ip := wanAddress(results); ip != "" {
		if e.wanIP != "" && e.wanIP != ip {
			events = append([]domain.Event{{Time: at, Text: "WAN 地址变化 " + e.wanIP + " → " + ip}}, events...)
		}
		e.wanIP = ip
	}
	for _, d := range list {
		if old, ok := e.previous[d.MAC]; ok && old.APID != "" && d.APID != "" && old.APID != d.APID {
			events = append([]domain.Event{{Time: at, MAC: d.MAC, Name: d.Name, Text: old.APName + " → " + d.APName}}, events...)
		}
		e.previous[d.MAC] = d
	}
	if len(events) > 100 {
		events = events[:100]
	}
	var traffic *domain.TrafficPoint
	history := append([]domain.TrafficPoint{}, e.state.History...)
	for _, r := range results {
		if r.Status != "ok" {
			continue
		}
		if point, ok := r.Data["traffic"].(*domain.TrafficPoint); ok && point != nil {
			traffic = point
			history = append(history, *traffic)
			break
		}
	}
	for _, r := range results {
		if r.Adapter != "openwrt" || r.Status != "ok" {
			continue
		}
		counters, ok := r.Data["counters"].(map[string][]uint64)
		if !ok {
			continue
		}
		dev, _ := r.Data["wan_device"].(string)
		v := counters[dev]
		if len(v) < 9 {
			continue
		}
		prev, found := e.lastCounters[r.ID]
		next := counter{at: r.CollectedAt, rx: v[0], tx: v[8]}
		if tooSoon(prev.at, found, next.at) {
			// Two cycles landed back to back; keeping the older baseline counts
			// those bytes over the real window instead of inventing a spike.
			break
		}
		e.lastCounters[r.ID] = next
		if found && next.at.After(prev.at) && next.rx >= prev.rx && next.tx >= prev.tx {
			dt := next.at.Sub(prev.at).Seconds()
			traffic = &domain.TrafficPoint{Time: r.CollectedAt, DownMbps: float64(next.rx-prev.rx) * 8 / dt / 1e6, UpMbps: float64(next.tx-prev.tx) * 8 / dt / 1e6}
			if e.wanPeak != nil {
				if downPeak, upPeak, ok := e.wanPeak(); ok {
					// A rolling peak can never be below the interval average.
					if downPeak < traffic.DownMbps {
						downPeak = traffic.DownMbps
					}
					if upPeak < traffic.UpMbps {
						upPeak = traffic.UpMbps
					}
					traffic.DownPeak, traffic.UpPeak = &downPeak, &upPeak
				}
			}
			history = append(history, *traffic)
		}
		break
	}
	if len(history) > 180 {
		history = history[len(history)-180:]
	}
	e.state = domain.State{LastUpdated: at, Profiles: results, Devices: list, Radios: radios, History: history, Events: events, Traffic: traffic, Ping: ping, Collecting: false}
	observation := e.state
	e.mu.Unlock()
	e.saveHistory(parent, observation)
}
func (e *Engine) View() map[string]any {
	e.mu.RLock()
	defer e.mu.RUnlock()
	profiles := []domain.ProfileView{}
	for _, p := range e.config.Profiles {
		profiles = append(profiles, domain.ProfileView{Profile: p, HasPassword: e.secrets[p.ID] != ""})
	}
	return map[string]any{"state": e.state, "config": map[string]any{"profiles": profiles, "interval": e.config.Interval}, "credentials": map[string]any{"persisted": e.secretsStore != nil}, "topology": e.topology, "storage": e.repo.Kind(), "archive": map[string]any{"enabled": e.archive != nil, "last_saved": e.archiveSaved, "error": e.archiveError}}
}
func (e *Engine) Export() map[string]any {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return map[string]any{"exported_at": time.Now(), "config": e.config, "state": e.state, "topology": e.topology, "labels": e.labels}
}
func (e *Engine) SetConfig(ctx context.Context, input []domain.ProfileInput, interval int) error {
	profiles := []domain.Profile{}
	for _, p := range input {
		profiles = append(profiles, p.Profile)
	}
	if err := ValidateProfiles(profiles); err != nil {
		return err
	}
	if interval < 20 || interval > 300 {
		return errors.New("采集周期应在20—300秒之间")
	}
	config := domain.Config{Profiles: profiles, Interval: interval}
	if err := e.repo.Write(ctx, "config", config); err != nil {
		return err
	}
	e.mu.Lock()
	e.config = config
	keep := map[string]bool{}
	for _, p := range input {
		keep[p.ID] = true
		if p.ClearPassword {
			delete(e.secrets, p.ID)
		} else if p.Password != "" {
			e.secrets[p.ID] = p.Password
		}
	}
	for id := range e.secrets {
		if !keep[id] {
			delete(e.secrets, id)
		}
	}
	snapshot := make(map[string]string, len(e.secrets))
	for id, v := range e.secrets {
		if v != "" {
			snapshot[id] = v
		}
	}
	e.mu.Unlock()
	err := e.saveSecrets(snapshot)
	e.Refresh()
	return err
}
func (e *Engine) Rename(ctx context.Context, mac, name string) error {
	m, err := net.ParseMAC(mac)
	if err != nil || len(m) != 6 {
		return errors.New("MAC地址格式无效")
	}
	name = strings.TrimSpace(name)
	if len(name) == 0 || len(name) > 160 {
		return errors.New("名称不能为空或过长")
	}
	mac = strings.ToLower(m.String())
	e.mu.Lock()
	defer e.mu.Unlock()
	labels := map[string]domain.Label{}
	for k, v := range e.labels {
		labels[k] = v
	}
	l := labels[mac]
	l.Name = name
	labels[mac] = l
	if err = e.repo.Write(ctx, "labels", labels); err != nil {
		return err
	}
	e.labels = labels
	go e.Refresh()
	return nil
}

// Forget removes a retired device from the ledger: its entity row and its
// label, but not the history samples that prove it existed.
func (e *Engine) Forget(ctx context.Context, mac string) error {
	m, err := net.ParseMAC(mac)
	if err != nil || len(m) != 6 {
		return errors.New("MAC地址格式无效")
	}
	mac = strings.ToLower(m.String())
	e.mu.Lock()
	labels := map[string]domain.Label{}
	for k, v := range e.labels {
		if k == mac {
			continue
		}
		labels[k] = v
	}
	if err = e.repo.Write(ctx, "labels", labels); err != nil {
		e.mu.Unlock()
		return err
	}
	e.labels = labels
	e.mu.Unlock()
	if store, ok := e.repo.(history.Store); ok {
		if err = store.ForgetEntity(ctx, mac); err != nil {
			return err
		}
	}
	go e.Refresh()
	return nil
}

// SetWANPeak wires the 2 s SNMP sampler so history keeps the real peak instead
// of the 30 s average. Call it before Run starts.
func (e *Engine) SetWANPeak(fn func() (down, up float64, ok bool)) {
	e.mu.Lock()
	e.wanPeak = fn
	e.mu.Unlock()
}

// SetPingTargets overrides the latency probes. Call it before Run starts.
func (e *Engine) SetPingTargets(targets []collector.PingTarget) {
	e.mu.Lock()
	e.pingTargets = append([]collector.PingTarget{}, targets...)
	e.mu.Unlock()
}

// SetLANCIDR sets the range used by discovery when the request does not name one.
func (e *Engine) SetLANCIDR(cidr string) {
	e.mu.Lock()
	e.lanCIDR = cidr
	e.mu.Unlock()
}

// PendingHostKeys lists SSH fingerprints waiting for approval.
func (e *Engine) PendingHostKeys() []collector.PendingHostKeyView {
	return collector.PendingHostKeyViews()
}

// TrustHostKey approves a pending fingerprint and writes it to known_hosts.
func (e *Engine) TrustHostKey(host string) error {
	return collector.TrustHostKey(host, e.knownHosts)
}

// DeviceUptime returns the confirmed share of hourly samples per device.
func (e *Engine) DeviceUptime(ctx context.Context, days int) ([]history.Uptime, error) {
	if e.archive == nil {
		return nil, errors.New("尚未启用PostgreSQL长期历史")
	}
	now := time.Now()
	return e.archive.Uptime(ctx, now.AddDate(0, 0, -days), now)
}

// SystemStats describes the process and the history database for the settings page.
func (e *Engine) SystemStats(ctx context.Context) (map[string]any, error) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	e.mu.RLock()
	last := e.lastMaintenance
	saved := e.archiveSaved
	e.mu.RUnlock()
	out := map[string]any{
		"rss_bytes":        mem.Sys,
		"goroutines":       runtime.NumGoroutine(),
		"uptime_seconds":   time.Since(e.startedAt).Seconds(),
		"storage":          e.repo.Kind(),
		"archive_enabled":  e.archive != nil,
		"last_maintenance": last,
		"last_saved":       saved,
	}
	if e.archive != nil {
		if st, err := e.archive.Stats(ctx); err == nil {
			out["database"] = st
		}
	}
	return out, nil
}

// AutoTopology rebuilds the logical map from the configured profiles and the
// devices the collectors currently see. It only returns the suggestion: the
// operator still reviews and saves it, so a curated map is never overwritten.
func (e *Engine) AutoTopology(ctx context.Context) (json.RawMessage, error) {
	e.mu.RLock()
	profiles := append([]domain.Profile(nil), e.config.Profiles...)
	devices := append([]domain.Device(nil), e.state.Devices...)
	e.mu.RUnlock()
	if len(profiles) == 0 {
		return nil, errors.New("还没有采集连接，无法生成拓扑")
	}
	kinds := map[string]string{
		"openwrt": "router", "asus": "ap", "esxi": "hypervisor", "zte": "ont", "tcp": "switch",
	}
	level := func(p domain.Profile) int {
		depth := 0
		seen := map[string]bool{p.ID: true}
		cur := p
		for cur.Parent != "" && depth < 4 {
			parent := findProfile(profiles, cur.Parent)
			if parent == nil || seen[parent.ID] {
				break
			}
			seen[parent.ID] = true
			cur = *parent
			depth++
		}
		return depth
	}
	rows := map[int][]domain.Profile{}
	for _, p := range profiles {
		rows[level(p)] = append(rows[level(p)], p)
	}
	nodes := []map[string]any{}
	for l := 0; l <= 4; l++ {
		for i, p := range rows[l] {
			sub := ""
			if kinds[p.Adapter] == "ap" {
				wireless := 0
				for _, d := range devices {
					if d.Online && d.APID == p.ID {
						wireless++
					}
				}
				sub = fmt.Sprintf("%d 台无线终端在线", wireless)
			}
			nodes = append(nodes, map[string]any{
				"id": p.ID, "name": p.Name, "host": p.Host, "kind": kinds[p.Adapter],
				"profile_id": p.ID, "x": 85 + l*180, "y": 55 + i*110, "subtitle": sub,
			})
		}
	}
	links := []map[string]any{}
	for _, p := range profiles {
		if p.Parent != "" && findProfile(profiles, p.Parent) != nil {
			links = append(links, map[string]any{"from": p.Parent, "to": p.ID, "label": "配置关系", "evidence": "采集配置中的上级关系"})
		}
	}
	doc := map[string]any{
		"recorded_at": time.Now().UTC().Format(time.RFC3339),
		"nodes":       nodes, "links": links,
		"limitations": []string{"按采集配置的上级关系生成；端口、线缆与速率仍以实测记录为准。"},
	}
	return json.Marshal(doc)
}

func findProfile(list []domain.Profile, id string) *domain.Profile {
	for i := range list {
		if list[i].ID == id {
			return &list[i]
		}
	}
	return nil
}

func (e *Engine) SetTopology(ctx context.Context, raw json.RawMessage) error {
	var t struct {
		Nodes []struct{ ID, Host string }
		Links []struct{ From, To string }
	}
	if err := json.Unmarshal(raw, &t); err != nil || len(t.Nodes) > 64 || len(t.Nodes) == 0 || len(t.Links) > 128 {
		return errors.New("拓扑格式无效，最多64个节点和128条连线")
	}
	ids := map[string]bool{}
	for _, n := range t.Nodes {
		if !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`).MatchString(n.ID) || ids[n.ID] {
			return errors.New("拓扑节点ID无效或重复")
		}
		ids[n.ID] = true
	}
	for _, l := range t.Links {
		if !ids[l.From] || !ids[l.To] {
			return errors.New("拓扑连线引用了不存在的节点")
		}
	}
	if err := e.repo.Write(ctx, "topology", raw); err != nil {
		return err
	}
	e.mu.Lock()
	e.topology = append(json.RawMessage{}, raw...)
	e.mu.Unlock()
	return nil
}

// Discover probes a /24 (or the configured LAN) for management ports and
// suggests an adapter from the SSH banner or the HTTP title. It is the first
// step of the setup wizard, so it must stay bounded and never block for long.
func (e *Engine) Discover(ctx context.Context, cidr string) []domain.Candidate {
	hosts := e.scanHosts(cidr)
	e.mu.RLock()
	configured := map[string]bool{}
	for _, p := range e.config.Profiles {
		configured[p.Host] = true
	}
	e.mu.RUnlock()
	out := []domain.Candidate{}
	var mu sync.Mutex
	sem := make(chan struct{}, 32)
	var wg sync.WaitGroup
	for _, host := range hosts {
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			services, kind, title := probeHost(ctx, host)
			if len(services) == 0 {
				return
			}
			mu.Lock()
			out = append(out, domain.Candidate{Host: host, Services: services, Configured: configured[host], Kind: kind, Title: title})
			mu.Unlock()
		}(host)
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool { return ipLess(out[i].Host, out[j].Host) })
	return out
}

// scanHosts resolves the address range: explicit CIDR, then the configured LAN,
// then the /24 of the devices already known.
func (e *Engine) scanHosts(cidr string) []string {
	if cidr == "" {
		cidr = e.lanCIDR
	}
	if cidr == "" {
		count := map[string]int{}
		e.mu.RLock()
		for _, d := range e.state.Devices {
			if ip := net.ParseIP(d.IP); ip != nil && ip.IsPrivate() {
				count[ip.Mask(net.CIDRMask(24, 32)).String()]++
			}
		}
		e.mu.RUnlock()
		best := ""
		for c, n := range count {
			if n > count[best] {
				best = c
			}
		}
		if best == "" {
			return nil
		}
		cidr = best + "/24"
	}
	ip, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil
	}
	_ = ip
	hosts := []string{}
	for cur := network.IP.Mask(network.Mask); network.Contains(cur) && len(hosts) < 1024; cur = nextIP(cur) {
		hosts = append(hosts, cur.String())
	}
	// Drop the network and broadcast addresses of a /24.
	if len(hosts) > 2 {
		hosts = hosts[1 : len(hosts)-1]
	}
	return hosts
}

func nextIP(ip net.IP) net.IP {
	next := make(net.IP, len(ip))
	copy(next, ip)
	for i := len(next) - 1; i >= 0; i-- {
		next[i]++
		if next[i] != 0 {
			break
		}
	}
	return next
}

func ipLess(a, b string) bool {
	return bytes.Compare(net.ParseIP(a), net.ParseIP(b)) < 0
}

// probeHost checks the management ports and reads just enough of each banner to
// guess what the device is.
func probeHost(ctx context.Context, host string) ([]string, string, string) {
	services := []string{}
	kind, title := "", ""
	// A TLS certificate subject is the most reliable signal for ESXi and ASUS.
	if c, err := tls.DialWithDialer(&net.Dialer{Timeout: 350 * time.Millisecond}, "tcp",
		net.JoinHostPort(host, "443"), &tls.Config{InsecureSkipVerify: true}); err == nil {
		if certs := c.ConnectionState().PeerCertificates; len(certs) > 0 {
			subj := strings.ToLower(certs[0].Subject.String() + " " + certs[0].Issuer.String())
			switch {
			case strings.Contains(subj, "vmware"):
				kind = "esxi"
			case strings.Contains(subj, "asus"):
				kind = "asus"
			}
		}
		c.Close()
	}
	for _, p := range []struct{ port, label string }{{"22", "SSH"}, {"23", "Telnet"}, {"80", "HTTP"}, {"443", "HTTPS"}} {
		conn, err := (&net.Dialer{Timeout: 350 * time.Millisecond}).DialContext(ctx, "tcp", net.JoinHostPort(host, p.port))
		if err != nil {
			continue
		}
		services = append(services, p.label)
		if p.port == "80" {
			_ = conn.SetDeadline(time.Now().Add(900 * time.Millisecond))
			_, _ = conn.Write([]byte("GET / HTTP/1.0\r\nHost: " + host + "\r\n\r\n"))
		}
		banner := readSome(conn, 4096, 900*time.Millisecond)
		conn.Close()
		switch p.port {
		case "22":
			if strings.Contains(banner, "dropbear") {
				kind = "openwrt"
			}
		case "80":
			title = pageTitle(banner)
			lower := strings.ToLower(title + " " + banner)
			switch {
			case strings.Contains(lower, "vmware") || strings.Contains(lower, "esxi"):
				kind = "esxi"
			case strings.Contains(lower, "asus"), strings.Contains(lower, "main_login.asp"):
				kind = "asus"
			case strings.Contains(lower, "luci") || strings.Contains(lower, "openwrt"):
				kind = "openwrt"
			}
		}
	}
	if kind == "" && len(services) > 0 {
		if hasService(services, "Telnet") && !hasService(services, "SSH") {
			kind = "zte"
		} else if hasService(services, "SSH") {
			kind = "openwrt"
		} else {
			kind = "tcp"
		}
	}
	return services, kind, title
}

// readSome reads until the closing title tag or the deadline, whichever is
// first: a few hundred bytes are usually not enough to reach the title.
func readSome(conn net.Conn, max int, wait time.Duration) string {
	var buf bytes.Buffer
	_ = conn.SetReadDeadline(time.Now().Add(wait))
	tmp := make([]byte, 512)
	for buf.Len() < max {
		n, err := conn.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
		}
		if err != nil {
			break
		}
		if strings.Contains(buf.String(), "</title>") {
			break
		}
	}
	return buf.String()
}

func hasService(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func pageTitle(banner string) string {
	if m := regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`).FindStringSubmatch(banner); len(m) == 2 {
		return strings.Join(strings.Fields(m[1]), " ")
	}
	return ""
}
