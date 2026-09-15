// Package wallhaven 实现 wallhaven.cc 匿名 API 客户端。
// 接口文档: https://wallhaven.cc/help/api
// 限流: 45 次/分钟, 此处按 1.5 秒/次节流。
package wallhaven

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"wallpaper-tool/internal/config"
)

const baseURL = "https://wallhaven.cc/api/v1"
const userAgent = "WallpaperTool/0.1"

// Item 搜索结果中的单个壁纸条目。
type Item struct {
	ID         string `json:"id"`
	URL        string `json:"url"`         // 页面地址
	Path       string `json:"path"`        // 原图直链
	FileType   string `json:"file_type"`   // image/jpeg / image/png
	DimensionX int    `json:"dimension_x"` // 宽
	DimensionY int    `json:"dimension_y"` // 高
	Purity     string `json:"purity"`
	Category   string `json:"category"`
}

// Meta 分页元信息。
type Meta struct {
	CurrentPage int    `json:"current_page"`
	LastPage    int    `json:"last_page"`
	Seed        string `json:"seed"`
}

// SearchResult 搜索响应。
type SearchResult struct {
	Data []Item `json:"data"`
	Meta Meta   `json:"meta"`
}

// Client 带节流的 HTTP 客户端。
// 代理优先级: 显式配置 > Windows 系统代理 > 环境变量 > 直连。
type Client struct {
	http          *http.Client
	log           *log.Logger
	mu            sync.Mutex
	lastReq       time.Time
	proxyOverride atomic.Value // string, 显式代理地址
}

// NewClient 创建客户端。
func NewClient(lg *log.Logger) *Client {
	c := &Client{
		log: lg,
	}
	c.http = &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			Proxy: c.proxyForRequest,
		},
	}
	return c
}

// SetProxy 更新显式代理地址(空串表示跟随系统代理)。
func (c *Client) SetProxy(addr string) {
	c.proxyOverride.Store(addr)
}

// proxyForRequest 每次请求时解析实际使用的代理。
func (c *Client) proxyForRequest(req *http.Request) (*url.URL, error) {
	if v := c.proxyOverride.Load(); v != nil {
		if s, _ := v.(string); s != "" {
			return url.Parse(s)
		}
	}
	if u := systemProxy(); u != nil {
		return u, nil
	}
	return http.ProxyFromEnvironment(req)
}

// throttle 保证相邻两次请求间隔至少 1.5 秒(约 40 次/分钟, 低于官方 45 上限)。
func (c *Client) throttle() {
	c.mu.Lock()
	defer c.mu.Unlock()
	wait := time.Until(c.lastReq.Add(1500 * time.Millisecond))
	if wait > 0 {
		time.Sleep(wait)
	}
	c.lastReq = time.Now()
}

func categoriesParam(c config.Categories) string {
	f := func(b bool) string {
		if b {
			return "1"
		}
		return "0"
	}
	return f(c.General) + f(c.Anime) + f(c.People)
}

// purityParam 匿名接口永远不带 NSFW 位(第三位恒为 0)。
func purityParam(p config.Purity) string {
	f := func(b bool) string {
		if b {
			return "1"
		}
		return "0"
	}
	return f(p.SFW) + f(p.Sketchy) + "0"
}

// Search 执行一次搜索。
// sorting=random 时传入 seed 可在同一随机序列内翻页避免重复。
func (c *Client) Search(cfg config.Config, page int, seed string) (SearchResult, error) {
	var out SearchResult
	if page < 1 {
		page = 1
	}
	v := url.Values{}
	v.Set("categories", categoriesParam(cfg.Categories))
	v.Set("purity", purityParam(cfg.Purity))
	v.Set("sorting", cfg.Sorting)
	v.Set("order", cfg.Order)
	if cfg.Query != "" {
		v.Set("q", cfg.Query)
	}
	if cfg.Sorting == "toplist" {
		v.Set("topRange", cfg.TopRange)
	}
	if cfg.AtLeast != "" {
		v.Set("atleast", cfg.AtLeast)
	}
	// 方向选择(横屏/竖屏)优先于自定义宽高比; wallhaven API 支持 landscape/portrait 关键词
	if cfg.Orientation == "landscape" || cfg.Orientation == "portrait" {
		v.Set("ratios", cfg.Orientation)
	} else if cfg.Ratios != "" {
		v.Set("ratios", cfg.Ratios)
	}
	v.Set("page", strconv.Itoa(page))
	if cfg.Sorting == "random" && seed != "" {
		v.Set("seed", seed)
	}
	searchURL := baseURL + "/search?" + v.Encode()

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		c.throttle()
		req, err := http.NewRequest(http.MethodGet, searchURL, nil)
		if err != nil {
			return out, err
		}
		req.Header.Set("User-Agent", userAgent)
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("搜索请求失败: %w", err)
			time.Sleep(3 * time.Second)
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			lastErr = errors.New("wallhaven 限流(429), 等待 60 秒后重试")
			time.Sleep(60 * time.Second)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			resp.Body.Close()
			return out, fmt.Errorf("搜索 http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		err = json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if err != nil {
			return out, fmt.Errorf("解析搜索结果失败: %w", err)
		}
		return out, nil
	}
	return out, lastErr
}

func extFromFileType(ft string) string {
	if ft == "image/png" {
		return ".png"
	}
	return ".jpg"
}

// Download 下载原图到 destDir, 文件名为 wallhaven-<id>.<ext>。
// 已存在且大小合理的文件直接复用; 先写临时文件再改名, 避免半成品。
func (c *Client) Download(item Item, destDir string) (string, error) {
	if item.Path == "" {
		return "", errors.New("下载地址为空")
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("创建缓存目录失败: %w", err)
	}
	dest := filepath.Join(destDir, "wallhaven-"+item.ID+extFromFileType(item.FileType))
	if fi, err := os.Stat(dest); err == nil && fi.Size() > 10*1024 {
		return dest, nil
	}
	c.throttle()
	req, err := http.NewRequest(http.MethodGet, item.Path, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("下载原图失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载 http %d: %s", resp.StatusCode, item.Path)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "image/") {
		return "", fmt.Errorf("响应不是图片: %s", ct)
	}
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	n, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return "", copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return "", closeErr
	}
	if n < 10*1024 {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("文件过小(%d 字节), 疑似无效内容", n)
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if c.log != nil {
		c.log.Printf("downloaded %s (%d KB)", dest, n/1024)
	}
	return dest, nil
}
