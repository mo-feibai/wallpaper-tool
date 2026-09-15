//go:build windows

// Package autostart 通过 HKCU Run 键管理开机自启(无需管理员权限)。
package autostart

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

const (
	runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	valueName  = "WallpaperTool"
)

func isNotExist(err error) bool {
	return errors.Is(err, syscall.ERROR_FILE_NOT_FOUND)
}

// Enable 写入自启项(路径带引号, 兼容含空格目录)。
func Enable(exePath string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return key.SetStringValue(valueName, `"`+exePath+`"`)
}

// Disable 删除自启项, 不存在时视为成功。
func Disable() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	err = key.DeleteValue(valueName)
	if isNotExist(err) {
		return nil
	}
	return err
}

// IsEnabled 查询自启项是否存在。
func IsEnabled() (bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false, err
	}
	defer key.Close()
	_, _, err = key.GetStringValue(valueName)
	if isNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
