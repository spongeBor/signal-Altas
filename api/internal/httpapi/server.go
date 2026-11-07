package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"signalatlas/api/internal/domain"
	"signalatlas/api/internal/service"
	"strings"
	"time"
)

type Server struct {
	Engine                 *service.Engine
	DataDir, WebDir, Token string
	webFS                  fs.FS
	// WANMeter returns the latest high-frequency WAN reading, or nil when the
	// optional SNMP sampler is not configured.
	WANMeter       func() any
	mux            *http.ServeMux
	allowedHosts   map[string]bool
	allowedOrigins map[string]bool
	proxyKey       string
	login          *frontendLogin
	// LAN ports that accept any private address as Host, which is what a
	// container behind port mapping sees when the operator opens the app by IP.
	lanPorts map[string]bool
}

// SetWebAssets serves the frontend from an embedded filesystem when set; the
// WebDir fallback stays available for development.
func (s *Server) SetWebAssets(fsys fs.FS) { s.webFS = fsys }

func New(e *service.Engine, dataDir, webDir string) *Server {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	s := &Server{Engine: e, DataDir: dataDir, WebDir: webDir, Token: hex.EncodeToString(b), mux: http.NewServeMux()}
	s.routes()
	return s
}
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, err error) {
	write(w, status, map[string]string{"error": err.Error()})
}
func loopbackHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return host == "127.0.0.1" || host == "localhost" || host == "[::1]" || host == "::1"
}

// ConfigureAccess opts a deployment into exact LAN hosts and origins. The
// default constructor remains loopback-only for local development.
func (s *Server) ConfigureAccess(hosts, origins []string) error {
	h := map[string]bool{}
	o := map[string]bool{}
	ports := map[string]bool{}
	for _, host := range hosts {
		host = strings.ToLower(strings.TrimSpace(host))
		if host == "" {
			continue
		}
		if strings.HasPrefix(host, "*:") {
			port := strings.TrimPrefix(host, "*:")
			if port == "" || strings.Trim(port, "0123456789") != "" {
				return errors.New("通配Host必须写成 *:端口")
			}
			ports[port] = true
			continue
		}
		u, err := url.Parse("http://" + host)
		if err != nil || u.Host != host || u.Hostname() == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil || strings.ContainsAny(host, "* /\\") {
			return errors.New("允许的Host必须为精确域名或IP及可选端口")
		}
		h[host] = true
	}
	for _, origin := range origins {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (!h[strings.ToLower(u.Host)] && !loopbackHost(u.Host)) {
			return errors.New("允许的Origin必须与允许的Host精确对应")
		}
		o[u.Scheme+"://"+strings.ToLower(u.Host)] = true
	}
	s.allowedHosts = h
	s.allowedOrigins = o
	s.lanPorts = ports
	return nil
}
func (s *Server) hostAllowed(host string) bool {
	if loopbackHost(host) || s.allowedHosts[strings.ToLower(host)] {
		return true
	}
	return s.lanHostAllowed(host)
}

// lanHostAllowed accepts a private IPv4 literal on a wildcard port. Public
// hostnames stay rejected, so DNS rebinding is not made easier.
func (s *Server) lanHostAllowed(host string) bool {
	h, port, err := net.SplitHostPort(host)
	if err != nil || !s.lanPorts[port] {
		return false
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsPrivate()
}

func (s *Server) originAllowedFor(u *url.URL, requestHost string) bool {
	if s.originAllowed(u) {
		return true
	}
	return s.lanHostAllowed(requestHost) && strings.EqualFold(u.Host, requestHost)
}

// The authenticated frontend signs its proxy requests with a private key.
// Unpublished API ports and direct development requests keep exact Host rules.
func (s *Server) ConfigureTrustedProxy(key string) error {
	if key == "" {
		return nil
	}
	b, err := hex.DecodeString(key)
	if err != nil || len(b) != 32 {
		return errors.New("前端代理密钥应为32字节十六进制字符串")
	}
	s.proxyKey = key
	return nil
}
func (s *Server) trustedProxy(r *http.Request) bool {
	return s.proxyKey != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Atlas-Proxy-Key")), []byte(s.proxyKey)) == 1
}
func validRequestHost(host string) bool {
	u, err := url.Parse("http://" + host)
	return err == nil && u.Host == host && u.Hostname() != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" && !strings.ContainsAny(host, "* /\\")
}
func sameRequestOrigin(r *http.Request, u *url.URL) bool {
	return u != nil && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" && (u.Scheme == "http" || u.Scheme == "https") && strings.EqualFold(u.Host, r.Host)
}
func (s *Server) originAllowed(u *url.URL) bool {
	if u == nil || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if u.Scheme == "http" && loopbackHost(u.Host) {
		return true
	}
	return s.allowedOrigins[u.Scheme+"://"+strings.ToLower(u.Host)]
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Cache-Control", "no-store")
	trusted := s.trustedProxy(r)
	if r.Header.Get("X-Atlas-Proxy-Key") != "" && !trusted {
		fail(w, 403, errors.New("前端代理身份无效"))
		return
	}
	if !s.hostAllowed(r.Host) && !(trusted && validRequestHost(r.Host)) {
		fail(w, 403, errors.New("该访问地址未获允许"))
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/v1/health" && !s.loggedIn(r) {
		fail(w, 401, errors.New("请先登录工作台"))
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") && r.Method != "GET" && r.Method != "HEAD" {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Atlas-Token")), []byte(s.Token)) != 1 {
			fail(w, 403, errors.New("请求凭证无效，请刷新网页"))
			return
		}
		if trusted && r.Header.Get("Origin") == "" {
			fail(w, 403, errors.New("管理请求需要同源Origin"))
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			allowed := s.originAllowedFor(u, r.Host)
			if trusted {
				allowed = sameRequestOrigin(r, u)
			}
			if err != nil || !allowed {
				fail(w, 403, errors.New("请求来源不匹配"))
				return
			}
		}
	}
	defer func() {
		if recover() != nil {
			fail(w, 500, errors.New("请求处理失败，请检查输入并重试"))
		}
	}()
	s.mux.ServeHTTP(w, r)
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	if err := d.Decode(v); err != nil {
		fail(w, 400, errors.New("请求JSON格式无效或内容过大"))
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		fail(w, 400, errors.New("请求只能包含一个JSON文档"))
		return false
	}
	return true
}
func mutation(w http.ResponseWriter, err error) {
	if err != nil {
		fail(w, 400, err)
	} else {
		write(w, 200, map[string]bool{"ok": true})
	}
}
func (s *Server) routes() {
	s.authRoutes()
	s.historyRoutes()
	s.mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		write(w, 200, map[string]string{"service": "signal-atlas", "version": "0.3.1", "backend": "go"})
	})
	s.mux.HandleFunc("GET /api/v1/state", func(w http.ResponseWriter, r *http.Request) {
		v := s.Engine.View()
		v["token"] = s.Token
		user := ""
		if s.login != nil {
			user = s.login.user
		}
		v["access"] = map[string]any{"login_enabled": s.login != nil, "user": user}
		if s.WANMeter != nil {
			if m := s.WANMeter(); m != nil {
				v["wan_meter"] = m
			}
		}
		write(w, 200, v)
	})
	s.mux.HandleFunc("GET /api/v1/export", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="signal-atlas-export.json"`)
		write(w, 200, s.Engine.Export())
	})
	s.mux.HandleFunc("POST /api/v1/refresh", func(w http.ResponseWriter, r *http.Request) {
		s.Engine.Refresh()
		write(w, 200, map[string]bool{"ok": true})
	})
	s.mux.HandleFunc("GET /api/v1/hostkeys", func(w http.ResponseWriter, r *http.Request) {
		write(w, 200, map[string]any{"pending": s.Engine.PendingHostKeys()})
	})
	s.mux.HandleFunc("POST /api/v1/hostkeys/trust", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Host string `json:"host"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body) != nil || body.Host == "" {
			fail(w, 400, errors.New("请求格式无效"))
			return
		}
		if err := s.Engine.TrustHostKey(body.Host); err != nil {
			fail(w, 400, err)
			return
		}
		s.Engine.Refresh()
		write(w, 200, map[string]any{"ok": true, "pending": s.Engine.PendingHostKeys()})
	})
	s.mux.HandleFunc("PUT /api/v1/config", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			Profiles []domain.ProfileInput `json:"profiles"`
			Interval int                   `json:"interval"`
		}
		if decode(w, r, &v) {
			mutation(w, s.Engine.SetConfig(r.Context(), v.Profiles, v.Interval))
		}
	})
	s.mux.HandleFunc("PUT /api/v1/labels", func(w http.ResponseWriter, r *http.Request) {
		var v struct{ MAC, Name string }
		if decode(w, r, &v) {
			mutation(w, s.Engine.Rename(r.Context(), v.MAC, v.Name))
		}
	})
	s.mux.HandleFunc("POST /api/v1/discover", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			CIDR string `json:"cidr"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&body)
		if body.CIDR == "" {
			// A fresh install has no devices and no configured LAN, so the
			// browser's own address is the only hint about the subnet.
			if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
				if ip := net.ParseIP(host); ip != nil && ip.To4() != nil && ip.IsPrivate() {
					body.CIDR = ip.Mask(net.CIDRMask(24, 32)).String() + "/24"
				}
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		write(w, 200, map[string]any{"candidates": s.Engine.Discover(ctx, body.CIDR)})
	})
	s.mux.HandleFunc("POST /api/v1/entities/forget", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			MAC string `json:"mac"`
		}
		if decode(w, r, &body) {
			mutation(w, s.Engine.Forget(r.Context(), body.MAC))
		}
	})
	s.mux.HandleFunc("POST /api/v1/topology/auto", func(w http.ResponseWriter, r *http.Request) {
		doc, err := s.Engine.AutoTopology(r.Context())
		if err != nil {
			fail(w, 400, err)
			return
		}
		write(w, 200, map[string]any{"ok": true, "topology": json.RawMessage(doc)})
	})
	s.mux.HandleFunc("PUT /api/v1/topology", func(w http.ResponseWriter, r *http.Request) {
		var v json.RawMessage
		if decode(w, r, &v) {
			mutation(w, s.Engine.SetTopology(r.Context(), v))
		}
	})
	s.mux.HandleFunc("/", s.web)
}
func (s *Server) web(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		fail(w, 404, errors.New("接口不存在"))
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		w.WriteHeader(405)
		return
	}
	if s.webFS != nil {
		name := strings.TrimPrefix(filepath.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if f, err := s.webFS.Open(name); err == nil {
			_ = f.Close()
			http.ServeFileFS(w, r, s.webFS, name)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFileFS(w, r, s.webFS, "index.html")
		return
	}
	if s.WebDir == "" {
		write(w, 200, map[string]string{"service": "signal-atlas", "frontend": "开发模式请打开 http://127.0.0.1:5173"})
		return
	}
	clean := filepath.Clean("/" + r.URL.Path)
	p := filepath.Join(s.WebDir, clean)
	if st, err := os.Stat(p); err != nil || st.IsDir() {
		p = filepath.Join(s.WebDir, "index.html")
	}
	if _, err := os.Stat(p); err != nil {
		fail(w, 404, fmt.Errorf("前端尚未构建，请在工程根目录执行npm run build"))
		return
	}
	http.ServeFile(w, r, p)
}
