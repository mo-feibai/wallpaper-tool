//go:build !windows

package netproxy

import "net/url"

// SystemProxy 非 Windows 平台不支持读系统代理, 走环境变量或直连。
func SystemProxy() *url.URL { return nil }
