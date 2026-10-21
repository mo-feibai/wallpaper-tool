//go:build windows

// Package fullscreen 检测前台窗口是否全屏。
// 用于换图时避让正在全屏的游戏/演示/视频, 避免在观影时壁纸突然切换。
package fullscreen

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                = windows.NewLazySystemDLL("user32.dll")
	procGetWindowRect     = user32.NewProc("GetWindowRect")
	procMonitorFromWindow = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW   = user32.NewProc("GetMonitorInfoW")
)

// monitorDefToNearest 与 MONITOR_DEFAULTTONEAREST 一致: 取最近的显示器。
const monitorDefToNearest = 0x00000002

// monitorInfo 对应 Win32 的 MONITORINFO 结构。
type monitorInfo struct {
	Size    uint32
	Monitor windows.Rect
	Work    windows.Rect
	Flags   uint32
}

// IsFullscreen 报告当前前台窗口是否铺满了它所在的显示器。
//
// 用显示器区域而不是整屏分辨率来判断, 这样多显示器下
// 在副屏全屏的游戏同样能被识别。
func IsFullscreen() bool {
	hwnd := windows.GetForegroundWindow()
	if hwnd == 0 {
		return false
	}
	var rect windows.Rect
	if r, _, _ := procGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rect))); r == 0 {
		return false
	}
	mon, _, _ := procMonitorFromWindow.Call(uintptr(hwnd), monitorDefToNearest)
	if mon == 0 {
		return false
	}
	mi := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
	if r, _, _ := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi))); r == 0 {
		return false
	}
	// 窗口需要覆盖所在显示器的整个区域(允许边界差 1 像素)
	return rect.Left <= mi.Monitor.Left+1 &&
		rect.Top <= mi.Monitor.Top+1 &&
		rect.Right >= mi.Monitor.Right-1 &&
		rect.Bottom >= mi.Monitor.Bottom-1
}
