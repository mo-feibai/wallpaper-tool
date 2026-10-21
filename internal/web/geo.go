package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// 位置检测(ip-api 风格的服务)相关。
const (
	// geoURL 免费额度无需 key 的 IP 定位服务(城市级精度)。
	// 其免费接口只提供 HTTP, 这里查询的只是不含任何凭据的粗略坐标。
	geoURL   = "http://ip-api.com/json/?fields=status,message,lat,lon,city"
	geoCache = 30 * time.Minute
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
}

// handleThemeGeo 按当前出口 IP 粗略定位(城市级), 供页面一键填入经纬度。
//
// 只用于填充日出日落计算所需的坐标, 不做精确定位;
// 失败时返回错误, 页面可改为手动填写。
func (s *Server) handleThemeGeo(w http.ResponseWriter, r *http.Request) {
	geoMu.Lock()
	if geoCached != nil && time.Since(geoCachedAt) < geoCache {
		out := *geoCached
		geoMu.Unlock()
		writeJSON(w, out)
		return
	}
	geoMu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, geoURL, nil)
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	req.Header.Set("User-Agent", "WallpaperTool/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeJSON(w, map[string]any{"error": fmt.Sprintf("位置检测失败: %v", err)})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		writeJSON(w, map[string]any{"error": fmt.Sprintf("位置服务返回 %d", resp.StatusCode)})
		return
	}
	var raw struct {
		Status    string   `json:"status"`
		Message   string   `json:"message"`
		Latitude  *float64 `json:"lat"`
		Longitude *float64 `json:"lon"`
		City      string   `json:"city"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		writeJSON(w, map[string]any{"error": "解析位置服务响应失败"})
		return
	}
	if raw.Status != "success" {
		writeJSON(w, map[string]any{"error": "位置服务: " + raw.Message})
		return
	}
	if raw.Latitude == nil || raw.Longitude == nil {
		writeJSON(w, map[string]any{"error": "位置服务未返回坐标"})
		return
	}
	out := geoResult{
		Latitude:  *raw.Latitude,
		Longitude: *raw.Longitude,
		City:      raw.City,
	}
	geoMu.Lock()
	geoCached = &out
	geoCachedAt = time.Now()
	geoMu.Unlock()
	writeJSON(w, out)
}
