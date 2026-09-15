// Package assets 提供内嵌的静态资源。
package assets

import (
	"bytes"
	_ "embed"
	"os"
)

//go:embed icon.ico
var iconData []byte

//go:embed icon.png
var pngData []byte

// Icon 返回内嵌的应用图标(ico)字节。
func Icon() []byte { return iconData }

// Png 返回内嵌的应用图标(png)字节。
// Windows 通知的 IconUri 对 PNG 支持最好。
func Png() []byte { return pngData }

// EnsureIconFile 把内嵌图标落盘到指定路径(已存在则跳过), 返回实际路径。
func EnsureIconFile(dest string) (string, error) {
	if _, err := os.Stat(dest); err == nil {
		return dest, nil
	}
	if err := os.WriteFile(dest, iconData, 0o644); err != nil {
		return "", err
	}
	return dest, nil
}

// EnsurePngFile 把内嵌 PNG 图标写入指定路径(图标更新时自动覆盖), 返回实际路径。
func EnsurePngFile(dest string) (string, error) {
	cur, err := os.ReadFile(dest)
	if err != nil || !bytes.Equal(cur, pngData) {
		if err := os.WriteFile(dest, pngData, 0o644); err != nil {
			return "", err
		}
	}
	return dest, nil
}
