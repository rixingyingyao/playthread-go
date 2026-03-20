package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/rs/zerolog/log"
	_ "modernc.org/sqlite"
)

// DB 封装 SQLite 数据库连接，提供串行化写入。
// SQLite 不支持并发写入，所有写操作通过 writeCh 串行化到单一 goroutine。
type DB struct {
	conn    *sql.DB
	writeCh chan writeJob
	wg      sync.WaitGroup
	mu      sync.Mutex // 保护 closed 和 writeCh 的 send，防止向已关闭 channel 发送
	closed  bool
}

type writeJob struct {
	fn    func(*sql.DB) error
	errCh chan error
}

// Open 打开 SQLite 数据库（WAL 模式 + 单写入连接）
func Open(path string) (*DB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建数据库目录失败: %w", err)
	}

	dsn := path + "?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL"
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	conn.SetMaxOpenConns(1)

	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("数据库连接测试失败: %w", err)
	}

	d := &DB{
		conn:    conn,
		writeCh: make(chan writeJob, 64),
	}
	d.wg.Add(1)
	go d.writeLoop()

	log.Info().Str("path", path).Msg("数据库连接已打开")
	return d, nil
}

// Conn 返回底层 sql.DB（用于只读查询）
func (d *DB) Conn() *sql.DB {
	return d.conn
}

// Write 提交写操作到串行化队列并等待完成
func (d *DB) Write(fn func(*sql.DB) error) error {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return fmt.Errorf("数据库已关闭")
	}
	errCh := make(chan error, 1)
	d.writeCh <- writeJob{fn: fn, errCh: errCh}
	d.mu.Unlock()
	return <-errCh
}

// WriteContext 带上下文的写操作
func (d *DB) WriteContext(ctx context.Context, fn func(*sql.DB) error) error {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return fmt.Errorf("数据库已关闭")
	}
	errCh := make(chan error, 1)
	select {
	case d.writeCh <- writeJob{fn: fn, errCh: errCh}:
		d.mu.Unlock()
	case <-ctx.Done():
		d.mu.Unlock()
		return ctx.Err()
	}
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *DB) writeLoop() {
	defer d.wg.Done()
	for job := range d.writeCh {
		job.errCh <- job.fn(d.conn)
	}
}

// Close 关闭数据库连接
func (d *DB) Close() error {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil
	}
	d.closed = true
	close(d.writeCh)
	d.mu.Unlock()

	d.wg.Wait()
	if err := d.conn.Close(); err != nil {
		return fmt.Errorf("关闭数据库失败: %w", err)
	}
	log.Info().Msg("数据库连接已关闭")
	return nil
}
