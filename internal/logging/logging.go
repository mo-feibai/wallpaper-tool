// Package logging 提供文件日志。
// 程序以 windowsgui 子系统构建，没有控制台输出，所有日志写入 log.txt。
package logging

import (
	"log"
	"os"
)

// Setup 打开日志文件(追加模式)并返回 logger。
func Setup(path string) *log.Logger {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		// 日志文件打不开时退回 stderr，至少不崩溃
		return log.New(os.Stderr, "", log.LstdFlags)
	}
	return log.New(f, "", log.LstdFlags)
}
