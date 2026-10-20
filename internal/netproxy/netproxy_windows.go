//go:build windows

package netproxy

import (
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows/registry"
)

var (
	sysProxyMu      sync.Mutex
	sysProxyCache   *url.URL
	sysProxyChecked time.Time
)

// SystemProxy 读取 Windows 系统代理(IE/WinINET 设置, 各类代理客户端都会写入)。
// 读取结果缓存 60 秒, 避免每次请求都查注册表。
// 返回 nil 表示系统未启用代理。
func SystemProxy() *url.URL {
	sysProxyMu.Lock()
	defer sysProxyMu.Unlock()
	if time.Since(sysProxyChecked) < 60*time.Second {
		return sysProxyCache
	}
	sysProxyChecked = time.Now()
	sysProxyCache = readSystemProxy()
	return sysProxyCache
}

func readSystemProxy() *url.URL {
	key, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer key.Close()
	enable, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil || enable == 0 {
		return nil
	}
	raw, _, err := key.GetStringValue("ProxyServer")
	if err != nil || strings.TrimSpace(raw) == "" {
		return nil
	}
	return parseProxyServer(raw)
}

// parseProxyServer 解析 ProxyServer 值的两种格式:
// "host:port"(全协议) 或 "http=host:port;https=host:port;..."(分协议)。
func parseProxyServer(raw string) *url.URL {
	raw = strings.TrimSpace(raw)
	if strings.Contains(raw, "=") {
		var httpsPart string
		for _, kv := range strings.Split(raw, ";") {
			k, v, ok := strings.Cut(strings.TrimSpace(kv), "=")
			if !ok {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(k), "https") {
				httpsPart = strings.TrimSpace(v)
			}
		}
		if httpsPart == "" {
			return nil
		}
		raw = httpsPart
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil
	}
	return u
}
