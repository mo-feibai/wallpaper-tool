//go:build windows

package wallpaper

import (
	"errors"
	"fmt"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

// PersonalizationCSP 注册表键路径(需管理员权限写入)。
// 该键生效后锁屏壁纸由策略接管, Windows 设置里不可再手动修改。
const lockScreenKeyPath = `SOFTWARE\Microsoft\Windows\CurrentVersion\PersonalizationCSP`

func isNotExist(err error) bool {
	return errors.Is(err, syscall.ERROR_FILE_NOT_FOUND)
}

// ApplyLockScreen 写入锁屏壁纸注册表值(仅可在高权限进程内调用)。
func ApplyLockScreen(path string) error {
	key, _, err := registry.CreateKey(registry.LOCAL_MACHINE, lockScreenKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开 PersonalizationCSP 键失败: %w", err)
	}
	defer key.Close()
	if err := key.SetDWordValue("LockScreenImageStatus", 1); err != nil {
		return err
	}
	if err := key.SetStringValue("LockScreenImagePath", path); err != nil {
		return err
	}
	return key.SetStringValue("LockScreenImageUrl", path)
}

// ClearLockScreen 删除锁屏策略注册表值, 恢复用户可在设置中自行修改(仅可在高权限进程内调用)。
func ClearLockScreen() error {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, lockScreenKeyPath, registry.SET_VALUE)
	if err != nil {
		if isNotExist(err) {
			return nil // 键不存在说明从未设置过
		}
		return err
	}
	defer key.Close()
	_ = key.DeleteValue("LockScreenImageStatus")
	_ = key.DeleteValue("LockScreenImagePath")
	_ = key.DeleteValue("LockScreenImageUrl")
	return nil
}
