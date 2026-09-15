//go:build windows

package elevate

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

// TaskName 锁屏计划任务名。
const TaskName = "WallpaperToolLockScreen"

// createNoWindow CREATE_NO_WINDOW: schtasks 是控制台程序,
// 从 windowsgui 主程序调用时必须隐藏其控制台窗口, 否则每次触发都闪黑框。
const createNoWindow = 0x08000000

func runSchtasks(args ...string) (string, error) {
	cmd := exec.Command("schtasks", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func compact(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// InstallTask 创建以最高权限运行的一次性计划任务(仅在已提权进程内调用)。
// 触发方式为手动 schtasks /Run, /SC ONCE 的时间点仅为占位(触发时若无动作则无副作用)。
func InstallTask(exePath string) error {
	tr := fmt.Sprintf(`"%s" --apply-lockscreen`, exePath)
	out, err := runSchtasks("/Create", "/TN", TaskName, "/TR", tr,
		"/SC", "ONCE", "/ST", "00:00", "/RL", "HIGHEST", "/F")
	if err != nil {
		return fmt.Errorf("创建计划任务失败: %s", compact(out))
	}
	return nil
}

// RunTask 静默触发计划任务(普通权限即可, 不弹 UAC)。
func RunTask() error {
	out, err := runSchtasks("/Run", "/TN", TaskName)
	if err != nil {
		return fmt.Errorf("触发计划任务失败: %s", compact(out))
	}
	return nil
}

// TaskExists 查询计划任务是否存在。查询出错一律视为不存在。
func TaskExists() (bool, error) {
	_, err := runSchtasks("/Query", "/TN", TaskName)
	if err != nil {
		return false, nil
	}
	return true, nil
}

// RemoveTask 删除计划任务(仅在已提权进程内调用)。
func RemoveTask() error {
	out, err := runSchtasks("/Delete", "/TN", TaskName, "/F")
	if err != nil {
		return fmt.Errorf("删除计划任务失败: %s", compact(out))
	}
	return nil
}
