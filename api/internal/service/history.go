package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"signalatlas/api/internal/domain"
	"signalatlas/api/internal/history"
)

func (e *Engine) saveHistory(parent context.Context, s domain.State) {
	if e.archive == nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	err := e.archive.Append(ctx, history.Observe(s))
	e.mu.Lock()
	if err != nil {
		e.archiveError = "历史写入失败，实时采集继续运行；该时段可能有记录缺口"
	} else {
		at := s.LastUpdated
		e.archiveSaved = &at
		e.archiveError = ""
	}
	maintain := time.Since(e.lastMaintenance) > time.Hour
	if maintain {
		e.lastMaintenance = time.Now()
	}
	e.mu.Unlock()
	if err == nil && maintain {
		if e.archive.Maintain(ctx, time.Now()) != nil {
			e.mu.Lock()
			e.archiveError = "历史清理暂未完成，将于下一轮维护重试"
			e.lastMaintenance = time.Now().Add(-59 * time.Minute)
			e.mu.Unlock()
		}
	}
}
func (e *Engine) ArchiveStatus(ctx context.Context) (history.Status, error) {
	if e.archive == nil {
		return history.Status{Enabled: false, DetailDays: 90, SummaryDays: 365}, nil
	}
	s, err := e.archive.Status(ctx)
	e.mu.RLock()
	s.LastSaved = e.archiveSaved
	s.Error = e.archiveError
	e.mu.RUnlock()
	if err != nil {
		return s, errors.New("历史数据库暂不可用")
	}
	return s, nil
}
func (e *Engine) History(ctx context.Context, q history.Query) (history.Page, error) {
	if e.archive == nil {
		return history.Page{}, errors.New("尚未启用PostgreSQL长期历史")
	}
	return e.archive.Query(ctx, q)
}
func (e *Engine) HistoricalEntities(ctx context.Context) ([]json.RawMessage, error) {
	if e.archive == nil {
		return nil, errors.New("尚未启用长期历史")
	}
	return e.archive.Entities(ctx)
}
func (e *Engine) HistoricalSnapshot(ctx context.Context, at time.Time) (history.Snapshot, error) {
	if e.archive == nil {
		return history.Snapshot{}, errors.New("尚未启用长期历史")
	}
	return e.archive.Snapshot(ctx, at)
}
