// updater 实现自升级逻辑。
// 从指定 URL 下载新版本二进制文件，校验 MD5，替换当前文件后通知重启。
// 支持主控进程和播放服务的双二进制升级。
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
	"runtime"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// UpdateInfo 升级信息
type UpdateInfo struct {
	Version     string `json:"version"`
	PlaythreadURL string `json:"playthread_url"` // 主控二进制下载地址
	AudioURL    string `json:"audio_url"`       // 播放服务二进制下载地址
	PlaythreadMD5 string `json:"playthread_md5"`
	AudioMD5    string `json:"audio_md5"`
}

// UpdateResult 升级结果
type UpdateResult struct {
	Success          bool   `json:"success"`
	Version          string `json:"version"`
	PlaythreadUpdated bool  `json:"playthread_updated"`
	AudioUpdated     bool   `json:"audio_updated"`
	Error            string `json:"error,omitempty"`
	NeedRestart      bool   `json:"need_restart"`
}

// Updater 自升级管理器
type Updater struct {
	mu         sync.Mutex
	updating   bool
	currentVer string
	httpClient *http.Client
}

// NewUpdater 创建升级管理器
func NewUpdater(currentVersion string) *Updater {
	return &Updater{
		currentVer: currentVersion,
		httpClient: &http.Client{Timeout: 5 * time.Minute},
	}
}

// IsUpdating 是否正在升级中
func (u *Updater) IsUpdating() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.updating
}

// Apply 执行升级操作
// 下载新版二进制 → MD5 校验 → 替换旧文件（旧文件重命名为 .bak）→ 标记需要重启
func (u *Updater) Apply(ctx context.Context, info *UpdateInfo) *UpdateResult {
	u.mu.Lock()
	if u.updating {
		u.mu.Unlock()
		return &UpdateResult{Error: "升级正在进行中"}
	}
	u.updating = true
	u.mu.Unlock()

	defer func() {
		u.mu.Lock()
		u.updating = false
		u.mu.Unlock()
	}()

	result := &UpdateResult{Version: info.Version}
	exePath, err := os.Executable()
	if err != nil {
		result.Error = fmt.Sprintf("获取可执行文件路径失败: %v", err)
		return result
	}
	exeDir := filepath.Dir(exePath)

	// 升级主控进程
	if info.PlaythreadURL != "" {
		targetName := "playthread"
		if runtime.GOOS == "windows" {
			targetName += ".exe"
		}
		targetPath := filepath.Join(exeDir, targetName)

		log.Info().Str("url", info.PlaythreadURL).Str("target", targetPath).Msg("开始下载主控新版本")
		if err := u.downloadAndReplace(ctx, info.PlaythreadURL, info.PlaythreadMD5, targetPath); err != nil {
			result.Error = fmt.Sprintf("升级主控失败: %v", err)
			return result
		}
		result.PlaythreadUpdated = true
		log.Info().Msg("主控二进制已替换")
	}

	// 升级播放服务
	if info.AudioURL != "" {
		targetName := "audio-service"
		if runtime.GOOS == "windows" {
			targetName += ".exe"
		}
		targetPath := filepath.Join(exeDir, targetName)

		log.Info().Str("url", info.AudioURL).Str("target", targetPath).Msg("开始下载播放服务新版本")
		if err := u.downloadAndReplace(ctx, info.AudioURL, info.AudioMD5, targetPath); err != nil {
			result.Error = fmt.Sprintf("升级播放服务失败: %v", err)
			// 主控已升级但播放服务失败，仍需要重启
			if result.PlaythreadUpdated {
				result.NeedRestart = true
			}
			return result
		}
		result.AudioUpdated = true
		log.Info().Msg("播放服务二进制已替换")
	}

	result.Success = true
	result.NeedRestart = result.PlaythreadUpdated || result.AudioUpdated
	return result
}

// downloadAndReplace 下载文件并替换目标路径
// 流程：下载到临时文件 → MD5 校验 → 重命名旧文件为 .bak → 重命名临时文件到目标路径
func (u *Updater) downloadAndReplace(ctx context.Context, url, expectedMD5, targetPath string) error {
	// 下载到临时文件
	tmpPath := targetPath + ".new"
	defer os.Remove(tmpPath) // 清理临时文件

	if err := u.downloadFile(ctx, url, tmpPath); err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}

	// MD5 校验
	if expectedMD5 != "" {
		actualMD5, err := fileMD5(tmpPath)
		if err != nil {
			return fmt.Errorf("计算 MD5 失败: %w", err)
		}
		if actualMD5 != expectedMD5 {
			return fmt.Errorf("MD5 校验失败: expected=%s actual=%s", expectedMD5, actualMD5)
		}
		log.Debug().Str("md5", actualMD5).Msg("MD5 校验通过")
	}

	// 备份旧文件
	bakPath := targetPath + ".bak"
	os.Remove(bakPath)
	if _, err := os.Stat(targetPath); err == nil {
		if err := os.Rename(targetPath, bakPath); err != nil {
			return fmt.Errorf("备份旧文件失败: %w", err)
		}
	}

	// 替换
	if err := os.Rename(tmpPath, targetPath); err != nil {
		// 回滚：恢复备份
		if _, bakErr := os.Stat(bakPath); bakErr == nil {
			os.Rename(bakPath, targetPath)
		}
		return fmt.Errorf("替换文件失败: %w", err)
	}

	return nil
}

// downloadFile 下载文件到本地路径
func (u *Updater) downloadFile(ctx context.Context, url, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}
	return f.Sync()
}

// fileMD5 计算文件 MD5 哈希
func fileMD5(path string) (string, error) {
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
