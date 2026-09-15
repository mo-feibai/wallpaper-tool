// 缓存目录变更迁移逻辑:
// 目录变化时把旧目录内的图片移动到新目录, 并同步重映射 state 中记录的所有本地路径。
package scheduler

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"wallpaper-tool/internal/paths"
	"wallpaper-tool/internal/state"
)

// SyncCacheDir 检查配置的缓存目录与 state 记录的活动目录是否一致;
// 不一致时执行迁移(旧目录 -> 新目录), 迁移后按新限额清理缓存。
// 调用前 s.cfg 必须已是要生效的配置(启动时或 UpdateConfig 之后)。
func (s *Scheduler) SyncCacheDir() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	newDir := s.cacheDir()
	st := s.state.Get()
	oldDir := st.CacheDir
	if strings.TrimSpace(oldDir) == "" {
		oldDir = paths.CacheDir() // 从未记录过, 说明此前一直在用默认目录
	}
	if strings.EqualFold(filepath.Clean(oldDir), filepath.Clean(newDir)) {
		// 目录未变化: 补记活动目录; 若默认缓存目录仍有残留文件(历史迁移失败的源文件), 一并捞回
		_ = os.MkdirAll(newDir, 0o755)
		def := paths.CacheDir()
		if !strings.EqualFold(filepath.Clean(def), filepath.Clean(newDir)) {
			if entries, err := os.ReadDir(def); err == nil && len(entries) > 0 {
				moved, failed := migrateCacheDir(def, newDir)
				if moved > 0 || failed > 0 {
					s.log.Printf("cache cleanup retry: moved=%d failed=%d from=%s to=%s", moved, failed, def, newDir)
				}
				_ = os.Remove(def) // 已空则删除
			}
		}
		if st.CacheDir != filepath.Clean(newDir) {
			_ = s.state.Update(func(u *state.State) { u.CacheDir = filepath.Clean(newDir) })
		}
		return nil
	}

	if err := os.MkdirAll(newDir, 0o755); err != nil {
		return fmt.Errorf("创建新缓存目录失败: %w", err)
	}

	moved, failed := migrateCacheDir(oldDir, newDir)
	s.log.Printf("cache migration: moved=%d failed=%d from=%s to=%s", moved, failed, oldDir, newDir)

	// 重映射 state 中所有本地路径(仅对已存在于新目录的文件)
	remap := func(p string) string {
		return remapCachePath(p, oldDir, newDir)
	}
	_ = s.state.Update(func(u *state.State) {
		u.CacheDir = filepath.Clean(newDir)
		u.Current.Path = remap(u.Current.Path)
		u.LockCurrent.Path = remap(u.LockCurrent.Path)
		u.LockScreenTarget = remap(u.LockScreenTarget)
		for i := range u.History {
			u.History[i].Path = remap(u.History[i].Path)
		}
		for i := range u.Queue {
			u.Queue[i].Path = remap(u.Queue[i].Path)
		}
	})

	// 锁屏注册表仍指向旧路径, 若文件已迁移则重写目标文件并重新应用
	if s.cfg.Targets.LockScreen && s.state.Get().TaskInstalled {
		lock := s.state.Get().LockCurrent
		if lock.Path != "" {
			if _, err := os.Stat(lock.Path); err == nil {
				if err := s.applyLockScreenItem(lock); err != nil {
					s.log.Printf("迁移后重新应用锁屏失败: %v", err)
				}
			}
		}
	}

	// 旧目录已空则删除
	_ = os.Remove(oldDir)

	// 按新限额立即清理
	s.cleanupCache()
	return nil
}

// cacheDir 返回当前生效的缓存目录。调用方需持有 s.mu。
func (s *Scheduler) cacheDir() string {
	return paths.ResolveCacheDir(s.cfg.Cache.Dir)
}

// migrateCacheDir 把旧目录内的文件移动到新目录, 返回成功与失败数量。
// 目标已存在同名文件时跳过移动(视为成功)。
func migrateCacheDir(oldDir, newDir string) (moved, failed int) {
	entries, err := os.ReadDir(oldDir)
	if err != nil {
		return 0, 0 // 旧目录不存在, 无需迁移
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		src := filepath.Join(oldDir, e.Name())
		dst := filepath.Join(newDir, e.Name())
		if _, err := os.Stat(dst); err == nil {
			_ = os.Remove(src) // 新目录已有同名文件, 丢弃旧副本
			moved++
			continue
		}
		if err := moveFile(src, dst); err != nil {
			failed++
			continue
		}
		moved++
	}
	return moved, failed
}

// moveFile 移动文件, 跨盘时回退为复制+删除。
// 复制完成后即使删除源文件失败(如被占用)也视为成功, 源文件由后续重试清理。
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	_ = os.Remove(src)
	return nil
}

// remapCachePath 把位于 oldDir 下的路径重映射到 newDir;
// 文件不在新目录时保留原路径(迁移失败的文件仍可从旧位置访问)。
func remapCachePath(p, oldDir, newDir string) string {
	if p == "" {
		return p
	}
	lowP := strings.ToLower(filepath.Clean(p))
	lowOld := strings.ToLower(filepath.Clean(oldDir))
	if lowP != lowOld && !strings.HasPrefix(lowP, lowOld+string(os.PathSeparator)) && !strings.HasPrefix(lowP, lowOld+"/") {
		return p
	}
	rel, err := filepath.Rel(oldDir, p)
	if err != nil {
		return p
	}
	candidate := filepath.Join(newDir, rel)
	if _, err := os.Stat(candidate); err != nil {
		return p // 新位置不存在, 保留旧路径
	}
	return candidate
}
