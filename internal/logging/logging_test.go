package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRotatingWriterRotates 验证日志超过上限后自动轮转, 且不会无限增长。
func TestRotatingWriterRotates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.txt")
	w, err := newRotatingWriter(path)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	line := strings.Repeat("x", 1024) + "\n"
	// 写入约 1.2 MiB, 超过 MaxSize(1 MiB)
	for i := 0; i < int(MaxSize/len(line))+200; i++ {
		if _, err := w.Write([]byte(line)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	_ = w.Close()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if st.Size() > MaxSize {
		t.Errorf("主日志大小 = %d, 超过上限 %d", st.Size(), MaxSize)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Errorf("未生成历史日志 %s: %v", path+".1", err)
	}
}

// TestSetupReturnsLogger 验证 Setup 可用且能写文件。
func TestSetupReturnsLogger(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.txt")
	lg, w := SetupWithWriter(path)
	defer func() {
		if w != nil {
			_ = w.Close()
		}
	}()
	lg.Printf("hello")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "hello") {
		t.Errorf("日志内容 = %q, 未包含 hello", string(data))
	}
}
