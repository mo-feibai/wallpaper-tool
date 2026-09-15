// Package scheduler 实现定时换图的编排核心:
// 预取队列、下载、桌面/锁屏应用、历史与缓存清理。
package scheduler

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"wallpaper-tool/internal/config"
	"wallpaper-tool/internal/elevate"
	"wallpaper-tool/internal/notify"
	"wallpaper-tool/internal/paths"
	"wallpaper-tool/internal/state"
	"wallpaper-tool/internal/wallhaven"
	"wallpaper-tool/internal/wallpaper"
)

const historyLimit = 50

var errNoResults = errors.New("搜索结果为空或已全部看过, 请调整筛选条件")

// Status 暴露给 Web/托盘的状态快照。
type Status struct {
	Paused        bool         `json:"paused"`
	NextRun       time.Time    `json:"next_run"`
	LastError     string       `json:"last_error"`
	TaskInstalled bool         `json:"task_installed"`
	Current       state.Item   `json:"current"`
	LockCurrent   state.Item   `json:"lock_current"`
	History       []state.Item `json:"history"`
	LockHistory   []state.Item `json:"lock_history"`
}

// Scheduler 调度器。
type Scheduler struct {
	mu      sync.Mutex
	cfg     config.Config
	state   *state.Store
	client  *wallhaven.Client
	log     *log.Logger
	ticker  *time.Ticker
	stop    chan struct{}
	nextRun time.Time
	lastErr string
	seen    map[string]bool
	started bool
}

// New 创建调度器。
func New(cfg config.Config, st *state.Store, client *wallhaven.Client, lg *log.Logger) *Scheduler {
	cfg.Sanitize()
	return &Scheduler{
		cfg:    cfg,
		state:  st,
		client: client,
		log:    lg,
		seen:   map[string]bool{},
	}
}

// Config 返回当前配置副本。
func (s *Scheduler) Config() config.Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

// UpdateConfig 保存并应用新配置(重置定时器)。
// 搜索相关字段变化时清空预取队列, 避免继续使用旧筛选条件拉取的图片。
func (s *Scheduler) UpdateConfig(cfg config.Config) error {
	cfg.Sanitize()
	oldSig := searchSignature(s.cfg)
	newSig := searchSignature(cfg)
	if err := config.Save(paths.ConfigFile(), cfg); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}
	s.mu.Lock()
	s.cfg = cfg
	s.client.SetProxy(cfg.Proxy)
	interval := time.Duration(cfg.IntervalMinutes) * time.Minute
	if s.ticker != nil {
		s.ticker.Reset(interval)
	}
	s.nextRun = time.Now().Add(interval)
	s.mu.Unlock()
	if oldSig != newSig {
		_ = s.state.Update(func(u *state.State) {
			u.Queue = nil
			u.Page = 1
			u.Seed = ""
		})
		s.log.Printf("search filters changed, prefetch queue cleared")
	}
	return nil
}

// searchSignature 生成影响搜索结果的配置签名, 用于检测筛选条件变化。
func searchSignature(c config.Config) string {
	return fmt.Sprintf("%v|%v|%v|%v|%v|%s|%s|%s|%s|%s|%s|%s",
		c.Categories.General, c.Categories.Anime, c.Categories.People,
		c.Purity.SFW, c.Purity.Sketchy,
		c.Query, c.Sorting, c.Order, c.TopRange, c.AtLeast, c.Ratios, c.Orientation)
}

// Start 启动定时循环。
func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.stop = make(chan struct{})
	s.ticker = time.NewTicker(time.Duration(s.cfg.IntervalMinutes) * time.Minute)
	s.nextRun = time.Now().Add(time.Duration(s.cfg.IntervalMinutes) * time.Minute)
	stop := s.stop
	ticker := s.ticker
	s.mu.Unlock()

	go func() {
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				st := s.state.Get()
				if st.Paused {
					continue
				}
				if err := s.applyNext(); err != nil {
					s.setErr(err.Error())
					s.log.Printf("自动换图失败: %v", err)
				}
			}
		}
	}()
}

// Stop 停止定时循环。
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return
	}
	s.started = false
	if s.stop != nil {
		close(s.stop)
	}
	if s.ticker != nil {
		s.ticker.Stop()
	}
}

// Next 手动换一张新图(忽略暂停状态)。
func (s *Scheduler) Next() error {
	if err := s.applyNext(); err != nil {
		s.setErr(err.Error())
		return err
	}
	s.setErr("")
	return nil
}

// Prev 回退到上一张(历史)。桌面与锁屏各自独立回退;
// 锁屏历史缺失或对应文件已被清理时, 仅回退桌面。
func (s *Scheduler) Prev() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state.Get()
	if len(st.History) == 0 || st.HistoryIndex+1 >= len(st.History) {
		err := errors.New("没有更早的历史记录")
		s.lastErr = err.Error()
		return err
	}
	idx := st.HistoryIndex + 1
	item := st.History[idx]
	if item.Path == "" {
		err := errors.New("历史记录缺少本地文件")
		s.lastErr = err.Error()
		return err
	}
	if _, err := os.Stat(item.Path); err != nil {
		err = fmt.Errorf("历史图片已被缓存清理, 无法回退")
		s.lastErr = err.Error()
		return err
	}
	if err := s.applyDesktop(item); err != nil {
		s.lastErr = err.Error()
		return err
	}
	_ = s.state.Update(func(u *state.State) {
		u.Current = item
		u.HistoryIndex = idx
	})

	// 锁屏独立回退
	lockIdx := st.LockHistoryIndex + 1
	if lockIdx < len(st.LockHistory) {
		lockItem := st.LockHistory[lockIdx]
		if lockItem.Path != "" {
			if _, err := os.Stat(lockItem.Path); err == nil {
				if err := s.applyLockScreenItem(lockItem); err != nil {
					s.log.Printf("lock screen prev failed: %v", err)
				} else {
					_ = s.state.Update(func(u *state.State) {
						u.LockCurrent = lockItem
						u.LockScreenTarget = lockItem.Path
						u.LockHistoryIndex = lockIdx
					})
				}
			} else {
				s.log.Printf("lock screen history file cleaned, skip revert: %s", lockItem.ID)
			}
		}
	}

	s.lastErr = ""
	notify.Show("更换成功", "已回退到上一张壁纸", s.log)
	return nil
}

// Pause 暂停自动更换。
func (s *Scheduler) Pause() error {
	return s.state.Update(func(u *state.State) { u.Paused = true })
}

// Resume 恢复自动更换(重新计时)。
func (s *Scheduler) Resume() error {
	if err := s.state.Update(func(u *state.State) { u.Paused = false }); err != nil {
		return err
	}
	s.mu.Lock()
	interval := time.Duration(s.cfg.IntervalMinutes) * time.Minute
	if s.ticker != nil {
		s.ticker.Reset(interval)
	}
	s.nextRun = time.Now().Add(interval)
	s.mu.Unlock()
	return nil
}

// TogglePause 切换暂停/继续。
func (s *Scheduler) TogglePause() error {
	if s.state.Get().Paused {
		return s.Resume()
	}
	return s.Pause()
}

// Status 返回状态快照。
func (s *Scheduler) Status() Status {
	s.mu.Lock()
	lastErr := s.lastErr
	nextRun := s.nextRun
	s.mu.Unlock()
	st := s.state.Get()
	return Status{
		Paused:        st.Paused,
		NextRun:       nextRun,
		LastError:     lastErr,
		TaskInstalled: st.TaskInstalled,
		Current:       st.Current,
		LockCurrent:   st.LockCurrent,
		History:       st.History,
		LockHistory:   st.LockHistory,
	}
}

func (s *Scheduler) setErr(msg string) {
	s.mu.Lock()
	s.lastErr = msg
	s.mu.Unlock()
}

// applyNext 完整执行一次换图: 取图 -> 下载 -> 应用 -> 记录历史 -> 清理缓存。
// 桌面与锁屏同时启用时各取一张不同的图; 仅锁屏时第一张直接给锁屏。
// 通过 s.mu 串行化, 定时/手动调用不会并发执行。
func (s *Scheduler) applyNext() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	st := s.state.Get()
	queue, page, seed := st.Queue, st.Page, st.Seed

	// 第一张: 桌面用; 桌面未启用时直接给锁屏
	item1, q1, p1, sd1, err := s.nextItemFrom(queue, page, seed, st.Current.ID)
	queue, page, seed = q1, p1, sd1
	if err != nil {
		s.persistFetch(queue, page, seed)
		return err
	}
	local1, derr := s.client.Download(
		wallhaven.Item{ID: item1.ID, Path: item1.FileURL, FileType: fileExtGuess(item1.FileURL)},
		s.cacheDir(),
	)
	if derr != nil {
		// 下载失败也推进队列并标记已见, 避免坏地址反复阻塞后续换图
		s.seen[item1.ID] = true
		s.persistFetch(queue, page, seed)
		return fmt.Errorf("下载壁纸失败: %w", derr)
	}
	item1.Path = local1

	lockItem := item1
	if s.cfg.Targets.Desktop {
		if err := s.applyDesktop(item1); err != nil {
			s.persistFetch(queue, page, seed)
			return err
		}
		s.seen[item1.ID] = true
		_ = s.state.Update(func(u *state.State) {
			u.Current = item1
			u.History = append([]state.Item{item1}, u.History...)
			if len(u.History) > historyLimit {
				u.History = u.History[:historyLimit]
			}
			u.HistoryIndex = 0
		})
		s.log.Printf("desktop wallpaper changed: id=%s file=%s", item1.ID, item1.Path)

		// 第二张不同的图给锁屏
		if s.cfg.Targets.LockScreen {
			item2, q2, p2, sd2, err2 := s.nextItemFrom(queue, page, seed, item1.ID)
			queue, page, seed = q2, p2, sd2
			if err2 == nil {
				local2, derr2 := s.client.Download(
					wallhaven.Item{ID: item2.ID, Path: item2.FileURL, FileType: fileExtGuess(item2.FileURL)},
					s.cacheDir(),
				)
				if derr2 == nil {
					item2.Path = local2
					lockItem = item2
				} else {
					s.seen[item2.ID] = true
					s.log.Printf("锁屏壁纸下载失败, 本次锁屏不更新: %v", derr2)
				}
			} else {
				s.log.Printf("获取锁屏壁纸失败, 本次锁屏沿用桌面图: %v", err2)
			}
		}
	}

	if s.cfg.Targets.LockScreen {
		s.seen[lockItem.ID] = true
		if err := s.applyLockScreenItem(lockItem); err != nil {
			s.persistFetch(queue, page, seed)
			return err
		}
		_ = s.state.Update(func(u *state.State) {
			u.LockCurrent = lockItem
			u.LockScreenTarget = lockItem.Path
			u.LockHistory = append([]state.Item{lockItem}, u.LockHistory...)
			if len(u.LockHistory) > historyLimit {
				u.LockHistory = u.LockHistory[:historyLimit]
			}
			u.LockHistoryIndex = 0
		})
		s.log.Printf("lock screen changed: id=%s file=%s", lockItem.ID, lockItem.Path)
	}

	s.persistFetch(queue, page, seed)
	s.nextRun = time.Now().Add(time.Duration(s.cfg.IntervalMinutes) * time.Minute)
	s.cleanupCache()
	s.notifyRotation(item1, lockItem)
	return nil
}

// notifyRotation 换图成功后发送 Windows 通知。
func (s *Scheduler) notifyRotation(desktop, lock state.Item) {
	var body string
	switch {
	case s.cfg.Targets.Desktop && s.cfg.Targets.LockScreen:
		body = "壁纸和锁屏更换成功"
	case s.cfg.Targets.Desktop:
		body = "桌面壁纸更换成功"
	case s.cfg.Targets.LockScreen:
		body = "锁屏壁纸更换成功"
	default:
		return
	}
	notify.Show("更换成功", body, s.log)
}

// persistFetch 持久化预取队列推进结果。
func (s *Scheduler) persistFetch(queue []state.Item, page int, seed string) {
	_ = s.state.Update(func(u *state.State) {
		u.Queue = queue
		u.Page = page
		u.Seed = seed
	})
}

// fileExtGuess 从直链 URL 猜测扩展名(wallhaven 直链以 .jpg/.png 结尾)。
func fileExtGuess(fileURL string) string {
	lower := strings.ToLower(fileURL)
	if strings.HasSuffix(lower, ".png") {
		return "image/png"
	}
	return "image/jpeg"
}

// nextItemFrom 从队列取一条(跳过 seen 与 excludeID); 队列空则拉取新的一页补充。
// 无论成败都返回推进后的队列/页码/seed, 由调用方决定是否持久化。
// 调用方需持有 s.mu。
func (s *Scheduler) nextItemFrom(queue []state.Item, page int, seed string, excludeID string) (item state.Item, newQueue []state.Item, newPage int, newSeed string, err error) {
	newPage, newSeed = page, seed
	for len(queue) > 0 {
		cand := queue[0]
		queue = queue[1:]
		if s.seen[cand.ID] || cand.ID == excludeID {
			continue
		}
		return cand, queue, newPage, newSeed, nil
	}

	res, err := s.client.Search(s.cfg, page, seed)
	if err != nil {
		return item, queue, newPage, newSeed, err
	}
	newPage = page + 1
	if res.Meta.LastPage > 0 && newPage > res.Meta.LastPage {
		newPage = 1
	}
	newSeed = seed
	if s.cfg.Sorting == "random" {
		newSeed = res.Meta.Seed
		if newPage == 1 {
			newSeed = "" // 回到第 1 页时丢弃旧 seed, 让服务端生成新的随机序列
		}
	}
	for _, it := range res.Data {
		if s.seen[it.ID] || it.ID == excludeID {
			continue
		}
		queue = append(queue, state.Item{ID: it.ID, FileURL: it.Path, PageURL: it.URL})
	}
	for len(queue) > 0 {
		cand := queue[0]
		queue = queue[1:]
		if s.seen[cand.ID] || cand.ID == excludeID {
			continue
		}
		return cand, queue, newPage, newSeed, nil
	}
	return item, queue, newPage, newSeed, errNoResults
}

// applyDesktop 把本地图片应用到桌面。调用方需持有 s.mu。
func (s *Scheduler) applyDesktop(item state.Item) error {
	if err := wallpaper.SetDesktop(item.Path, s.cfg.DesktopFit); err != nil {
		return fmt.Errorf("设置桌面壁纸失败: %w", err)
	}
	return nil
}

// applyLockScreenItem 把本地图片写入锁屏目标文件并触发计划任务。调用方需持有 s.mu。
func (s *Scheduler) applyLockScreenItem(item state.Item) error {
	// 无论任务是否已安装都写目标文件, 便于 --install-task 安装后立即生效
	if err := os.WriteFile(paths.LockScreenTargetFile(), []byte(item.Path), 0o644); err != nil {
		return fmt.Errorf("写入锁屏目标文件失败: %w", err)
	}
	if s.state.Get().TaskInstalled {
		if err := elevate.RunTask(); err != nil {
			// 任务可能已被外部删除: 自愈重置标记, 降级为警告, 不中断本轮换图
			if ok, _ := elevate.TaskExists(); !ok {
				_ = s.state.Update(func(u *state.State) { u.TaskInstalled = false })
				s.log.Printf("锁屏计划任务不存在, 已重置安装标记(保存配置可重新安装): %v", err)
				return nil
			}
			return fmt.Errorf("触发锁屏计划任务失败: %w", err)
		}
		s.log.Printf("lock screen task triggered: %s", item.Path)
	}
	return nil
}

// cleanupCache 按修改时间清理缓存:
// 先删除超过 max_days 的文件(max_days=0 表示不限), 再保留最近 max_items 张;
// 当前桌面图与当前锁屏图永远不删。
func (s *Scheduler) cleanupCache() {
	st := s.state.Get()
	keep := map[string]bool{}
	if st.Current.Path != "" {
		keep[strings.ToLower(st.Current.Path)] = true
	}
	if st.LockCurrent.Path != "" {
		keep[strings.ToLower(st.LockCurrent.Path)] = true
	}
	if st.LockScreenTarget != "" {
		keep[strings.ToLower(st.LockScreenTarget)] = true
	}
	maxItems := s.cfg.Cache.MaxItems
	var cutoff time.Time
	if s.cfg.Cache.MaxDays > 0 {
		cutoff = time.Now().AddDate(0, 0, -s.cfg.Cache.MaxDays)
	}

	cacheDir := s.cacheDir()
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return
	}
	type fileAge struct {
		path string
		mod  time.Time
	}
	var files []fileAge
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileAge{filepath.Join(cacheDir, e.Name()), info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })

	kept := 0
	for _, f := range files {
		if keep[strings.ToLower(f.path)] {
			continue
		}
		if !cutoff.IsZero() && f.mod.Before(cutoff) {
			if err := os.Remove(f.path); err != nil {
				s.log.Printf("清理过期缓存失败 %s: %v", f.path, err)
			} else {
				s.log.Printf("cache expired(%d days): %s", s.cfg.Cache.MaxDays, f.path)
			}
			continue
		}
		kept++
		if kept > maxItems {
			if err := os.Remove(f.path); err != nil {
				s.log.Printf("清理缓存失败 %s: %v", f.path, err)
			} else {
				s.log.Printf("cache cleaned: %s", f.path)
			}
		}
	}
}
