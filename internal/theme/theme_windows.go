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

// Result 是一次主题联动的结果。
type Result struct {
	// Color 壁纸主色(十六进制), 仅用于日志。
	Color string
	// Brightness 整图亮度中位数(0~1)。
	Brightness float64
	// Mode 生效的深浅色模式(1=浅色, 0=深色)。
	Mode int
	// Changed 本次是否真的切换了深浅色模式。
	Changed bool
}

// ModeName 返回深浅色模式的中文名。
func (r Result) ModeName() string {
	if r.Mode == 1 {
		return "浅色"
	}
	return "深色"
}

// Apply 按壁纸联动 Windows 主题。
//
// accent 为 true 时开启 Windows 原生的"从背景自动选取主题色"
// (Themes\History\AutoColor=1), 由系统自己从壁纸提取强调色,
// 比自己算主色更准, 且换图时系统会实时跟随。
// lightDark 为 true 时按整图亮度切换应用的深/浅色模式。
func Apply(path string, accent, lightDark bool) (Result, error) {
	res := Result{}
	if !accent && !lightDark {
		return res, nil
	}
	a, err := Analyze(path)
	if err != nil {
		return res, err
	}
	res.Color = fmt.Sprintf("#%02X%02X%02X", a.Dominant.R, a.Dominant.G, a.Dominant.B)
	res.Brightness = a.Brightness
	res.Mode = 1

	if accent {
		if err := setAutoColor(true); err != nil {
			return res, err
		}
	}
	if lightDark {
		mode, changed, err := applyLightDark(a.Brightness)
		if err != nil {
			return res, err
		}
		res.Mode = mode
		res.Changed = changed
	}
	return res, nil
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

// applyLightDark 按整图亮度切换应用主题(AppsUseLightTheme)。
//
// 只改应用主题, 不动系统界面主题(SystemUsesLightTheme):
// 后者保持用户自己的选择, 避免整台机器的界面被壁纸带着反复变色。
func applyLightDark(brightness float64) (mode int, changed bool, err error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, personalizeKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return 0, false, fmt.Errorf("打开主题注册表失败: %w", err)
	}
	defer key.Close()

	current := 0
	if v, _, err := key.GetIntegerValue("AppsUseLightTheme"); err == nil {
		current = int(v)
	}
	target, changed := LightDarkDecision(brightness, current)
	if !changed {
		return current, false, nil
	}
	if err := key.SetDWordValue("AppsUseLightTheme", uint32(target)); err != nil {
		return current, false, err
	}
	broadcast("WindowsThemeElement")
	broadcast("ImmersiveColorSet")
	return target, true, nil
}

// RestoreAccent 关闭"从背景自动取色", 让系统回到用户自己设定的强调色。
func RestoreAccent() error {
	return setAutoColor(false)
}

// RestoreLightDark 关闭深浅色联动, 把应用主题恢复成与系统界面一致。
//
// 系统界面主题(SystemUsesLightTheme)从未被本程序改过,
// 它保存的正是用户自己的偏好, 用它来还原最自然。
func RestoreLightDark() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, personalizeKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("打开主题注册表失败: %w", err)
	}
	defer key.Close()

	system, _, err := key.GetIntegerValue("SystemUsesLightTheme")
	if err != nil {
		return nil // 系统主题没设置过, 保持原样
	}
	if err := key.SetDWordValue("AppsUseLightTheme", uint32(system)); err != nil {
		return err
	}
	broadcast("WindowsThemeElement")
	broadcast("ImmersiveColorSet")
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
