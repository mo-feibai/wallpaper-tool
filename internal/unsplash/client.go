// Package unsplash 实现 Unsplash 官方 API 数据源。
// 文档: https://unsplash.com/documentation
// 认证: Authorization: Client-ID <Access Key>(key 不进 URL, 避免出现在日志与错误信息中)
// 限流: demo 模式 50 次/小时(仅计 api.unsplash.com 的 JSON 请求, 图片下载不计);
//       随机端点一次可取 30 张, 因此每批只消耗 1 次配额。
package unsplash

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

const baseURL = "https://api.unsplash.com"
const userAgent = "WallpaperTool/0.2"

// Name 数据源名。
const Name = "unsplash"

// batchSize 单次随机请求取图数量(random 端点上限 30)。
const batchSize = 30

// photo Unsplash 照片原始结构(仅取需要的字段)。
type photo struct {
	ID     string `json:"id"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	URLs   struct {
		Raw string `json:"raw"`
	} `json:"urls"`
	Links struct {
		HTML string `json:"html"`
	} `json:"links"`
	User struct {
		Name     string `json:"name"`
		Username string `json:"username"`
	} `json:"user"`
	Description string `json:"description"`
	AltDesc     string `json:"alt_description"`
}

// Client Unsplash 数据源客户端, 实现 source.Source 接口。
type Client struct {
	http  *http.Client
	log   *log.Logger
	proxy *netproxy.Resolver

	muCfg sync.RWMutex
	cfg   config.Config
	key   string
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

// Update 实现 source.Source: 记录最新配置(含 Access Key)。
func (c *Client) Update(cfg config.Config) {
	c.muCfg.Lock()
	c.cfg = cfg
	c.key = strings.TrimSpace(cfg.UnsplashKey)
	c.muCfg.Unlock()
}

// SetProxy 更新显式代理地址。
func (c *Client) SetProxy(addr string) {
	c.proxy.SetOverride(addr)
}

func (c *Client) currentKey() string {
	c.muCfg.RLock()
	defer c.muCfg.RUnlock()
	return c.key
}

// ErrNoKey 未配置 Access Key。
var ErrNoKey = errors.New("未配置 Unsplash Access Key, 请在配置页填写")

// Search 实现 source.Source: 取一批随机图(默认 30 张)。
// 随机端点不提供分页, 因此 LastPage 恒为 1, Seed 为空。
func (c *Client) Search(q source.Query) (source.Result, error) {
	var out source.Result
	key := c.currentKey()
	if key == "" {
		return out, ErrNoKey
	}
	v := url.Values{}
	v.Set("count", strconv.Itoa(batchSize))
	if q.Orientation == "landscape" || q.Orientation == "portrait" || q.Orientation == "squarish" {
		v.Set("orientation", q.Orientation)
	}
	if q.Keyword != "" {
		v.Set("query", q.Keyword)
	}
	// content_filter 保持默认 low, 避免出现不适合作为壁纸的内容
	req, err := http.NewRequest(http.MethodGet, baseURL+"/photos/random?"+v.Encode(), nil)
	if err != nil {
		return out, err
	}
	c.authorize(req, key)
	resp, err := c.http.Do(req)
	if err != nil {
		return out, fmt.Errorf("Unsplash 请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return out, errors.New("Unsplash Access Key 无效(401), 请检查配置页中的 key")
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return out, errors.New("Unsplash 限流(429), demo 模式每小时 50 次, 请稍后重试")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return out, fmt.Errorf("Unsplash http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var photos []photo
	if err := json.NewDecoder(resp.Body).Decode(&photos); err != nil {
		return out, fmt.Errorf("解析 Unsplash 响应失败: %w", err)
	}
	for _, p := range photos {
		if p.URLs.Raw == "" {
			continue
		}
		// Unsplash 不支持按分辨率筛选, 这里按返回的原始尺寸本地过滤
		if q.MinWidth > 0 && p.Width < q.MinWidth {
			continue
		}
		if q.MinHeight > 0 && p.Height < q.MinHeight {
			continue
		}
		title := p.Description
		if title == "" {
			title = p.AltDesc
		}
		out.Items = append(out.Items, source.Item{
			ID:       p.ID,
			Source:   Name,
			FileURL:  p.URLs.Raw,
			PageURL:  p.Links.HTML,
			FileType: "image/jpeg",
			Title:    title,
			Author:   p.User.Name,
			Width:    p.Width,
			Height:   p.Height,
		})
	}
	out.LastPage = 1
	return out, nil
}

// Download 实现 source.Source: 下载原图到 destDir, 文件名为 unsplash-<id>.jpg。
// 先调用 /photos/{id}/download 跟踪下载(官方要求), 失败不阻断;
// 实际取图用 urls.raw 加上尺寸参数, 保留 ixid 以便统计浏览量。
func (c *Client) Download(item source.Item, destDir string) (string, error) {
	if item.FileURL == "" {
		return "", errors.New("下载地址为空")
	}
	key := c.currentKey()
	if key == "" {
		return "", ErrNoKey
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("创建缓存目录失败: %w", err)
	}
	dest := filepath.Join(destDir, "unsplash-"+item.ID+".jpg")
	if fi, err := os.Stat(dest); err == nil && fi.Size() > 10*1024 {
		return dest, nil
	}
	c.trackDownload(item.ID, key)

	raw := item.FileURL
	if !strings.Contains(raw, "w=") {
		raw += "&w=3840&q=85&fm=jpg&fit=max"
	}
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("下载 Unsplash 原图失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载 http %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "image/") {
		return "", fmt.Errorf("响应不是图片: %s", ct)
	}
	return source.SaveImage(dest, resp.Body, c.log)
}

// trackDownload 调用下载跟踪端点, 失败只记日志不阻断下载。
func (c *Client) trackDownload(id, key string) {
	req, err := http.NewRequest(http.MethodGet, baseURL+"/photos/"+url.PathEscape(id)+"/download", nil)
	if err != nil {
		return
	}
	c.authorize(req, key)
	resp, err := c.http.Do(req)
	if err != nil {
		if c.log != nil {
			c.log.Printf("unsplash 下载跟踪失败(忽略): %v", err)
		}
		return
	}
	// 跟踪端点会 302 到图片地址, 这里不下载图片, 只消费重定向
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	resp.Body.Close()
}

// authorize 附加 Access Key 认证头。key 只出现在请求头, 不进 URL。
func (c *Client) authorize(req *http.Request, key string) {
	req.Header.Set("Authorization", "Client-ID "+key)
	req.Header.Set("Accept-Version", "v1")
	req.Header.Set("User-Agent", userAgent)
}
