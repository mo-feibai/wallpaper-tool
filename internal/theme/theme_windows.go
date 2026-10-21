//go:build windows

package theme

import (
	"fmt"
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
	smtoAbortIfHun  = 0x0002
	// historyKey 下的 AutoColor 是 Windows 原生"从背景自动选取主题色"的开关。
	historyKey     = `Software\Microsoft\Windows\CurrentVersion\Themes\History`
	personalizeKey = `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`
)

// Apply 按壁纸联动 Windows 主题。
//
// accent 为 true 时开启 Windows 原生的"从背景自动选取主题色"
// (Themes\History\AutoColor=1), 由系统自己从壁纸提取强调色,
// 比自己算主色更准, 且换图时系统会实时跟随。
// lightDark 为 true 时按壁纸亮度设置系统深/浅色模式。
// 返回实际使用的主色(仅用于日志), 便于调用方排查。
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
		if err := setAutoColor(true); err != nil {
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

// setAutoColor 开关 Windows 原生的"从背景自动选取主题色"。
//
// 实测结论(Win10 19045): 只写 Personalize\AccentColor 不会让外壳生效,
// 外壳认的是 Themes\History\AutoColor, 由系统自己从壁纸提取强调色。
func setAutoColor(on bool) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, historyKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开主题历史键失败: %w", err)
	}
	defer key.Close()
	var v uint32
	if on {
		v = 1
	}
	if err := key.SetDWordValue("AutoColor", v); err != nil {
		return err
	}
	// 通知外壳重新取色
	broadcast("ImmersiveColorSet")
	broadcast("WindowsThemeElement")
	return nil
}

// writeLightDark 写入深/浅色模式设置并通知外壳刷新。
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
	broadcast("ImmersiveColorSet")
	return nil
}

// Restore 关闭"从背景自动取色", 让系统回到用户自己设定的强调色。
func Restore() error {
	if err := setAutoColor(false); err != nil {
		return err
	}
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
		smtoAbortIfHun,
		1000,
		0,
	)
}
