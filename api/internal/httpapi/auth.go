package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const sessionCookie = "signal_atlas_session"

type frontendLogin struct {
	mu       sync.Mutex
	user     string
	hash     []byte
	sessions map[[32]byte]time.Time
}

func (s *Server) ConfigureLogin(user, password string) error {
	if user == "" && password == "" {
		return nil
	}
	if len(user) < 1 || len(user) > 40 || strings.ContainsAny(user, "\r\n:") || len(password) < 16 || len(password) > 72 {
		return errors.New("网页登录配置无效，请检查用户名和16到72字节密码")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return errors.New("网页登录初始化失败")
	}
	s.login = &frontendLogin{user: user, hash: hash, sessions: map[[32]byte]time.Time{}}
	return nil
}
func (s *Server) loggedIn(r *http.Request) bool {
	if s.login == nil {
		return true
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil || len(c.Value) != 64 {
		return false
	}
	key := sha256.Sum256([]byte(c.Value))
	s.login.mu.Lock()
	defer s.login.mu.Unlock()
	expires, ok := s.login.sessions[key]
	if ok && time.Now().Before(expires) {
		return true
	}
	delete(s.login.sessions, key)
	return false
}
func (s *Server) authOriginAllowed(r *http.Request) bool {
	u, err := url.Parse(r.Header.Get("Origin"))
	if err != nil || r.Header.Get("Origin") == "" {
		return false
	}
	if s.trustedProxy(r) {
		return sameRequestOrigin(r, u)
	}
	return s.originAllowedFor(u, r.Host)
}
func (s *Server) cookie(r *http.Request, value string, expires time.Time, maxAge int) *http.Cookie {
	secure := r.TLS != nil
	if s.trustedProxy(r) {
		u, _ := url.Parse(r.Header.Get("Origin"))
		secure = secure || r.Header.Get("X-Forwarded-Proto") == "https" || (sameRequestOrigin(r, u) && u.Scheme == "https")
	}
	return &http.Cookie{Name: sessionCookie, Value: value, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, Expires: expires, MaxAge: maxAge}
}
func (s *Server) authRoutes() {
	s.mux.HandleFunc("GET /auth/session", func(w http.ResponseWriter, r *http.Request) {
		v := map[string]any{"authenticated": s.loggedIn(r), "enabled": s.login != nil}
		if s.login != nil && s.loggedIn(r) {
			v["user"] = s.login.user
		}
		write(w, 200, v)
	})
	s.mux.HandleFunc("POST /auth/login", func(w http.ResponseWriter, r *http.Request) {
		if !s.authOriginAllowed(r) {
			fail(w, 403, errors.New("登录来源不匹配"))
			return
		}
		if s.login == nil {
			write(w, 200, map[string]bool{"ok": true})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		var input struct{ Username, Password string }
		if !decode(w, r, &input) {
			return
		}
		a, b := sha256.Sum256([]byte(input.Username)), sha256.Sum256([]byte(s.login.user))
		validUser := subtle.ConstantTimeCompare(a[:], b[:]) == 1
		validPassword := bcrypt.CompareHashAndPassword(s.login.hash, []byte(input.Password)) == nil
		if !validUser || !validPassword {
			fail(w, 401, errors.New("账号或密码不正确"))
			return
		}
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			fail(w, 500, errors.New("登录暂不可用"))
			return
		}
		token := hex.EncodeToString(raw)
		key := sha256.Sum256([]byte(token))
		expires := time.Now().Add(12 * time.Hour)
		s.login.mu.Lock()
		for k, v := range s.login.sessions {
			if time.Now().After(v) {
				delete(s.login.sessions, k)
			}
		}
		if len(s.login.sessions) >= 256 {
			s.login.mu.Unlock()
			fail(w, 429, errors.New("登录会话过多，请稍后重试"))
			return
		}
		s.login.sessions[key] = expires
		s.login.mu.Unlock()
		http.SetCookie(w, s.cookie(r, token, expires, 12*3600))
		write(w, 200, map[string]bool{"ok": true})
	})
	s.mux.HandleFunc("POST /auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if !s.authOriginAllowed(r) {
			fail(w, 403, errors.New("退出来源不匹配"))
			return
		}
		if s.login != nil {
			if c, err := r.Cookie(sessionCookie); err == nil {
				key := sha256.Sum256([]byte(c.Value))
				s.login.mu.Lock()
				delete(s.login.sessions, key)
				s.login.mu.Unlock()
			}
		}
		http.SetCookie(w, s.cookie(r, "", time.Unix(1, 0), -1))
		write(w, 200, map[string]bool{"ok": true})
	})
}
