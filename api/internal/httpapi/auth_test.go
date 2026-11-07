package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPageLoginProtectsDataAndLogoutRevokesSession(t *testing.T) {
	s := serverForTest(t)
	key := strings.Repeat("b", 64)
	_ = s.ConfigureTrustedProxy(key)
	if s.ConfigureLogin("atlas", "long-password-for-test") != nil {
		t.Fatal("configure failed")
	}
	call := func(path, method, body, origin string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		r.Host = "atlas.example.test"
		r.Header.Set("X-Atlas-Proxy-Key", key)
		r.Header.Set("Origin", origin)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	if w := call("/api/v1/state", "GET", "", "", nil); w.Code != 401 || w.Header().Get("WWW-Authenticate") != "" {
		t.Fatal("data unprotected or native auth challenge emitted")
	}
	if call("/auth/login", "POST", `{"Username":"atlas","Password":"wrong"}`, "https://atlas.example.test", nil).Code != 401 {
		t.Fatal("wrong password accepted")
	}
	if call("/auth/login", "POST", `{"Username":"atlas","Password":"long-password-for-test"}`, "https://other.example.test", nil).Code != 403 {
		t.Fatal("cross-site login accepted")
	}
	w := call("/auth/login", "POST", `{"Username":"atlas","Password":"long-password-for-test"}`, "https://atlas.example.test", nil)
	if w.Code != 200 || len(w.Result().Cookies()) != 1 {
		t.Fatal("login failed", w.Code)
	}
	cookie := w.Result().Cookies()[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatal("cookie protections missing")
	}
	if call("/api/v1/state", "GET", "", "", cookie).Code != 200 {
		t.Fatal("session rejected")
	}
	if call("/auth/logout", "POST", "", "https://other.example.test", cookie).Code != 403 {
		t.Fatal("cross-site logout accepted")
	}
	if call("/auth/logout", "POST", "", "https://atlas.example.test", cookie).Code != 200 || call("/api/v1/state", "GET", "", "", cookie).Code != 401 {
		t.Fatal("logout did not revoke session")
	}
}
