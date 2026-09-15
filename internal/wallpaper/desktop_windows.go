//go:build windows

// Package wallpaper 封装 Windows 桌面壁纸与锁屏壁纸的设置。
package wallpaper

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	user32                   = windows.NewLazySystemDLL("user32.dll")
	procSystemParametersInfoW = user32.NewProc("SystemParametersInfoW")
)

const (
	spiSetDeskWallpaper = 0x0014
	spifUpdateIniFile   = 0x01
	spifSendChange      = 0x02
)

// desktopStyle 桌面适配方式对应 HKCU\Control Panel\Desktop 的注册表值。
// WallpaperStyle / TileWallpaper:
//
//	fill=10/0, fit=6/0, stretch=2/0, center=0/0, tile=0/1, span=22/0
var desktopStyle = map[string][2]string{
	"fill":    {"10", "0"},
	"fit":     {"6", "0"},
	"stretch": {"2", "0"},
	"center":  {"0", "0"},
	"tile":    {"0", "1"},
	"span":    {"22", "0"},
}

func setDesktopStyle(fit string) error {
	vals, ok := desktopStyle[fit]
	if !ok {
		vals = desktopStyle["fill"]
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, `Control Panel\Desktop`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	if err := key.SetStringValue("WallpaperStyle", vals[0]); err != nil {
		return err
	}
	return key.SetStringValue("TileWallpaper", vals[1])
}

// SetDesktop 设置桌面壁纸(无需管理员权限)。
func SetDesktop(path, fit string) error {
	if err := setDesktopStyle(fit); err != nil {
		return fmt.Errorf("设置桌面适配方式失败: %w", err)
	}
	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	r1, _, callErr := procSystemParametersInfoW.Call(
		spiSetDeskWallpaper,
		0,
		uintptr(unsafe.Pointer(ptr)),
		spifUpdateIniFile|spifSendChange,
	)
	if r1 == 0 {
		return fmt.Errorf("SystemParametersInfoW 失败: %v", callErr)
	}
	return nil
}
