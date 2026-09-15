//go:build windows

// Package elevate 封装提权启动(ShellExecute runas)与计划任务管理。
// 锁屏链路: 首次 runas 弹一次 UAC 注册计划任务, 之后 schtasks /Run 静默触发。
package elevate

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shell32           = windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteW = shell32.NewProc("ShellExecuteW")
)

const swHide = 0

func quoteArg(s string) string {
	if s == "" || strings.ContainsAny(s, " \t\"") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

// RunElevated 以管理员身份启动 exe(UAC 弹窗由用户确认)。
// 返回时目标进程已启动但尚未执行完毕, 属异步触发。
func RunElevated(exe string, args ...string) error {
	verb, err := syscall.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	exePtr, err := syscall.UTF16PtrFromString(exe)
	if err != nil {
		return err
	}
	argLine := strings.Join(mapSlice(args, quoteArg), " ")
	argsPtr, err := syscall.UTF16PtrFromString(argLine)
	if err != nil {
		return err
	}
	r1, _, callErr := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(exePtr)),
		uintptr(unsafe.Pointer(argsPtr)),
		0,
		swHide,
	)
	if r1 <= 32 {
		return fmt.Errorf("ShellExecuteW 提权启动失败: %v", callErr)
	}
	return nil
}

func mapSlice(in []string, fn func(string) string) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = fn(v)
	}
	return out
}
