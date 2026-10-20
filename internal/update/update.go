// Package update 检查 GitHub 上是否存在比当前版本更新的 tag。
//
// 实现要点:
//   - 走 GitHub 公开 API 的 tags 接口, 无需 token;
//   - 未认证请求限额 60 次/小时/IP, 因此本地缓存 12 小时, 且用 ETag 条件请求
//     (304 响应不计入限额);
//   - 任何失败都静默降级为"无可用更新", 绝不打断换图主流程。
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"wallpaper-tool/internal/paths"
	"wallpaper-tool/internal/version"
)

// RepoOwner/RepoName 指向 GitHub 仓库。
const (
	RepoOwner = "mo-feibai"
	RepoName  = "wallpaper-tool"

	apiURL     = "https://api.github.com/repos/" + RepoOwner + "/" + RepoName + "/tags"
	releaseURL = "https://github.com/" + RepoOwner + "/" + RepoName + "/releases/latest"

	cacheTTL = 12 * time.Hour
)

// Info 描述一次更新检查的结果。
type Info struct {
	Current     string    `json:"current"`
	Latest      string    `json:"latest"`
	Available   bool      `json:"available"`
	ReleaseURL  string    `json:"release_url"`
	CheckedAt   time.Time `json:"checked_at"`
	FromCache   bool      `json:"from_cache"`
	CheckFailed bool      `json:"check_failed"`
}

// cache 是落盘的检查结果, 避免频繁请求触发 API 限流。
type cache struct {
	ETag      string    `json:"etag"`
	Latest    string    `json:"latest"`
	CheckedAt time.Time `json:"checked_at"`
}

var (
	mu       sync.Mutex
	httpCli  = &http.Client{Timeout: 15 * time.Second}
	memCache *cache
	// lastInfo 保存最近一次检查结果, 供 Web 接口读取。
	lastInfo atomic.Value
)

// SetInfo 记录最近一次检查结果。
func SetInfo(i Info) {
	lastInfo.Store(i)
}

// Last 返回最近一次检查结果; 从未检查过时返回空 Info。
func Last() Info {
	if v, ok := lastInfo.Load().(Info); ok {
		return v
	}
	return Info{Current: version.Current(), ReleaseURL: releaseURL}
}

// Check 查询 GitHub 上最新的 tag, 返回与当前版本的对比结果。
// 失败时返回的 Info 里 CheckFailed 为 true, 调用方应静默忽略。
func Check(ctx context.Context, current string) Info {
	mu.Lock()
	defer mu.Unlock()

	info := Info{Current: current, ReleaseURL: releaseURL}

	c := loadCache()
	if c != nil && time.Since(c.CheckedAt) < cacheTTL {
		info.Latest = c.Latest
		info.CheckedAt = c.CheckedAt
		info.FromCache = true
		applyCompare(&info, current)
		return info
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		info.CheckFailed = true
		return info
	}
	req.Header.Set("User-Agent", "WallpaperTool/"+current)
	req.Header.Set("Accept", "application/vnd.github+json")
	if c != nil && c.ETag != "" {
		req.Header.Set("If-None-Match", c.ETag)
	}

	resp, err := httpCli.Do(req)
	if err != nil {
		info.CheckFailed = true
		if c != nil {
			info.Latest = c.Latest
			info.CheckedAt = c.CheckedAt
			applyCompare(&info, current)
		}
		return info
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotModified:
		// 条件请求命中, 不消耗 API 限额。
		if c == nil {
			info.CheckFailed = true
			return info
		}
		c.CheckedAt = time.Now()
		saveCache(c)
		info.Latest = c.Latest
		info.CheckedAt = c.CheckedAt
		info.FromCache = true
		applyCompare(&info, current)
		return info
	case http.StatusOK:
	default:
		info.CheckFailed = true
		return info
	}

	var tags []struct {
		Name string `json:"name"`
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		info.CheckFailed = true
		return info
	}
	if err := json.Unmarshal(body, &tags); err != nil {
		info.CheckFailed = true
		return info
	}

	latest := pickLatest(tags)
	newCache := &cache{ETag: resp.Header.Get("ETag"), Latest: latest, CheckedAt: time.Now()}
	saveCache(newCache)
	memCache = newCache

	info.Latest = latest
	info.CheckedAt = newCache.CheckedAt
	applyCompare(&info, current)
	return info
}

// applyCompare 根据最新 tag 与当前版本比较, 填充 Available。
func applyCompare(info *Info, current string) {
	if info.Latest == "" {
		return
	}
	info.Available = version.Compare(info.Latest, current) > 0
}

// pickLatest 从 tag 列表里挑出语义化版本最高的一个(忽略非版本 tag)。
func pickLatest(tags []struct {
	Name string `json:"name"`
}) string {
	best := ""
	for _, t := range tags {
		name := strings.TrimSpace(t.Name)
		if name == "" || !strings.HasPrefix(name, "v") {
			continue
		}
		if best == "" || version.Compare(name, best) > 0 {
			best = name
		}
	}
	return best
}

// cachePath 返回检查缓存文件路径。
func cachePath() string {
	return filepath.Join(paths.BaseDir(), "update_cache.json")
}

// loadCache 读取缓存(内存优先, 其次磁盘)。
func loadCache() *cache {
	if memCache != nil {
		return memCache
	}
	data, err := os.ReadFile(cachePath())
	if err != nil {
		return nil
	}
	var c cache
	if err := json.Unmarshal(data, &c); err != nil {
		return nil
	}
	memCache = &c
	return &c
}

// saveCache 写入缓存, 失败不影响主流程。
func saveCache(c *cache) {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(paths.BaseDir(), 0o755); err != nil {
		return
	}
	if err := os.WriteFile(cachePath(), data, 0o644); err != nil {
		return
	}
}

// Describe 返回用于日志的可读描述。
func (i Info) Describe() string {
	if i.CheckFailed {
		return fmt.Sprintf("update check failed (current=%s)", i.Current)
	}
	if i.Available {
		return fmt.Sprintf("update available: %s -> %s", i.Current, i.Latest)
	}
	return fmt.Sprintf("up to date (current=%s latest=%s)", i.Current, i.Latest)
}
