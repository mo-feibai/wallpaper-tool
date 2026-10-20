// Package netproxy 提供统一的代理解析。
// 优先级: 显式配置 > Windows 系统代理 > 环境变量 > 直连。
package netproxy

import (
	"net/http"
	"net/url"
	"sync/atomic"
)

// Resolver 按优先级解析每次请求实际使用的代理。
// 显式地址可在运行时热更新(配置页保存后立即生效)。
type Resolver struct {
	override atomic.Value // string, 显式代理地址
}

// New 创建解析器。
func New() *Resolver {
	return &Resolver{}
}

// SetOverride 设置显式代理地址, 空串表示跟随系统代理。
func (r *Resolver) SetOverride(addr string) {
	r.override.Store(addr)
}

// Override 返回当前显式代理地址。
func (r *Resolver) Override() string {
	if v := r.override.Load(); v != nil {
		s, _ := v.(string)
		return s
	}
	return ""
}

// Proxy 返回 http.Transport 使用的代理函数。
func (r *Resolver) Proxy(req *http.Request) (*url.URL, error) {
	if s := r.Override(); s != "" {
		return url.Parse(s)
	}
	if u := SystemProxy(); u != nil {
		return u, nil
	}
	return http.ProxyFromEnvironment(req)
}
