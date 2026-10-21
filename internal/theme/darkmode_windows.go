//go:build windows

package theme

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
)

// personalizeKey 深浅色相关值所在的位置。
const personalizeKey = `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`

// DarkModeValues 深浅色设置的一组取值。
type DarkModeValues struct {
	// Apps 应用主题(1=浅色, 0=深色)。
	Apps int
	// System 系统界面主题(1=浅色, 0=深色)。
	System int
	// Valid 是否成功读到。
	Valid bool
}

// ReadDarkMode 读取当前的深浅色设置。
func ReadDarkMode() (DarkModeValues, error) {
	out := DarkModeValues{}
	key, err := registry.OpenKey(registry.CURRENT_USER, personalizeKey, registry.QUERY_VALUE)
	if err != nil {
		return out, fmt.Errorf("打开主题注册表失败: %w", err)
	}
	defer key.Close()
	if v, _, err := key.GetIntegerValue("AppsUseLightTheme"); err == nil {
		out.Apps = int(v)
		out.Valid = true
	}
	if v, _, err := key.GetIntegerValue("SystemUsesLightTheme"); err == nil {
		out.System = int(v)
	}
	return out, nil
}

// ApplyDarkMode 写入深浅色模式。
//
// light  true=浅色, false=深色
// systemUI 为 true 时同时改系统界面主题(任务栏/开始菜单)。
func ApplyDarkMode(light bool, systemUI bool) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, personalizeKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开主题注册表失败: %w", err)
	}
	defer key.Close()

	var v uint32
	if light {
		v = 1
	}
	if err := key.SetDWordValue("AppsUseLightTheme", v); err != nil {
		return err
	}
	if systemUI {
		if err := key.SetDWordValue("SystemUsesLightTheme", v); err != nil {
			return err
		}
	}
	broadcast("WindowsThemeElement")
	broadcast("ImmersiveColorSet")
	return nil
}

// RestoreDarkMode 恢复用户原本的深浅色设置。
func RestoreDarkMode(apps, system int) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, personalizeKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开主题注册表失败: %w", err)
	}
	defer key.Close()
	if err := key.SetDWordValue("AppsUseLightTheme", uint32(apps)); err != nil {
		return err
	}
	if err := key.SetDWordValue("SystemUsesLightTheme", uint32(system)); err != nil {
		return err
	}
	broadcast("WindowsThemeElement")
	broadcast("ImmersiveColorSet")
	return nil
}
