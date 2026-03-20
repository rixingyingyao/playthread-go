package infra

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/time/rate"
)

// FileCacheConfig 文件缓存配置
type FileCacheConfig struct {
	CacheDir       string        `yaml:"cache_dir"`        // 缓存根目录
	MaxSizeMB      int64         `yaml:"max_size_mb"`      // 缓存最大容量(MB)，默认 2048
	RateLimitBytes int           `yaml:"rate_limit_bytes"`  // 下载限速(bytes/s)，默认 5MB/s
	DownloadTimeout time.Duration `yaml:"download_timeout"` // 单文件下载超时，默认 10min
	RetryCount     int           `yaml:"retry_count"`       // 下载重试次数，默认 3
	RetryDelay     time.Duration `yaml:"retry_delay"`       // 重试延迟，默认 2s
}

// DefaultFileCacheConfig 默认配置
func DefaultFileCacheConfig() FileCacheConfig {
	return FileCacheConfig{
		CacheDir:        "cache/media",
		MaxSizeMB:       2048,
		RateLimitBytes:  5 * 1024 * 1024,
		DownloadTimeout: 10 * time.Minute,
		RetryCount:      3,
		RetryDelay:      2 * time.Second,
	}
}

// fileEntry LRU 条目
type fileEntry struct {
	Path       string
	Size       int64
	AccessTime time.Time
}

// FileCache 素材文件缓存管理器
// 支持断点续传、MD5 校验、令牌桶限速、LRU 清理
type FileCache struct {
	mu      sync.Mutex
	cfg     FileCacheConfig
	limiter *rate.Limiter
	client  *http.Client
}

// NewFileCache 创建文件缓存管理器
func NewFileCache(cfg FileCacheConfig) *FileCache {
	if err := os.MkdirAll(cfg.CacheDir, 0o755); err != nil {
		log.Warn().Err(err).Str("dir", cfg.CacheDir).Msg("创建缓存目录失败")
	}

	burstSize := 1024 * 1024 // 1MB burst
	if cfg.RateLimitBytes < burstSize {
		burstSize = cfg.RateLimitBytes
	}

	return &FileCache{
		cfg:     cfg,
		limiter: rate.NewLimiter(rate.Limit(cfg.RateLimitBytes), burstSize),
		client:  &http.Client{Timeout: cfg.DownloadTimeout},
	}
}

// Exists 检查文件是否在缓存中
func (fc *FileCache) Exists(relativePath string) bool {
	p := fc.absPath(relativePath)
	_, err := os.Stat(p)
	return err == nil
}

// AbsPath 返回缓存文件的绝对路径
func (fc *FileCache) AbsPath(relativePath string) string {
	return fc.absPath(relativePath)
}

func (fc *FileCache) absPath(relativePath string) string {
	return filepath.Join(fc.cfg.CacheDir, filepath.Clean(relativePath))
}

// Download 下载文件到缓存
// 支持断点续传：如果本地存在部分文件且 url 支持 Range，则续传。
// 下载完成后用 expectedMD5（如果非空）校验完整性。
func (fc *FileCache) Download(ctx context.Context, url, relativePath, expectedMD5 string) error {
	destPath := fc.absPath(relativePath)
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("创建缓存子目录失败: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= fc.cfg.RetryCount; attempt++ {
		if attempt > 0 {
			log.Info().
				Int("attempt", attempt).
				Str("file", relativePath).
				Msg("下载重试")
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(fc.cfg.RetryDelay):
			}
		}

		lastErr = fc.downloadOnce(ctx, url, destPath, expectedMD5)
		if lastErr == nil {
			return nil
		}

		log.Warn().Err(lastErr).
			Str("file", relativePath).
			Int("attempt", attempt+1).
			Msg("下载失败")
	}
	return fmt.Errorf("下载 %s 最终失败: %w", relativePath, lastErr)
}

// downloadOnce 单次下载尝试（支持断点续传）
func (fc *FileCache) downloadOnce(ctx context.Context, url, destPath, expectedMD5 string) error {
	tmpPath := destPath + ".downloading"

	// 检查是否存在部分下载的文件
	var offset int64
	if info, err := os.Stat(tmpPath); err == nil {
		offset = info.Size()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("创建下载请求失败: %w", err)
	}

	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}

	resp, err := fc.client.Do(req)
	if err != nil {
		return fmt.Errorf("下载请求失败: %w", err)
	}
	defer resp.Body.Close()

	var f *os.File
	switch resp.StatusCode {
	case http.StatusOK:
		// 服务器返回完整文件，覆盖
		offset = 0
		f, err = os.Create(tmpPath)
	case http.StatusPartialContent:
		// 续传
		f, err = os.OpenFile(tmpPath, os.O_APPEND|os.O_WRONLY, 0o644)
	default:
		return fmt.Errorf("服务器返回 %d", resp.StatusCode)
	}
	if err != nil {
		return fmt.Errorf("打开临时文件失败: %w", err)
	}
	defer f.Close()

	// 限速写入
	written, err := fc.rateLimitedCopy(ctx, f, resp.Body)
	if err != nil {
		return fmt.Errorf("写入失败（已写 %d bytes）: %w", written, err)
	}

	// 确保数据落盘
	if err := f.Sync(); err != nil {
		return fmt.Errorf("fsync 失败: %w", err)
	}
	f.Close()

	// MD5 校验
	if expectedMD5 != "" {
		actualMD5, err := fc.fileMD5(tmpPath)
		if err != nil {
			return fmt.Errorf("计算 MD5 失败: %w", err)
		}
		if actualMD5 != expectedMD5 {
			os.Remove(tmpPath)
			return fmt.Errorf("MD5 校验失败: expected=%s, actual=%s", expectedMD5, actualMD5)
		}
	}

	// 原子移动到最终路径
	_ = os.Remove(destPath)
	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("重命名到最终路径失败: %w", err)
	}

	log.Info().
		Str("path", destPath).
		Int64("offset", offset).
		Int64("written", written).
		Msg("文件下载完成")

	return nil
}

// rateLimitedCopy 限速复制
func (fc *FileCache) rateLimitedCopy(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, 32*1024) // 32KB buffer
	var total int64

	for {
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		default:
		}

		// 等待令牌
		if err := fc.limiter.WaitN(ctx, len(buf)); err != nil {
			return total, err
		}

		n, err := src.Read(buf)
		if n > 0 {
			nw, ew := dst.Write(buf[:n])
			total += int64(nw)
			if ew != nil {
				return total, ew
			}
		}
		if err != nil {
			if err == io.EOF {
				return total, nil
			}
			return total, err
		}
	}
}

// fileMD5 计算文件 MD5
func (fc *FileCache) fileMD5(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// CleanupLRU 执行 LRU 清理，移除最旧的文件直到总大小低于 MaxSizeMB
// 返回清理的文件数
func (fc *FileCache) CleanupLRU() (int, error) {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	entries, totalSize, err := fc.scanCache()
	if err != nil {
		return 0, fmt.Errorf("扫描缓存目录失败: %w", err)
	}

	maxBytes := fc.cfg.MaxSizeMB * 1024 * 1024
	if totalSize <= maxBytes {
		return 0, nil
	}

	// 按访问时间升序排序（最旧的在前）
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].AccessTime.Before(entries[j].AccessTime)
	})

	removed := 0
	for _, e := range entries {
		if totalSize <= maxBytes {
			break
		}
		if err := os.Remove(e.Path); err != nil {
			log.Warn().Err(err).Str("path", e.Path).Msg("LRU 清理删除失败")
			continue
		}
		totalSize -= e.Size
		removed++
		log.Debug().Str("path", e.Path).Int64("size", e.Size).Msg("LRU 清理")
	}

	log.Info().
		Int("removed", removed).
		Int64("current_mb", totalSize/(1024*1024)).
		Int64("max_mb", fc.cfg.MaxSizeMB).
		Msg("LRU 清理完成")

	return removed, nil
}

// CacheSize 返回当前缓存总大小 (bytes)
func (fc *FileCache) CacheSize() (int64, error) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	_, totalSize, err := fc.scanCache()
	return totalSize, err
}

// scanCache 扫描缓存目录，返回所有文件条目和总大小
func (fc *FileCache) scanCache() ([]fileEntry, int64, error) {
	var entries []fileEntry
	var totalSize int64

	err := filepath.Walk(fc.cfg.CacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // 跳过不可访问的文件
		}
		if info.IsDir() {
			return nil
		}
		// 跳过正在下载的临时文件
		if filepath.Ext(path) == ".downloading" {
			return nil
		}
		entries = append(entries, fileEntry{
			Path:       path,
			Size:       info.Size(),
			AccessTime: info.ModTime(), // 使用 ModTime 作为 access time（Windows 不一定有 atime）
		})
		totalSize += info.Size()
		return nil
	})

	return entries, totalSize, err
}

// Touch 更新文件访问时间（用于 LRU）
func (fc *FileCache) Touch(relativePath string) {
	p := fc.absPath(relativePath)
	now := time.Now()
	_ = os.Chtimes(p, now, now)
}

// Remove 移除缓存文件
func (fc *FileCache) Remove(relativePath string) error {
	return os.Remove(fc.absPath(relativePath))
}
