package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// 位置检测相关。
const (
	// geoIPURLs 取"真实出口 IP"的服务。
	//
	// 为什么不用常见的 ip-api.com/json/ (自省模式): 开启 TUN 模式的代理会
	// 在网络层接管全部流量, 直连请求也会被转发到代理节点, 于是自省拿到的
	// 是节点的位置(实测拿到 Hong Kong)。
	// 这些国内服务在代理规则里走 DIRECT, 返回的是本机真实出口 IP。
	geoIPURL  = "https://ip.3322.net"
	geoIPURL2 = "http://ip.3322.net"
	// geoLookupURL 按指定 IP 查地理位置。
	// 按 IP 查询时, 请求本身从哪出去不影响结果(查的是那个 IP 的位置)。
	geoLookupURL = "http://ip-api.com/json/"
	geoCache     = 30 * time.Minute
)

var (
	geoMu       sync.Mutex
	geoCached   *geoResult
	geoCachedAt time.Time
)

// geoResult 是一次位置检测的结果。
type geoResult struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	City      string  `json:"city"`
	Region    string  `json:"region"`
	IP        string  `json:"ip"`
	// ViaProxy true 表示拿到的是代理节点的位置(未能取到真实出口 IP)。
	ViaProxy bool `json:"via_proxy"`
}

// handleThemeGeo 按出口 IP 粗略定位(城市级), 供页面一键填入经纬度。
//
// 只用于填充日出日落计算所需的坐标, 不做精确定位;
// 失败时返回错误, 页面可改为手动选择城市。
func (s *Server) handleThemeGeo(w http.ResponseWriter, r *http.Request) {
	geoMu.Lock()
	if geoCached != nil && time.Since(geoCachedAt) < geoCache {
		out := *geoCached
		geoMu.Unlock()
		writeJSON(w, out)
		return
	}
	geoMu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()

	ip, err := fetchExitIP(ctx)
	if err != nil {
		writeJSON(w, map[string]any{"error": fmt.Sprintf("获取出口 IP 失败: %v", err)})
		return
	}
	out, err := lookupGeo(ctx, ip)
	if err != nil {
		writeJSON(w, map[string]any{"error": fmt.Sprintf("查询位置失败: %v", err)})
		return
	}
	geoMu.Lock()
	geoCached = &out
	geoCachedAt = time.Now()
	geoMu.Unlock()
	writeJSON(w, out)
}

// fetchExitIP 取本机真实出口 IP。
func fetchExitIP(ctx context.Context) (string, error) {
	var lastErr error
	for _, u := range []string{geoIPURL, geoIPURL2} {
		ip, err := fetchIPFrom(ctx, u)
		if err == nil {
			return ip, nil
		}
		lastErr = err
	}
	return "", lastErr
}

// fetchIPFrom 从指定服务读取纯文本 IP。
func fetchIPFrom(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s 返回 %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		return "", err
	}
	m := regexp.MustCompile(`\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`).FindString(string(body))
	if m == "" {
		return "", fmt.Errorf("%s 未返回 IP", url)
	}
	return m, nil
}

// lookupGeo 按 IP 查询地理位置。
func lookupGeo(ctx context.Context, ip string) (geoResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, geoLookupURL+ip, nil)
	if err != nil {
		return geoResult{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return geoResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return geoResult{}, fmt.Errorf("位置服务返回 %d", resp.StatusCode)
	}
	var raw struct {
		Status     string  `json:"status"`
		Message    string  `json:"message"`
		City       string  `json:"city"`
		RegionName string  `json:"regionName"`
		Latitude   float64 `json:"lat"`
		Longitude  float64 `json:"lon"`
		Query      string  `json:"query"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return geoResult{}, fmt.Errorf("解析位置服务响应失败")
	}
	if raw.Status != "success" {
		return geoResult{}, fmt.Errorf("位置服务: %s", raw.Message)
	}
	return geoResult{
		Latitude:  raw.Latitude,
		Longitude: raw.Longitude,
		City:      raw.City,
		Region:    raw.RegionName,
		IP:        raw.Query,
	}, nil
}

// geoCityText 返回用于展示的城市描述。
func geoCityText(city, region string) string {
	city = strings.TrimSpace(city)
	region = strings.TrimSpace(region)
	switch {
	case city != "" && region != "":
		return region + " " + city
	case city != "":
		return city
	case region != "":
		return region
	default:
		return "当前位置"
	}
}
