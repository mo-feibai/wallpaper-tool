// Package logging 提供文件日志。
// 程序以 windowsgui 子系统构建，没有控制台输出，所有日志写入 log.txt。
package logging

import (
	"fmt"
	"log"
	"os"
	"runtime/debug"
	"sync"
)

const (
	// MaxSize 单个日志文件的大小上限(字节), 超过后轮转。
	MaxSize = 1 << 20 // 1 MiB
	// Backups 保留的历史日志份数(日志总占用上限约 (Backups+1)*MaxSize)。
	Backups = 2
)

// rotatingWriter 追加写日志文件, 超过 MaxSize 后自动轮转。
// 之前的实现只用 O_APPEND 追加, 日志会无限增长(实测已累积到 800KB+)。
type rotatingWriter struct {
	mu   sync.Mutex
	path string
	f    *os.File
	size int64
}

// newRotatingWriter 打开日志文件并准备轮转。
func newRotatingWriter(path string) (*rotatingWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	w := &rotatingWriter{path: path, f: f}
	if st, err := f.Stat(); err == nil {
		w.size = st.Size()
	}
	return w, nil
}

// Write 追加写入, 必要时先轮转。
func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return 0, os.ErrClosed
	}
	// 已有内容且写入后会超限 -> 先轮转
	if w.size > 0 && w.size+int64(len(p)) > MaxSize {
		w.rotateLocked()
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

// rotateLocked 把当前日志改名归档, 再重新开一个空文件。
// 调用方必须持有 w.mu。
func (w *rotatingWriter) rotateLocked() {
	_ = w.f.Close()
	w.f = nil

	// 已有历史依次后移: .1 -> .2 -> .3
	for i := Backups; i >= 1; i-- {
		from := fmt.Sprintf("%s.%d", w.path, i)
		to := fmt.Sprintf("%s.%d", w.path, i+1)
		if _, err := os.Stat(from); err == nil {
			_ = os.Rename(from, to)
		}
	}
	// 当前日志归档为 .1(上一步已把 .1 挪走, 不会覆盖)
	_ = os.Rename(w.path, w.path+".1")

	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		// 兜底: 退回追加模式, 至少不崩
		f, err = os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return
		}
	}
	w.f = f
	w.size = 0
}

// Close 关闭日志文件。
func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}

// Setup 打开日志文件(自动轮转)并返回 logger。
func Setup(path string) *log.Logger {
	lg, _ := SetupWithWriter(path)
	return lg
}

// SetupWithWriter 打开日志文件并同时返回底层写入器(供测试关闭句柄)。
func SetupWithWriter(path string) (*log.Logger, *rotatingWriter) {
	w, err := newRotatingWriter(path)
	if err != nil {
		// 日志文件打不开时退回 stderr，至少不崩溃
		return log.New(os.Stderr, "", log.LstdFlags), nil
	}
	return log.New(w, "", log.LstdFlags), w
}

// Recover 返回一个用于 defer 的 panic 恢复函数。
// 程序以 windowsgui 子系统构建, 没有控制台输出,
// 未捕获的 panic 会让进程静默退出且不留痕迹;
// 这里把 panic 与调用栈写进日志文件, 便于事后定位。
func Recover(lg *log.Logger) func() {
	return func() {
		if r := recover(); r != nil {
			lg.Printf("PANIC: %v\n%s", r, debug.Stack())
		}
	}
}
