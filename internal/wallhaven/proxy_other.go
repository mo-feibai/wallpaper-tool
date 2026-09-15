//go:build !windows

package wallhaven

import "net/url"

// systemProxy 非 Windows 平台不支持读系统代理, 走环境变量或直连。
func systemProxy() *url.URL { return nil }
