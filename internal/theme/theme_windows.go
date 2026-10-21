//go:build windows

package theme

import (
	"fmt"
	"image/color"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	user32                 = windows.NewLazySystemDLL("user32.dll")
	procSendMessageTimeout = user32.NewProc("SendMessageTimeoutW")
)

const (
	hwmBroadcast    = 0xffff
	wmSettingChange = 0x001A
	smtoAbortIfHung = 0x0002
	personalizeKey  = `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`
	themesKey       = `Software\Microsoft\Windows\CurrentVersion\Themes`
)

// Apply 按壁纸主色设置 Windows 强调色, 并可选地按明暗切换系统深/浅色模式。
//
// accent 为 true 时写入 AccentColor;
// lightDark 为 true 时按壁纸亮度设置 AppsUseLightTheme / SystemUsesLightTheme。
// 返回实际使用的主色, 便于调用方记录日志。
func Apply(path string, accent, lightDark bool) (string, error) {
	if !accent && !lightDark {
		return "", nil
	}
	rgb, err := Dominant(path)
	if err != nil {
		return "", err
	}
	hex := fmt.Sprintf("#%02X%02X%02X", rgb.R, rgb.G, rgb.B)

	if accent {
		if err := writeAccent(rgb); err != nil {
			return hex, err
		}
	}
	if lightDark {
		// 壁纸偏暗 -> 系统用深色模式(0), 偏亮 -> 浅色模式(1)
		dark := uint32(0)
		if Luminance(rgb) >= 0.5 {
			dark = 1
		}
		if err := writeLightDark(dark); err != nil {
			return hex, err
		}
	}
	return hex, nil
}

// writeAccent 写入强调色并通知系统刷新。
func writeAccent(rgb color.RGBA) error {
	abgr := ABGR(rgb)
	key, err := registry.OpenKey(registry.CURRENT_USER, personalizeKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开主题注册表失败: %w", err)
	}
	defer key.Close()
	if err := key.SetDWordValue("AccentColor", abgr); err != nil {
		return err
	}
	// AccentColorId 置 0, 表示使用上面写入的自定义强调色
	if err := key.SetDWordValue("AccentColorId", 0); err != nil {
		return err
	}
	// ColorizationColor 位于 Themes 键下, 一起写更稳妥
	if tkey, err := registry.OpenKey(registry.CURRENT_USER, themesKey, registry.SET_VALUE); err == nil {
		_ = tkey.SetDWordValue("ColorizationColor", abgr)
		_ = tkey.Close()
	}
	broadcast("ImmersiveColorSet")
	return nil
}

// writeLightDark 写入深/浅色模式设置。
func writeLightDark(light uint32) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, personalizeKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开主题注册表失败: %w", err)
	}
	defer key.Close()
	if err := key.SetDWordValue("AppsUseLightTheme", light); err != nil {
		return err
	}
	if err := key.SetDWordValue("SystemUsesLightTheme", light); err != nil {
		return err
	}
	broadcast("WindowsThemeElement")
	return nil
}

// broadcast 广播 WM_SETTINGCHANGE, 让外壳立即感知颜色/主题变化。
func broadcast(setting string) {
	ptr, err := syscall.UTF16PtrFromString(setting)
	if err != nil {
		return
	}
	procSendMessageTimeout.Call(
		hwmBroadcast,
		wmSettingChange,
		0,
		uintptr(unsafe.Pointer(ptr)),
		smtoAbortIfHung,
		1000,
		0,
	)
}
