package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"signalatlas/api/internal/repository"
	"signalatlas/api/internal/service"
	"strings"
	"testing"
)

func serverForTest(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	repo, _ := repository.NewFile(dir)
	engine, err := service.New(context.Background(), repo, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	return New(engine, dir, "")
}
func TestAuthenticatedProxyKeepsTokenAndSameHostOriginProtection(t *testing.T) {
	s := serverForTest(t)
	key := strings.Repeat("a", 64)
	if s.ConfigureTrustedProxy(key) != nil {
		t.Fatal("valid proxy key rejected")
	}
	for _, tc := range []struct {
		method, host, proxy, token, origin string
		want                               int
	}{
		{"GET", "atlas.example.test", "", "", "", 403},
		{"GET", "atlas.example.test", "wrong", "", "", 403},
		{"GET", "atlas.example.test", key, "", "", 200},
		{"POST", "atlas.example.test", key, "", "https://atlas.example.test", 403},
		{"POST", "atlas.example.test", key, s.Token, "", 403},
		{"POST", "atlas.example.test", key, s.Token, "https://other.example.test", 403},
		{"POST", "atlas.example.test", key, s.Token, "https://atlas.example.test", 200},
		{"POST", "atlas.example.test:1443", key, s.Token, "https://atlas.example.test", 403},
		{"POST", "atlas.example.test:1443", key, s.Token, "https://atlas.example.test:1443", 200},
	} {
		path := "/api/v1/state"
		if tc.method == "POST" {
			path = "/api/v1/refresh"
		}
		r := httptest.NewRequest(tc.method, path, nil)
		r.Host = tc.host
		r.Header.Set("X-Atlas-Proxy-Key", tc.proxy)
		r.Header.Set("X-Atlas-Token", tc.token)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%+v: got %d", tc, w.Code)
		}
	}
	if s.ConfigureTrustedProxy("short") == nil {
		t.Fatal("invalid proxy key accepted")
	}
}
func TestHistoryQueriesValidateBeforeStorageAndFileModeIsExplicit(t *testing.T) {
	s := serverForTest(t)
	for _, tc := range []struct {
		path string
		want int
	}{
		{"/api/v1/history/status", 200},
		{"/api/v1/history?kind=traffic&from=bad&to=bad", 400},
		{"/api/v1/history?kind=devices&from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z&mac=bad", 400},
		{"/api/v1/history?kind=traffic&from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z&limit=2001", 400},
		{"/api/v1/history?kind=traffic&from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z", 503},
		{"/api/v1/history/snapshot?at=bad", 400},
	} {
		r := httptest.NewRequest("GET", tc.path, nil)
		r.Host = "localhost"
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%s: got %d", tc.path, w.Code)
		}
	}
}
func TestLocalHostAndMutationProtection(t *testing.T) {
	s := serverForTest(t)
	for _, tc := range []struct {
		method, path, host, token, origin string
		want                              int
	}{
		{"GET", "/api/v1/state", "127.0.0.1:8790", "", "", 200},
		{"GET", "/api/v1/state", "attacker.example", "", "", 403},
		{"POST", "/api/v1/refresh", "127.0.0.1:8790", "", "", 403},
		{"POST", "/api/v1/refresh", "127.0.0.1:8790", s.Token, "https://attacker.example", 403},
		{"POST", "/api/v1/refresh", "127.0.0.1:8790", s.Token, "http://127.0.0.1:5173", 200},
	} {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		r.Host = tc.host
		r.Header.Set("X-Atlas-Token", tc.token)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("%+v: got %d", tc, w.Code)
		}
	}
}
func TestExportOmitsSessionToken(t *testing.T) {
	s := serverForTest(t)
	r := httptest.NewRequest("GET", "/api/v1/export", nil)
	r.Host = "localhost"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if strings.Contains(w.Body.String(), s.Token) {
		t.Fatal("session token in export")
	}
	var export map[string]any
	if json.Unmarshal(w.Body.Bytes(), &export) != nil {
		t.Fatal("invalid export")
	}
}

func TestLANAccessRequiresExactHostOriginAndToken(t *testing.T) {
	s := serverForTest(t)
	if err := s.ConfigureAccess([]string{"192.168.1.119:8768"}, []string{"http://192.168.1.119:8768"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		host, origin, token string
		want                int
	}{
		{"192.168.1.119:8768", "http://192.168.1.119:8768", s.Token, 200},
		{"192.168.1.119:8768", "http://192.168.1.119:9999", s.Token, 403},
		{"192.168.1.119:8768", "http://192.168.1.119.evil:8768", s.Token, 403},
		{"192.168.1.119:9999", "http://192.168.1.119:8768", s.Token, 403},
		{"192.168.1.119:8768", "http://192.168.1.119:8768", "", 403},
	} {
		r := httptest.NewRequest("POST", "/api/v1/refresh", nil)
		r.Host = tc.host
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("X-Atlas-Token", tc.token)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("host=%s origin=%s: got %d want %d", tc.host, tc.origin, w.Code, tc.want)
		}
	}
	for _, origin := range []string{"http://evil.example", "http://192.168.1.119:8768/path", "http://192.168.1.119:8768?query=x"} {
		if s.ConfigureAccess([]string{"192.168.1.119:8768"}, []string{origin}) == nil {
			t.Fatalf("accepted invalid origin %q", origin)
		}
	}
}
