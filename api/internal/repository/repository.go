package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"signalatlas/api/migrations"
)

var ErrNotFound = errors.New("document not found")
var validKey = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// Documents contains typed application documents, never network credentials.
type Documents interface {
	Read(context.Context, string, any) error
	Write(context.Context, string, any) error
	Kind() string
	Close()
}

type File struct {
	dir string
	mu  sync.RWMutex
}

func NewFile(dir string) (*File, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &File{dir: dir}, nil
}
func (f *File) Kind() string { return "file" }
func (f *File) Close()       {}
func (f *File) Read(ctx context.Context, key string, out any) error {
	if !validKey.MatchString(key) {
		return errors.New("invalid document key")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	b, err := os.ReadFile(filepath.Join(f.dir, key+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
func AtomicWrite(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".write-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, path)
}
func (f *File) Write(ctx context.Context, key string, value any) error {
	if !validKey.MatchString(key) {
		return errors.New("invalid document key")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return AtomicWrite(filepath.Join(f.dir, key+".json"), append(b, '\n'))
}

type Postgres struct {
	pool                    *pgxpool.Pool
	detailDays, summaryDays int
}

func NewPostgres(ctx context.Context, dsn string) (*Postgres, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("PostgreSQL连接配置格式无效")
	}
	cfg.MaxConns = 5
	cfg.MinConns = 0
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.ConnConfig.ConnectTimeout = 8 * time.Second
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, errors.New("无法建立PostgreSQL连接池")
	}
	if err = p.Ping(ctx); err != nil {
		p.Close()
		return nil, fmt.Errorf("PostgreSQL连接失败：%s", safeDBError(err))
	}
	tx, err := p.Begin(ctx)
	if err != nil {
		p.Close()
		return nil, errors.New("数据库事务创建失败")
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(673219854)"); err != nil {
		_ = tx.Rollback(ctx)
		p.Close()
		return nil, errors.New("迁移锁获取失败")
	}
	if _, err = tx.Exec(ctx, migrations.Initial); err != nil {
		_ = tx.Rollback(ctx)
		p.Close()
		return nil, errors.New("无法在signal_atlas schema执行迁移，请检查建表权限")
	}
	if _, err = tx.Exec(ctx, migrations.History); err != nil {
		_ = tx.Rollback(ctx)
		p.Close()
		return nil, errors.New("历史数据库迁移失败")
	}
	if _, err = tx.Exec(ctx, migrations.DeviceTraffic); err != nil {
		_ = tx.Rollback(ctx)
		p.Close()
		return nil, errors.New("设备流量数据库迁移失败")
	}
	if _, err = tx.Exec(ctx, migrations.EntityConfirmed); err != nil {
		_ = tx.Rollback(ctx)
		p.Close()
		return nil, errors.New("最近确认时间数据库迁移失败")
	}
	if _, err = tx.Exec(ctx, migrations.TrafficPeak); err != nil {
		_ = tx.Rollback(ctx)
		p.Close()
		return nil, errors.New("流量峰值数据库迁移失败")
	}
	if _, err = tx.Exec(ctx, migrations.Ping); err != nil {
		_ = tx.Rollback(ctx)
		p.Close()
		return nil, errors.New("时延数据库迁移失败")
	}
	if _, err = tx.Exec(ctx, migrations.DeviceColumns); err != nil {
		_ = tx.Rollback(ctx)
		p.Close()
		return nil, errors.New("设备快照迁移失败")
	}
	if err = tx.Commit(ctx); err != nil {
		p.Close()
		return nil, errors.New("数据库迁移提交失败")
	}
	return &Postgres{pool: p, detailDays: 60, summaryDays: 365}, nil
}
func safeDBError(err error) string {
	return "请检查地址、账号、密码、权限与SSL设置"
}
func (p *Postgres) Kind() string { return "postgresql" }
func (p *Postgres) Close()       { p.pool.Close() }
func (p *Postgres) Read(ctx context.Context, key string, out any) error {
	if !validKey.MatchString(key) {
		return errors.New("invalid document key")
	}

	var b []byte
	err := p.pool.QueryRow(ctx, "SELECT value FROM signal_atlas.documents WHERE key=$1", key).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return errors.New("数据库读取失败")
	}
	return json.Unmarshal(b, out)
}
func (p *Postgres) Write(ctx context.Context, key string, value any) error {
	if !validKey.MatchString(key) {
		return errors.New("invalid document key")
	}
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return errors.New("数据库事务创建失败")
	}
	defer tx.Rollback(ctx)
	// Serialize writers for this document even before its initial INSERT.
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", "signal-atlas-document-"+key); err != nil {
		return errors.New("文档锁获取失败")
	}
	var old []byte
	err = tx.QueryRow(ctx, "SELECT value FROM signal_atlas.documents WHERE key=$1 FOR UPDATE", key).Scan(&old)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return errors.New("旧文档读取失败")
	}
	_, err = tx.Exec(ctx, "INSERT INTO signal_atlas.documents(key,value) VALUES($1,$2) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,updated_at=now()", key, b)
	if err != nil {
		return errors.New("数据库保存失败")
	}
	if err = recordDocumentChange(ctx, tx, key, old, b); err != nil {
		return errors.New("修改记录保存失败，文档未更改")
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("文档事务提交失败")
	}
	return nil
}
