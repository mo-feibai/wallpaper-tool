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
	"time"

	"wallpaper-tool/internal/config"
	"wallpaper-tool/internal/netproxy"
	"wallpaper-tool/internal/source"
)

const baseURL = "https://wallhaven.cc/api/v1"
const userAgent = "WallpaperTool/0.2"

// Name 数据源名。
const Name = "wallhaven"

// photo 搜索结果中的单个壁纸条目(API 原始结构)。
type photo struct {
	ID         string `json:"id"`
	URL        string `json:"url"`         // 页面地址
	Path       string `json:"path"`        // 原图直链
	FileType   string `json:"file_type"`   // image/jpeg / image/png
	DimensionX int    `json:"dimension_x"` // 宽
	DimensionY int    `json:"dimension_y"` // 高
}

// Meta 分页元信息。
type Meta struct {
	CurrentPage int    `json:"current_page"`
	LastPage    int    `json:"last_page"`
	Seed        string `json:"seed"`
}

// SearchResult 搜索响应。
type SearchResult struct {
	Data []photo `json:"data"`
	Meta Meta    `json:"meta"`
}

// Client 带节流的 HTTP 客户端, 实现 source.Source 接口。
// 代理优先级: 显式配置 > Windows 系统代理 > 环境变量 > 直连。
type Client struct {
	http    *http.Client
	log     *log.Logger
	proxy   *netproxy.Resolver
	mu      sync.Mutex
	lastReq time.Time

	muCfg sync.RWMutex
	cfg   config.Config
}

// NewClient 创建客户端。
func NewClient(lg *log.Logger) *Client {
	c := &Client{
		log:   lg,
		proxy: netproxy.New(),
	}
	c.http = &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			Proxy: c.proxy.Proxy,
		},
	}
	return c
}

// Name 实现 source.Source。
func (c *Client) Name() string { return Name }

// Update 实现 source.Source: 记录最新配置。
func (c *Client) Update(cfg config.Config) {
	c.muCfg.Lock()
	c.cfg = cfg
	c.muCfg.Unlock()
}

// SetProxy 更新显式代理地址(空串表示跟随系统代理)。
func (c *Client) SetProxy(addr string) {
	c.proxy.SetOverride(addr)
}

func (c *Client) currentCfg() config.Config {
	c.muCfg.RLock()
	defer c.muCfg.RUnlock()
	return c.cfg
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

// Search 实现 source.Source。
// sorting=random 时传入 seed 可在同一随机序列内翻页避免重复。
func (c *Client) Search(q source.Query) (source.Result, error) {
	var out source.Result
	cfg := c.currentCfg()
	page := q.Page
	if page < 1 {
		page = 1
	}
	keyword := q.Keyword
	if keyword == "" {
		keyword = cfg.Query
	}
	orientation := q.Orientation
	if orientation == "" {
		orientation = cfg.Orientation
	}
	atLeast := cfg.AtLeast
	if q.MinWidth > 0 && q.MinHeight > 0 {
		atLeast = fmt.Sprintf("%dx%d", q.MinWidth, q.MinHeight)
	}

	v := url.Values{}
	v.Set("categories", categoriesParam(cfg.Categories))
	v.Set("purity", purityParam(cfg.Purity))
	v.Set("sorting", cfg.Sorting)
	v.Set("order", cfg.Order)
	if keyword != "" {
		v.Set("q", keyword)
	}
	if cfg.Sorting == "toplist" {
		v.Set("topRange", cfg.TopRange)
	}
	if atLeast != "" {
		v.Set("atleast", atLeast)
	}
	// 方向选择(横屏/竖屏)优先于自定义宽高比; wallhaven API 支持 landscape/portrait 关键词
	if orientation == "landscape" || orientation == "portrait" {
		v.Set("ratios", orientation)
	} else if cfg.Ratios != "" {
		v.Set("ratios", cfg.Ratios)
	}
	v.Set("page", strconv.Itoa(page))
	if cfg.Sorting == "random" && q.Seed != "" {
		v.Set("seed", q.Seed)
	}
	searchURL := baseURL + "/search?" + v.Encode()

	res, err := c.doSearch(searchURL)
	if err != nil {
		return out, err
	}
	out.LastPage = res.Meta.LastPage
	out.Seed = res.Meta.Seed
	for _, p := range res.Data {
		out.Items = append(out.Items, source.Item{
			ID:       p.ID,
			Source:   Name,
			FileURL:  p.Path,
			PageURL:  p.URL,
			FileType: p.FileType,
			Width:    p.DimensionX,
			Height:   p.DimensionY,
		})
	}
	return out, nil
}

// doSearch 执行搜索请求(含 429 退避重试)。
func (c *Client) doSearch(searchURL string) (SearchResult, error) {
	var out SearchResult
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

// Download 实现 source.Source: 下载原图到 destDir, 文件名为 wallhaven-<id>.<ext>。
// 已存在且大小合理的文件直接复用; 先写临时文件再改名, 避免半成品。
func (c *Client) Download(item source.Item, destDir string) (string, error) {
	if item.FileURL == "" {
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
	req, err := http.NewRequest(http.MethodGet, item.FileURL, nil)
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
		return "", fmt.Errorf("下载 http %d: %s", resp.StatusCode, item.FileURL)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "image/") {
		return "", fmt.Errorf("响应不是图片: %s", ct)
	}
	return source.SaveImage(dest, resp.Body, c.log)
}
