package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"signalatlas/api/internal/history"
	"signalatlas/api/internal/repository"
)

func (s *Server) historyRoutes() {
	s.mux.HandleFunc("GET /api/v1/history/status", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		v, err := s.Engine.ArchiveStatus(ctx)
		if err != nil {
			fail(w, 503, err)
			return
		}
		write(w, 200, v)
	})
	s.mux.HandleFunc("GET /api/v1/history/devices", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		v, err := s.Engine.HistoricalEntities(ctx)
		if err != nil {
			fail(w, 503, err)
			return
		}
		write(w, 200, map[string]any{"items": v})
	})
	s.mux.HandleFunc("GET /api/v1/history/snapshot", func(w http.ResponseWriter, r *http.Request) {
		at, err := time.Parse(time.RFC3339, r.URL.Query().Get("at"))
		if err != nil {
			fail(w, 400, errors.New("回放时间无效"))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		v, err := s.Engine.HistoricalSnapshot(ctx, at)
		if errors.Is(err, repository.ErrNotFound) {
			fail(w, 404, errors.New("该时间前5分钟没有明细记录；不能用小时汇总还原现场"))
			return
		}
		if err != nil {
			fail(w, 503, errors.New("状态回放暂不可用"))
			return
		}
		write(w, 200, v)
	})
	s.mux.HandleFunc("GET /api/v1/history/uptime", func(w http.ResponseWriter, r *http.Request) {
		days, err := strconv.Atoi(r.URL.Query().Get("days"))
		if err != nil || days < 1 || days > 90 {
			days = 7
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		v, err := s.Engine.DeviceUptime(ctx, days)
		if err != nil {
			fail(w, 503, err)
			return
		}
		write(w, 200, map[string]any{"days": days, "items": v})
	})
	s.mux.HandleFunc("GET /api/v1/system", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		v, err := s.Engine.SystemStats(ctx)
		if err != nil {
			fail(w, 503, err)
			return
		}
		write(w, 200, v)
	})
	s.mux.HandleFunc("GET /api/v1/history", func(w http.ResponseWriter, r *http.Request) {
		v := r.URL.Query()
		q := history.Query{Kind: v.Get("kind"), MAC: v.Get("mac"), ProfileID: v.Get("profile_id"), Target: v.Get("target"), Resolution: v.Get("resolution")}
		var err error
		q.From, err = time.Parse(time.RFC3339, v.Get("from"))
		if err != nil {
			fail(w, 400, errors.New("开始时间无效"))
			return
		}
		q.To, err = time.Parse(time.RFC3339, v.Get("to"))
		if err != nil {
			fail(w, 400, errors.New("结束时间无效"))
			return
		}
		if v.Get("limit") != "" {
			q.Limit, err = strconv.Atoi(v.Get("limit"))
			if err != nil {
				fail(w, 400, errors.New("分页大小无效"))
				return
			}
		}
		if err = q.Validate(v.Get("cursor")); err != nil {
			fail(w, 400, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		out, err := s.Engine.History(ctx, q)
		if err != nil {
			fail(w, 503, err)
			return
		}
		write(w, 200, out)
	})
}
