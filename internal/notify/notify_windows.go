// Package notify 发送 Windows 系统通知(经 winrt-toast 直接调用 WinRT, 无子进程)。
// 通知身份通过 HKCU\Software\Classes\AppUserModelId\<AUMID> 注册显示名与图标。
package notify

import (
	"log"
	"sync"

	"github.com/mtunjic/winrt-toast"

	"golang.org/x/sys/windows/registry"
)

// appAUMID 本应用的通知身份标识。
// 注意: 平台会缓存身份信息, 更换身份值可强制刷新。
const appAUMID = `WallpaperTool.App`

var (
	iconMu   sync.RWMutex
	iconPath string
)

// EnsureIdentity 注册本应用的通知身份(显示名与图标), 幂等可重复调用。
// iconPath 为落盘的 png 文件绝对路径, 同时供后续通知引用。
func EnsureIdentity(displayName, pngPath string) error {
	iconMu.Lock()
	iconPath = pngPath
	iconMu.Unlock()

	key, _, err := registry.CreateKey(registry.CURRENT_USER,
		`Software\Classes\AppUserModelId\`+appAUMID, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	if err := key.SetStringValue("DisplayName", displayName); err != nil {
		return err
	}
	return key.SetStringValue("IconUri", pngPath)
}

// Show 异步发送一条 Windows 通知, 不阻塞调用方; 错误仅记录日志。
func Show(title, body string, lg *log.Logger) {
	go func() {
		iconMu.RLock()
		p := iconPath
		iconMu.RUnlock()
		err := toast.Push(toast.Notification{
			AppID:   appAUMID,
			Title:   title,
			Message: body,
			Icon:    p,
		})
		if err != nil {
			if lg != nil {
				lg.Printf("send notification failed: %v", err)
			}
			return
		}
		if lg != nil {
			lg.Printf("notification sent: %s | %s", title, body)
		}
	}()
}
