package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"golang.org/x/term"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"signalatlas/api/internal/collector"
	"signalatlas/api/internal/config"
	"signalatlas/api/internal/history"
	"signalatlas/api/internal/httpapi"
	"signalatlas/api/internal/repository"
	"signalatlas/api/internal/service"
	"signalatlas/api/internal/wanmeter"
	"signalatlas/api/internal/webui"
	"strings"
	"syscall"
	"time"
)

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
func dotenv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		s := strings.TrimSpace(sc.Text())
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		k, v, ok := strings.Cut(s, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if os.Getenv(k) == "" && (k == "DATABASE_URL" || strings.HasPrefix(k, "SIGNAL_ATLAS_")) {
			_ = os.Setenv(k, v)
		}
	}
}
func cfgPathOrBuiltin(path string) string {
	if path == "" {
		return "内置默认"
	}
	return path
}
func main() {
	dotenv("../.env")
	dotenv(".env")
	healthcheck := flag.Bool("healthcheck", false, "check local API health and exit")
	addr := flag.String("addr", "", "local HTTP listen address (default from config.yaml or 127.0.0.1:8790)")
	dataDir := flag.String("data-dir", env("SIGNAL_ATLAS_DATA_DIR", "../.local/data"), "local data and images")
	webDir := flag.String("web-dir", "", "built frontend directory (production)")
	stdinDB := flag.Bool("database-stdin", false, "read database URL from private stdin pipe")
	askDB := flag.Bool("ask-db", false, "read database URL from terminal without echo")
	migrateOnly := flag.Bool("migrate-only", false, "check storage/migrate schema and exit")
	importHistory := flag.Bool("import-history-stdin", false, "import credential-free traffic/events from stdin")
	flag.Parse()
	cfgPath := os.Getenv("SIGNAL_ATLAS_CONFIG")
	if cfgPath == "" {
		if _, err := os.Stat(filepath.Join(*dataDir, "config.yaml")); err == nil {
			cfgPath = filepath.Join(*dataDir, "config.yaml")
		}
	}
	cfg, cfgErr := config.Load(cfgPath)
	if cfgErr != nil {
		log.Fatal(cfgErr)
	}
	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
	}
	if *addr == "" {
		*addr = env("SIGNAL_ATLAS_ADDR", cfg.Listen)
		if *addr == "" {
			*addr = "127.0.0.1:8790"
		}
	}
	if *healthcheck {
		client := http.Client{Timeout: 3 * time.Second}
		res, err := client.Get("http://127.0.0.1:8790/api/v1/health")
		if err != nil {
			os.Exit(1)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		return
	}
	allowedHosts := strings.Split(os.Getenv("SIGNAL_ATLAS_ALLOWED_HOSTS"), ",")
	allowedOrigins := strings.Split(os.Getenv("SIGNAL_ATLAS_ALLOWED_ORIGINS"), ",")
	host, _, err := net.SplitHostPort(*addr)
	if err != nil {
		log.Fatal("监听地址格式无效")
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		ip := net.ParseIP(host)
		if ip == nil || (!ip.IsPrivate() && !ip.IsUnspecified()) || strings.TrimSpace(os.Getenv("SIGNAL_ATLAS_ALLOWED_HOSTS")) == "" {
			log.Fatal("网络监听需要内网/容器地址及显式允许的Host")
		}
	}
	*dataDir, err = filepath.Abs(*dataDir)
	if err != nil {
		log.Fatal("数据目录无效")
	}
	if err = os.MkdirAll(*dataDir, 0700); err != nil {
		log.Fatal("无法创建数据目录")
	}
	dsn := os.Getenv("DATABASE_URL")
	if *stdinDB {
		b, readErr := io.ReadAll(io.LimitReader(os.Stdin, 16384))
		if readErr != nil {
			log.Fatal("无法读取数据库连接信息")
		}
		dsn = strings.TrimSpace(string(b))
		for i := range b {
			b[i] = 0
		}
		if dsn == "" {
			log.Fatal("数据库连接信息为空")
		}
	}
	if *askDB {
		fmt.Print("Database URL (hidden): ")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			log.Fatal("无法从终端读取数据库连接信息")
		}
		dsn = string(b)
		for i := range b {
			b[i] = 0
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	setup, stop := context.WithTimeout(ctx, 20*time.Second)
	var repo repository.Documents
	if dsn != "" {
		repo, err = repository.NewPostgres(setup, dsn)
		if err == nil {
			if err = repo.(*repository.Postgres).ConfigureRetention(cfg.Retention.DetailDays, cfg.Retention.SummaryDays); err != nil {
				log.Fatal(err)
			}
		}
	} else {
		sqlitePath := filepath.Join(*dataDir, "history.db")
		repo, err = repository.NewSQLite(sqlitePath)
		if err == nil {
			if err = repo.(*repository.SQLite).ConfigureRetention(cfg.Retention.DetailDays, cfg.Retention.SummaryDays); err != nil {
				log.Fatal(err)
			}
		}
	}
	if err != nil {
		stop()
		log.Fatal(err)
	}
	defer repo.Close()
	if *importHistory {
		store, ok := repo.(history.Store)
		if !ok {
			log.Fatal("历史导入需要PostgreSQL")
		}
		var seed history.Seed
		if json.NewDecoder(io.LimitReader(os.Stdin, 4<<20)).Decode(&seed) != nil {
			log.Fatal("历史导入JSON无效")
		}
		if err = store.Import(setup, seed); err != nil {
			log.Fatal("历史导入失败")
		}
		stop()
		log.Printf("Imported traffic=%d events=%d", len(seed.Traffic), len(seed.Events))
		return
	}
	knownHosts := os.Getenv("SIGNAL_ATLAS_KNOWN_HOSTS")
	if knownHosts != "" {
		// A fresh install has no known_hosts yet; create the empty file so the
		// first SSH attempt can report a pending fingerprint instead of a
		// missing-file error.
		if err := os.MkdirAll(filepath.Dir(knownHosts), 0700); err != nil {
			log.Fatal("无法创建 known_hosts 目录")
		}
		if _, statErr := os.Stat(knownHosts); errors.Is(statErr, os.ErrNotExist) {
			f, createErr := os.OpenFile(knownHosts, os.O_CREATE|os.O_WRONLY, 0600)
			if createErr != nil {
				log.Fatal("无法创建 known_hosts 文件")
			}
			_ = f.Close()
		}
	}
	engine, err := service.New(setup, repo, *dataDir, knownHosts)
	stop()
	if err != nil {
		log.Fatal(err)
	}
	targets := make([]collector.PingTarget, 0, len(cfg.PingTargets))
	for _, t := range cfg.PingTargets {
		targets = append(targets, collector.PingTarget{Name: t.Name, Host: t.Host})
	}
	engine.SetPingTargets(targets)
	engine.SetLANCIDR(cfg.LANCIDR)
	if *migrateOnly {
		log.Printf("Storage ready: %s", repo.Kind())
		return
	}
	api := httpapi.New(engine, *dataDir, *webDir)
	if *webDir == "" {
		if assets, ok := webui.Assets(); ok {
			api.SetWebAssets(assets)
		}
	}
	if err = api.ConfigureTrustedProxy(os.Getenv("SIGNAL_ATLAS_PROXY_TOKEN")); err != nil {
		log.Fatal(err)
	}
	if err = api.ConfigureLogin(os.Getenv("SIGNAL_ATLAS_WEB_USER"), os.Getenv("SIGNAL_ATLAS_WEB_PASSWORD")); err != nil {
		log.Fatal(err)
	}
	_ = os.Unsetenv("SIGNAL_ATLAS_WEB_PASSWORD")
	if host, community := cfg.SNMP.Host, os.Getenv("SIGNAL_ATLAS_SNMP_COMMUNITY"); host != "" && community != "" {
		meter := wanmeter.New(
			wanmeter.NewSNMP(host, community, 2*time.Second),
			cfg.SNMP.Interface,
			time.Duration(cfg.SNMP.IntervalMS)*time.Millisecond,
			time.Duration(cfg.SNMP.WindowSeconds)*time.Second,
		)
		go meter.Run(ctx)
		engine.SetWANPeak(func() (float64, float64, bool) {
			snapshot := meter.Snapshot()
			if snapshot == nil || snapshot.WindowSec <= 0 {
				return 0, 0, false
			}
			return snapshot.PeakDownMbps, snapshot.PeakUpMbps, true
		})
		api.WANMeter = func() any {
			if snapshot := meter.Snapshot(); snapshot != nil {
				return snapshot
			}
			return nil
		}
		log.Printf("WAN 峰值采样：%s 每 %dms 一次，窗口 %ds（配置：%s）", cfg.SNMP.Interface, cfg.SNMP.IntervalMS, cfg.SNMP.WindowSeconds, cfgPathOrBuiltin(cfgPath))
	}
	if err = api.ConfigureAccess(allowedHosts, allowedOrigins); err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Addr: *addr, Handler: api, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 60 * time.Second}
	go engine.Run(ctx)
	errCh := make(chan error, 1)
	go func() {
		log.Printf("Signal Atlas API: http://%s · storage=%s", *addr, repo.Kind())
		errCh <- server.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP服务退出：%s", err)
		}
		cancel()
	}
	shutdown, done := context.WithTimeout(context.Background(), 8*time.Second)
	defer done()
	_ = server.Shutdown(shutdown)
}
