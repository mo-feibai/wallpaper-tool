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
	historyKey = `Software\Microsoft\Windows\CurrentVersion\Themes\History`
)

// Apply 按壁纸联动 Windows 主题。
//
// 开启系统原生的"从背景自动选取主题色"(Themes\History\AutoColor=1),
// 由 Windows 自己从壁纸提取强调色, 比自己算主色更准,
// 而且换图时系统会实时重新取色。
//
// 深浅色模式联动已移除: 壁纸的明暗调整交给"图片后处理"直接控制,
// 不再让系统主题跟着壁纸变, 避免整台机器的界面被壁纸带着翻转。
func Apply(accent bool) error {
	if !accent {
		return nil
	}
	return setAutoColor(true)
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

// RestoreAccent 关闭"从背景自动取色", 让系统回到用户自己设定的强调色。
func RestoreAccent() error {
	return setAutoColor(false)
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
