//go:build windows

// Package singleinstance 通过命名互斥体保证托盘主程序单实例运行。
// 仅托盘模式使用; --apply-lockscreen 等助手模式由计划任务与主程序并发运行, 不得参与互斥。
package singleinstance

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32         = windows.NewLazySystemDLL("kernel32.dll")
	procCreateMutexW = kernel32.NewProc("CreateMutexW")
	user32           = windows.NewLazySystemDLL("user32.dll")
	procMessageBoxW  = user32.NewProc("MessageBoxW")
)

// errAlreadyExists ERROR_ALREADY_EXISTS(183)
const errAlreadyExists = syscall.Errno(183)

// mutexName 会话级命名互斥体: 同一登录会话内单实例, 不同 Windows 用户会话互不干扰。
const mutexName = `Local\WallpaperToolSingleInstance`

const mbIconInformation = 0x40

// Acquire 尝试获取单实例互斥体。
// 成功返回 release 函数(进程退出或崩溃时内核也会自动释放);
// 已有实例运行时返回错误, 此时不得继续启动服务。
func Acquire() (release func(), err error) {
	name, err := syscall.UTF16PtrFromString(mutexName)
	if err != nil {
		return nil, err
	}
	h, _, callErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return nil, fmt.Errorf("创建单实例互斥体失败: %v", callErr)
	}
	if errno, ok := callErr.(syscall.Errno); ok && errno == errAlreadyExists {
		_ = windows.CloseHandle(windows.Handle(h))
		return nil, fmt.Errorf("已有实例正在运行")
	}
	release = func() { _ = windows.CloseHandle(windows.Handle(h)) }
	return release, nil
}

// NotifyAlreadyRunning 弹出提示框告知用户程序已在运行。
func NotifyAlreadyRunning() {
	title, err1 := syscall.UTF16PtrFromString("壁纸工具")
	text, err2 := syscall.UTF16PtrFromString("壁纸工具已在运行, 请使用系统托盘右下角的托盘图标操作。")
	if err1 != nil || err2 != nil {
		return
	}
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), mbIconInformation)
}
